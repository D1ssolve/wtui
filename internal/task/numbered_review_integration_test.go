//go:build integration

package task

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

// Only provider discovery uses the synthetic URL; publications go to local bare remotes.
type reviewDiscoveryGit struct{ git.Client }

func (reviewDiscoveryGit) RemoteURL(_ context.Context, dir, _ string) (string, error) {
	return "git@gitlab.com:fixtures/" + filepath.Base(dir) + ".git", nil
}

func TestCloseTask_NumberedReviewIdentityBeforePublication(t *testing.T) {
	m, _, remotes := serviceRulesGitEnv(t)
	ctx := t.Context()
	const id = "T-IDENTITY"
	const source = "feature/" + id
	flow, err := gitflow.ParseWorkflowYAML(`workflow: {extends: git-flow, services: {api: {task_types: {feature: {close: [{review: {into: [main, stable]}}]}}}}}`)
	if err != nil {
		t.Fatal(err)
	}
	m.flow = flow
	m.cfg.Close.PushSourceBeforeReview = true
	if _, err = m.Init(ctx, InitParams{TaskID: id, Services: []string{"api", "web"}}); err != nil {
		t.Fatal(err)
	}
	before := map[string]string{}
	heads := map[string]string{}
	for name, remote := range remotes {
		wt := filepath.Join(m.cfg.TasksRoot, id, name)
		mustGit(t, wt, "push", "-u", "origin", "HEAD")
		heads[name] = gitOutput(t, remote, "rev-parse", source)
		before[name] = gitOutput(t, remote, "show-ref", "--heads")
	}
	web := filepath.Join(m.cfg.TasksRoot, id, "web")
	mustGit(t, web, "commit", "--allow-empty", "-m", "unpublished web change")
	m.git = reviewDiscoveryGit{m.git}
	var created []string
	api := forge.MRReadiness{Number: 3, SourceBranch: source, TargetBranch: "main", State: "open", HeadSHA: heads["api"]}
	listedWeb := forge.MRReadiness{Number: 7, SourceBranch: source, TargetBranch: "develop", State: "open", HeadSHA: heads["web"]}
	numberedWeb := listedWeb
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitLab: &mockForgeClient{
		mrHistoryFn: func(_ context.Context, _, repo string) ([]forge.MRInfo, error) {
			r := listedWeb
			if filepath.Base(repo) == "api" {
				r = api
			}
			return []forge.MRInfo{{Number: r.Number, SourceBranch: r.SourceBranch, TargetBranch: r.TargetBranch, State: r.State}}, nil
		},
		mrReadinessByNumberFn: func(_ context.Context, n int, _, _ string) (forge.MRReadiness, error) {
			if n == api.Number {
				return api, nil
			}
			return numberedWeb, nil
		},
		createMRFn: func(_ context.Context, p forge.CreateMRParams) (forge.MRInfo, error) {
			created = append(created, p.Repo+":"+p.TargetBranch)
			return forge.MRInfo{Number: 9}, nil
		},
	}}
	plan, err := m.PlanCloseTask(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	params := CloseTaskParams{TaskID: id, Fingerprint: plan.Fingerprint}
	for _, tc := range []struct {
		name   string
		change func(*forge.MRReadiness)
	}{
		{"target", func(r *forge.MRReadiness) { r.TargetBranch = "main" }}, {"source", func(r *forge.MRReadiness) { r.SourceBranch = "other" }}, {"number", func(r *forge.MRReadiness) { r.Number++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			numberedWeb = listedWeb
			tc.change(&numberedWeb)
			if _, err := m.CloseTask(ctx, params); err == nil {
				t.Fatal("accepted contradictory numbered detail")
			}
			if len(created) != 0 {
				t.Fatalf("created MRs before refusal: %v", created)
			}
			for name, remote := range remotes {
				if after := gitOutput(t, remote, "show-ref", "--heads"); after != before[name] {
					t.Fatalf("%s bare refs changed: %s -> %s", name, before[name], after)
				}
			}
		})
	}
	numberedWeb = listedWeb
	result, err := m.CloseTask(ctx, params)
	if err != nil || !result.Waiting {
		t.Fatalf("matching identities: %+v, %v", result, err)
	}
	if !slices.Equal(created, []string{"gitlab.com/fixtures/api:stable"}) {
		t.Fatalf("created %v; only api:stable was missing", created)
	}
	if got, want := gitOutput(t, remotes["web"], "rev-parse", source), gitOutput(t, web, "rev-parse", "HEAD"); got != want {
		t.Fatalf("web source=%s, want %s", got, want)
	}
}
