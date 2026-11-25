//go:build integration

package task

import (
	"errors"
	"github.com/D1ssolve/wtui/internal/git"
	"gopkg.in/yaml.v3"
	"path/filepath"
	"testing"
)

func TestReleaseCleanupRejectsEqualTipReplacement(t *testing.T) {
	env := newReleaseIntegrationEnv(t)
	env.addFeatureTask(t, "APP-1", nil)
	mustGit(t, env.repoPath, "push", "origin", "feature/APP-1")
	r, err := env.manager.CreateRelease(t.Context(), CreateReleaseParams{TaskIDs: []string{"APP-1"}, ServiceVersions: map[string]string{"svc-api": "1.2.3"}, StartImmediately: true})
	if err != nil {
		t.Fatal(err)
	}
	r = env.markReleaseMasterMerged(t, r)
	r, err = env.manager.FinalizeRelease(t.Context(), FinishReleaseParams{ReleaseID: r.ID})
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, env.repoPath, "remote", "rename", "origin", "upstream")
	if err := yaml.Unmarshal([]byte("forge: {remote: upstream}"), env.manager.cfg); err != nil {
		t.Fatal(err)
	}
	env.manager.git = git.NewRemoteCommandClient(newIntegrationLogger(), env.manager.cfg.ServiceRemote())
	sel := DefaultReleaseCleanupSelection()
	sel.DeleteRemoteTaskBranches = true
	plan, err := env.manager.PlanReleaseCleanup(t.Context(), r.ID, sel)
	if err != nil || len(plan.Preview().Blockers) != 0 {
		t.Fatalf("plan: %v %v", err, plan.Preview().Blockers)
	}
	old := gitOutput(t, env.repoPath, "remote", "get-url", "upstream")
	replacement := filepath.Join(t.TempDir(), "replacement.git")
	mustGit(t, env.repoPath, "clone", "--bare", old, replacement)
	ref := "refs/heads/feature/APP-1"
	before := gitOutput(t, old, "rev-parse", ref)
	mustGit(t, env.repoPath, "remote", "set-url", "upstream", replacement)
	result, err := env.manager.ExecuteReleaseCleanup(t.Context(), plan, nil)
	if !errors.Is(err, ErrReleaseCleanupBlocked) || len(result.Completed) != 0 {
		t.Errorf("equal-tip replacement: completed=%v err=%v", result.Completed, err)
	}
	for _, repo := range []string{old, replacement} {
		if got := gitOutput(t, repo, "for-each-ref", "--format=%(objectname)", ref); got != before {
			t.Errorf("%s = %q, want %q", repo, got, before)
		}
	}
}

func TestAddTasksRejectsEqualTipReplacement(t *testing.T) {
	env := newReleaseIntegrationEnv(t)
	env.addFeatureTask(t, "APP-1", nil)
	r, err := env.manager.CreateRelease(t.Context(), CreateReleaseParams{TaskIDs: []string{"APP-1"}, ServiceVersions: map[string]string{"svc-api": "1.2.3"}, StartImmediately: true})
	if err != nil {
		t.Fatal(err)
	}
	env.unmergedFeatureTask(t, "APP-2", "two.txt", "two")
	plan, err := env.manager.PlanReleaseTaskAdd(t.Context(), r.ID, []string{"APP-2"})
	if err != nil || !plan.Ready() {
		t.Fatalf("plan=%+v, %v", plan, err)
	}
	url := gitOutput(t, env.repoPath, "remote", "get-url", "origin")
	other := filepath.Join(t.TempDir(), "other.git")
	mustGit(t, env.repoPath, "clone", "--bare", url, other)
	mustGit(t, env.repoPath, "remote", "set-url", "origin", other)
	_, err = env.manager.AddReleaseTasks(t.Context(), plan, nil)
	if !errors.Is(err, ErrReleaseRetryUnsafe) {
		t.Errorf("changed endpoint: %v", err)
	}
	if got := gitOutput(t, other, "rev-parse", "refs/heads/"+r.Services[0].ReleaseBranch); got != plan.Services[0].ReleaseTipSHA {
		t.Fatal("replacement tip moved")
	}
}
