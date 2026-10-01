package task

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

// reviewCloseEnv builds a single-service task whose feature rule closes via
// review_request and requires tag + pipeline post-actions.
func reviewCloseEnv(t *testing.T, taskID string, mutateGit func(*mockGitClient)) (*manager, *mockGitClient, *mockForgeClient) {
	t.Helper()
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	svcPath := filepath.Join(tasksRoot, taskID, "svc-a")
	for _, dir := range []string{svcPath, filepath.Join(rootDir, "repos", "svc-a", ".git")} {
		if err := osMkdirAll(dir); err != nil {
			t.Fatal(err)
		}
	}

	gitMock := &mockGitClient{
		commonDirFn:      func(path string) (string, error) { return filepath.Join(rootDir, "repos", "svc-a", ".git"), nil },
		listWorktreesRes: []git.WorktreeEntry{{Path: svcPath, Branch: "refs/heads/feature/" + taskID}},
		repoStatusFn:     func(string) (git.RawStatus, error) { return git.RawStatus{Branch: "feature/" + taskID}, nil },
		remoteURLRes:     "git@gitlab.com:group/svc-a.git",
		resolveRefFn: func(_ string, ref string) (string, error) {
			switch ref {
			case "feature/" + taskID, "refs/heads/feature/" + taskID:
				return "task-head", nil
			case "refs/tags/v0.1.0":
				return "tag-oid-sha", nil
			case "tag-oid-sha^{commit}":
				return "squash-sha", nil
			}
			return "origin-tip", nil
		},
		remoteRefSHAFn: func(_ string, ref string) (string, error) {
			if ref == "refs/heads/feature/"+taskID {
				return "task-head", nil
			}
			return "", nil
		},
		isAncestorFn: func(_, _, _ string) (bool, error) { return true, nil },
	}
	if mutateGit != nil {
		mutateGit(gitMock)
	}

	cfg := newCloseTestConfig(rootDir, tasksRoot)
	cfg.Close.PushSourceBeforeReview = false
	cfg.GitFlow.BranchTypes["feature"] = config.BranchTypeRule{
		Prefixes:               []string{"feature/"},
		BaseBranch:             "develop",
		MergeTargets:           []string{"develop"},
		ReviewTargets:          []string{"develop"},
		CloseStrategy:          "review_request",
		MergeStrategy:          "merge_commit",
		TagOnClose:             true,
		TagSource:              "develop",
		TriggerPipelineOnClose: true,
		RequiresClean:          true,
	}
	flow, err := gitflow.EffectiveConfig(cfg.GitFlow)
	if err != nil {
		t.Fatal(err)
	}

	var mrStates []forge.MRInfo
	var created int
	forgeClient := &mockForgeClient{
		mrHistoryFn: func(_ context.Context, sourceBranch, _ string) ([]forge.MRInfo, error) {
			return mrStates, nil
		},
		createMRFn: func(_ context.Context, params forge.CreateMRParams) (forge.MRInfo, error) {
			if params.SourceBranch != "feature/"+taskID || params.TargetBranch != "develop" {
				t.Fatalf("CreateMR params = %+v", params)
			}
			mrStates = append(mrStates, forge.MRInfo{Number: 7, State: "opened", SourceBranch: params.SourceBranch, TargetBranch: params.TargetBranch, URL: "https://gitlab.example/7"})
			return forge.MRInfo{Number: 7, URL: "https://gitlab.example/7"}, nil
		},
	}
	forgeClient.createdCount = &created
	mgr := newTestManagerWithDeps(t, cfg, gitMock, flow, map[forge.ForgeProvider]forge.ForgeClient{
		forge.ForgeProviderGitLab: forgeClient,
	}).(*manager)
	return mgr, gitMock, forgeClient
}

