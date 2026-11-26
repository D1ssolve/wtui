//go:build integration

package task

import (
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"gopkg.in/yaml.v3"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanupAuthorizesOnlySelectedScope(t *testing.T) {
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
	old := gitOutput(t, env.repoPath, "remote", "get-url", "origin")
	other := filepath.Join(t.TempDir(), "push.git")
	mustGit(t, env.repoPath, "clone", "--bare", old, other)
	mustGit(t, env.repoPath, "remote", "set-url", "--push", "origin", other)
	t.Run("credential-free refusal", func(t *testing.T) {
		const marker = "synthetic-probe"
		mustGit(t, env.repoPath, "remote", "set-url", "origin", "file://user:"+marker+"@localhost"+old)
		defer mustGit(t, env.repoPath, "remote", "set-url", "origin", old)
		_, err := env.manager.PlanReleaseCleanup(t.Context(), r.ID, ReleaseDeletionSelection())
		if err == nil || strings.Contains(err.Error(), marker) {
			t.Fatal("expected refusal without endpoint credentials")
		}
	})
	t.Run("local only", func(t *testing.T) {
		before := gitOutput(t, old, "show-ref")
		otherBefore := gitOutput(t, other, "show-ref")
		plan, err := env.manager.PlanReleaseCleanup(t.Context(), r.ID, DefaultReleaseCleanupSelection())
		if err != nil || len(plan.Preview().Blockers) > 0 {
			t.Fatalf("local-only plan: %v %v", err, plan.Preview().Blockers)
		}
		if _, err := env.manager.ExecuteReleaseCleanup(t.Context(), plan, nil); err != nil {
			t.Fatal(err)
		}
		if gitOutput(t, old, "show-ref") != before || gitOutput(t, other, "show-ref") != otherBefore {
			t.Fatal("local cleanup modified remote refs")
		}
	})
}
func TestReviewTargetIgnoresShadowingLocalBranch(t *testing.T) {
	env := newReleaseIntegrationEnv(t)
	env.unmergedFeatureTask(t, "APP-2", "two.txt", "two")
	mustGit(t, env.repoPath, "remote", "rename", "origin", "upstream")
	if err := yaml.Unmarshal([]byte("forge: {remote: upstream}"), env.manager.cfg); err != nil {
		t.Fatal(err)
	}
	env.manager.git = git.NewRemoteCommandClient(newIntegrationLogger(), env.manager.cfg.ServiceRemote())
	svcs, err := env.manager.ListServices(t.Context(), "APP-2")
	if err != nil {
		t.Fatal(err)
	}
	rule := env.manager.flow.BranchTypes[gitflow.BranchTypeFeature]
	rule.CloseStrategy = gitflow.CloseStrategyReviewRequest
	rule.ReviewTargets = []string{"develop"}
	env.manager.flow.BranchTypes[gitflow.BranchTypeFeature] = rule
	plans, _, _, err := env.manager.workflowPlans(svcs)
	if err != nil {
		t.Fatal(err)
	}
	for _, shadow := range []bool{false, true} {
		if shadow {
			mustGit(t, env.repoPath, "branch", "upstream/develop", "feature/APP-2")
		}
		states, err := env.manager.workflowMergeStates(t.Context(), "APP-2", plans)
		if err != nil || len(states) != 1 || len(states[0].merged) != 1 || states[0].merged[0] {
			t.Fatalf("shadow=%t: states=%+v err=%v", shadow, states, err)
		}
	}
}
