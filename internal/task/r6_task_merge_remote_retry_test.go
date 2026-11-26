//go:build integration

package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"gopkg.in/yaml.v3"
)

// Model project-scoped MR detail on two GitLab projects containing the same
// imported MR and Git history. All changes to service refs use production Git.
type mirrorReviews struct {
	*mergeForgeClient
	projects map[string]forge.MRReadiness
}

func (f *mirrorReviews) MRStatus(_ context.Context, branch, repo string) ([]forge.MRInfo, error) {
	r, ok := f.projects[repo]
	if !ok || r.SourceBranch != branch {
		return nil, errors.New("no such review project or branch")
	}
	return []forge.MRInfo{{Number: r.Number, State: r.State, URL: r.URL, SourceBranch: branch, TargetBranch: r.TargetBranch}}, nil
}
func (f *mirrorReviews) MRReadinessByNumber(_ context.Context, number int, repo, _ string) (forge.MRReadiness, error) {
	r, ok := f.projects[repo]
	if !ok || r.Number != number {
		return forge.MRReadiness{}, errors.New("no such project or MR")
	}
	return r, nil
}

func TestR6DedicatedTaskMergeRetryKeepsReleaseRemote(t *testing.T) {
	env := newReleaseIntegrationEnv(t)
	m := env.manager
	repo := env.repoPath
	head := env.unmergedFeatureTask(t, "APP-9", "task.txt", "release task")
	up := gitOutput(t, repo, "remote", "get-url", "origin")
	dest := filepath.Join(t.TempDir(), "mirror.git")
	mustGit(t, repo, "clone", "--bare", up, dest)
	mustGit(t, repo, "remote", "rename", "origin", "upstream")
	upURL := "file://gitlab.com" + filepath.ToSlash(up)
	destURL := "file://gitlab.com" + filepath.ToSlash(dest)
	mustGit(t, repo, "remote", "set-url", "upstream", upURL)
	mustGit(t, repo, "remote", "add", "origin", destURL)
	mustGit(t, repo, "fetch", "origin")
	const workflow = `workflow:
  extends: git-flow
  release:
    steps:
      - integrate_tasks: {}
      - cut_branch: {from: integration}
      - gate: {label: regression}
      - review: {from: release, into: production}
      - back_merge: {from: release, into: integration, when: diverged}
      - tag: {on: accepted_merge}
`
	set := func(aliasYAML string) {
		var cfg config.Config
		if err := yaml.Unmarshal([]byte(workflow+aliasYAML), &cfg); err != nil {
			t.Fatal(err)
		}
		f, e := gitflow.Resolve(&cfg)
		if e != nil {
			t.Fatal(e)
		}
		m.flow = f
		m.cfg.Forge = cfg.Forge
		m.git = git.NewRemoteCommandClient(newIntegrationLogger(), m.cfg.ServiceRemote())
	}
	set("forge: {remote: upstream}\n")
	reviews := &mirrorReviews{mergeForgeClient: &mergeForgeClient{}, projects: map[string]forge.MRReadiness{}}
	for _, url := range []string{upURL, destURL} {
		project := forge.ExtractRepoPath(url)
		reviews.projects[project] = forge.MRReadiness{Number: 1, State: "open", SourceBranch: "feature/APP-9", TargetBranch: "develop", HeadSHA: head, Ready: true, URL: "https://" + project + "/-/merge_requests/1"}
	}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitLab: reviews}
	// A task merge denied on upstream leaves a real resumable task_merge_blocked manifest.
	if err := os.WriteFile(filepath.Join(up, "hooks", "pre-receive"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	params := CreateReleaseParams{TaskIDs: []string{"APP-9"}, ServiceVersions: map[string]string{"svc-api": "1.2.3"}, StartImmediately: true}
	plan, e := m.PlanReleaseTaskMerges(ctx, params)
	if e != nil {
		t.Fatal(e)
	}
	params.ConfirmedTaskMergePlan = &plan
	r, e := m.CreateRelease(ctx, params)
	if e == nil || r.Status != domain.ReleaseStatusTaskMergeBlocked {
		t.Fatalf("create fixture: status=%s err=%v", r.Status, e)
	}
	r, e = m.GetRelease(ctx, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("wtui-written blocked manifest fingerprint=%q", r.FlowFingerprint)
	before := gitOutput(t, dest, "show-ref")
	upBefore := gitOutput(t, up, "show-ref")
	set("")
	p, e := m.PlanReleaseTaskMergeRetry(ctx, r.ID)
	if e == nil {
		r, e = m.RetryReleaseTaskMerges(ctx, r.ID, &p)
	}
	after := gitOutput(t, dest, "show-ref")
	t.Logf("status=%s error=%v other project refs before:\n%s\nafter:\n%s", r.Status, e, before, after)
	if !errors.Is(e, ErrReleaseFlowChanged) || after != before || gitOutput(t, up, "show-ref") != upBefore {
		t.Fatal("dedicated retry did not enforce the saved release fingerprint before publication")
	}
}