func TestCloseTask_ReviewRequest_WaitsForVerifiedMergeBeforePostActions(t *testing.T) {
	taskID := "IN-REV-WAIT"
	mgr, gitMock, forgeClient := reviewCloseEnv(t, taskID, nil)

	// Phase 1: MR created — waiting, zero post-actions, zero proof.
	res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID})
	if err != nil {
		t.Fatalf("CloseTask: %v", err)
	}
	if !res.Waiting || res.Success {
		t.Fatalf("fresh MR must wait: %+v", res)
	}
	if len(res.MRURLs) != 1 {
		t.Fatalf("MRURLs = %v, want the created MR", res.MRURLs)
	}
	gitMock.mu.Lock()
	if gitMock.createTagCalls != 0 || gitMock.pushTagCalls != 0 {
		t.Fatalf("post-actions ran after MR creation: create=%d push=%d", gitMock.createTagCalls, gitMock.pushTagCalls)
	}
	gitMock.mu.Unlock()
	if _, err := os.Stat(mgr.closePostActionsProofPath(taskID)); !os.IsNotExist(err) {
		t.Fatalf("proof persisted before merge: %v", err)
	}

	// Phase 2: MR still open — still waiting, MR never recreated, still zero
	// post-actions.
	forgeClient.mrReadinessByNumberFn = func(_ context.Context, n int, _, _ string) (forge.MRReadiness, error) {
		if n != 7 {
			t.Fatalf("readiness for MR !%d, want !7", n)
		}
		return forge.MRReadiness{Number: 7, State: "open", SourceBranch: "feature/" + taskID, TargetBranch: "develop", HeadSHA: "task-head", URL: "https://gitlab.example/7"}, nil
	}
	res, err = mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID})
	if err != nil {
		t.Fatalf("retry CloseTask: %v", err)
	}
	if !res.Waiting || res.Success {
		t.Fatalf("open MR must keep waiting: %+v", res)
	}
	if *forgeClient.createdCount != 1 {
		t.Fatalf("CreateMR calls = %d, want 1: open MR must not be recreated", *forgeClient.createdCount)
	}
	gitMock.mu.Lock()
	if gitMock.createTagCalls != 0 || gitMock.pushTagCalls != 0 {
		t.Fatalf("post-actions ran while MR open: create=%d push=%d", gitMock.createTagCalls, gitMock.pushTagCalls)
	}
	gitMock.mu.Unlock()

	// Phase 3: MR merged externally (squash) — head equals current source,
	// authoritative MergedSHA contained in fresh origin/develop: post-actions
	// run against the accepted merge SHA and the close completes.
	triggered := 0
	forgeClient.triggerPipelineFn = func(_ context.Context, _ forge.TriggerPipelineParams) error {
		triggered++
		return nil
	}
	forgeClient.mrReadinessByNumberFn = func(_ context.Context, n int, _, _ string) (forge.MRReadiness, error) {
		return forge.MRReadiness{Number: n, State: "merged", SourceBranch: "feature/" + taskID, TargetBranch: "develop", HeadSHA: "task-head", MergedSHA: "squash-sha", URL: "https://gitlab.example/7"}, nil
	}
	res, err = mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID})
	if err != nil {
		t.Fatalf("merged retry CloseTask: %v", err)
	}
	if res.Waiting || !res.Success {
		t.Fatalf("merged retry must complete: %+v", res)
	}
	if *forgeClient.createdCount != 1 {
		t.Fatalf("CreateMR calls = %d, want 1: merged MR must not be recreated", *forgeClient.createdCount)
	}
	if triggered != 1 {
		t.Fatalf("TriggerPipeline calls = %d, want 1", triggered)
	}

	gitMock.mu.Lock()
	createCalls := append([]createTagCall(nil), gitMock.createTagCallList...)
	pushes := append([]pushTagCall(nil), gitMock.pushTagCallList...)
	gitMock.mu.Unlock()
	if len(createCalls) != 1 || createCalls[0].Target != "squash-sha" {
		t.Fatalf("CreateTag calls = %+v, want one tag at accepted merge SHA squash-sha", createCalls)
	}
	if len(pushes) != 1 || pushes[0].ObjectOID != "tag-oid-sha" {
		t.Fatalf("PushTag calls = %+v, want one push of the pinned tag object", pushes)
	}

	proof, err := mgr.loadClosePostActionProof(taskID)
	if err != nil {
		t.Fatal(err)
	}
	actions := proofActions(t, proof, "svc-a")
	for _, action := range []string{closePostActionTag, closePostActionPipeline} {
		if !slices.Contains(actions, action) {
			t.Fatalf("svc-a actions = %v, want %s recorded", actions, action)
		}
	}
}

func TestCloseTask_ReviewRequest_MergedRetryDriftBlocksPostActions(t *testing.T) {
	cases := []struct {
		name     string
		headSHA  string
		ancestor bool
		wantErr  string
	}{
		{name: "merged head behind current source", headSHA: "stale", ancestor: true, wantErr: "does not match current source"},
		{name: "merge SHA not contained in fresh target", headSHA: "task-head", ancestor: false, wantErr: "not contained"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			taskID := "IN-REV-DRIFT"
			mgr, gitMock, forgeClient := reviewCloseEnv(t, taskID, func(g *mockGitClient) {
				g.isAncestorFn = func(_, _, _ string) (bool, error) { return tc.ancestor, nil }
			})
			forgeClient.mrHistoryFn = func(_ context.Context, _, _ string) ([]forge.MRInfo, error) {
				return []forge.MRInfo{{Number: 7, State: "merged", SourceBranch: "feature/" + taskID, TargetBranch: "develop", URL: "https://gitlab.example/7"}}, nil
			}
			forgeClient.mrReadinessByNumberFn = func(_ context.Context, n int, _, _ string) (forge.MRReadiness, error) {
				return forge.MRReadiness{Number: n, State: "merged", SourceBranch: "feature/" + taskID, TargetBranch: "develop", HeadSHA: tc.headSHA, MergedSHA: "squash-sha"}, nil
			}

			res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID})
			if err == nil {
				t.Fatal("CloseTask error = nil, want drift rejection")
			}
			if res.Success {
				t.Fatalf("drifted merge accepted: %+v", res)
			}
			status, msg := closeStepStatus(res, "svc-a:review-request")
			if status != StepStatusFailed || !strings.Contains(msg, tc.wantErr) {
				t.Fatalf("review-request step = %q %q, want failed containing %q", status, msg, tc.wantErr)
			}
			gitMock.mu.Lock()
			defer gitMock.mu.Unlock()
			if gitMock.createTagCalls != 0 || gitMock.pushTagCalls != 0 {
				t.Fatalf("post-actions ran against drifted merge: create=%d push=%d", gitMock.createTagCalls, gitMock.pushTagCalls)
			}
		})
	}
}

func TestCloseTask_ReviewRequest_RemoteSourceAdvanceBlocksMergedAcceptance(t *testing.T) {
	taskID := "IN-REV-ADV"
	mgr, gitMock, forgeClient := reviewCloseEnv(t, taskID, func(g *mockGitClient) {
		// The forge merged the MR, but the remote source advanced past the
		// local branch: local "task-head", fresh remote "advanced-head".
		g.remoteRefSHAFn = func(_ string, ref string) (string, error) {
			if ref == "refs/heads/feature/"+taskID {
				return "advanced-head", nil
			}
			return "", nil
		}
	})
	forgeClient.mrHistoryFn = func(_ context.Context, _, _ string) ([]forge.MRInfo, error) {
		return []forge.MRInfo{{Number: 7, State: "merged", SourceBranch: "feature/" + taskID, TargetBranch: "develop", URL: "https://gitlab.example/7"}}, nil
	}
	forgeClient.mrReadinessByNumberFn = func(_ context.Context, n int, _, _ string) (forge.MRReadiness, error) {
		return forge.MRReadiness{Number: n, State: "merged", SourceBranch: "feature/" + taskID, TargetBranch: "develop", HeadSHA: "task-head", MergedSHA: "squash-sha"}, nil
	}

	res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID})
	if err == nil {
		t.Fatal("CloseTask error = nil, want remote source advance rejection")
	}
	if res.Success {
		t.Fatalf("advanced remote source accepted: %+v", res)
	}
	status, msg := closeStepStatus(res, "svc-a:review-request")
	if status != StepStatusFailed || !strings.Contains(msg, "diverged") {
		t.Fatalf("review-request step = %q %q, want failed containing diverged", status, msg)
	}
	gitMock.mu.Lock()
	defer gitMock.mu.Unlock()
	if gitMock.createTagCalls != 0 || gitMock.pushTagCalls != 0 {
		t.Fatalf("post-actions ran with stale local source: create=%d push=%d", gitMock.createTagCalls, gitMock.pushTagCalls)
	}
}

func TestCloseTask_PushURLMismatchBlocksTagPush(t *testing.T) {
	mgr, gitMock := newTagCloseFixture(t, "IN-PUSHURL-MISMATCH", func(g *mockGitClient, _ *config.Config) {
		g.pushURLRes = "git@gitlab.com:group/someone-else.git"
	})

	res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: "IN-PUSHURL-MISMATCH"})
	if err == nil || !strings.Contains(err.Error(), "push URL") {
		t.Fatalf("CloseTask error = %v, want push URL mismatch", err)
	}
	if res.Success {
		t.Fatalf("pushurl mismatch succeeded: %+v", res)
	}
	gitMock.mu.Lock()
	defer gitMock.mu.Unlock()
	if gitMock.createTagCalls != 0 || gitMock.pushTagCalls != 0 {
		t.Fatalf("tag mutated with mismatched push destination: create=%d push=%d", gitMock.createTagCalls, gitMock.pushTagCalls)
	}
}

func TestHotfixClose_PushURLMismatchBlocksTagPush(t *testing.T) {
	m, g, f := hotfixManager(t)
	f.requests = append(f.requests, forge.MRReadiness{Number: 2, State: "merged", SourceBranch: "hotfix/H", TargetBranch: "develop", HeadSHA: "source", MergedSHA: "develop-merge"})
	g.pushURLRes = "git@gitlab.com:group/someone-else.git"

	if _, err := m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H", TagVersion: "1.2.4"}); err == nil || !strings.Contains(err.Error(), "push URL") {
		t.Fatalf("CloseTask error = %v, want push URL mismatch", err)
	}
	if g.createTagCalls != 0 || g.pushTagCalls != 0 {
		t.Fatalf("hotfix tag mutated with mismatched push destination: create=%d push=%d", g.createTagCalls, g.pushTagCalls)
	}
	proof, err := m.loadClosePostActionProof("H")
	if err != nil {
		t.Fatal(err)
	}
	if len(proof.Services) != 0 {
		t.Fatalf("proof persisted with mismatched push destination: %+v", proof.Services)
	}
}

func TestHotfixClose_SourceMovementAfterPlanBlocksRemainingMutations(t *testing.T) {
	m, g, f := hotfixManager(t)
	f.requests = append(f.requests, forge.MRReadiness{Number: 2, State: "merged", SourceBranch: "hotfix/H", TargetBranch: "develop", HeadSHA: "source", MergedSHA: "develop-merge"})

	// The source stays in sync through planning and the pre-mutation recheck;
	// it moves only at the fresh resolution immediately before the tag action.
	sourceCalls := 0
	g.remoteRefSHAFn = func(_ string, ref string) (string, error) {
		if strings.HasPrefix(ref, "refs/heads/hotfix/") {
			sourceCalls++
			if sourceCalls <= 2 {
				return "source", nil
			}
			return "moved", nil
		}
		return "merge", nil
	}

	if _, err := m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H", TagVersion: "1.2.4"}); err == nil || !strings.Contains(err.Error(), "moved") {
		t.Fatalf("CloseTask error = %v, want source movement blocked", err)
	}
	if g.createTagCalls != 0 || g.pushTagCalls != 0 {
		t.Fatalf("tag mutated after source movement: create=%d push=%d", g.createTagCalls, g.pushTagCalls)
	}
	proof, err := m.loadClosePostActionProof("H")
	if err != nil {
		t.Fatal(err)
	}
	if len(proof.Services) != 0 {
		t.Fatalf("proof persisted after source movement: %+v", proof.Services)
	}
}
