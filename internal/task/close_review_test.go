package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
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
	flow, err := closeTestFlow(cfg, func(flow *gitflow.ResolvedGitFlow) {
		flow.BranchTypes["feature"] = gitflow.BranchTypeRule{
			Prefixes:               []string{"feature/"},
			BaseBranch:             "develop",
			MergeTargets:           []string{"develop"},
			ReviewTargets:          []string{"develop"},
			CloseStrategy:          gitflow.CloseStrategyReviewRequest,
			MergeStrategy:          gitflow.MergeStrategyMerge,
			TagOnClose:             true,
			TagSource:              "develop",
			TriggerPipelineOnClose: true,
			RequiresClean:          true,
		}
	})
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

func cherryPickCloseEnv(t *testing.T, taskID string, picked bool, mutateRelease ...func(*domain.Release)) (*manager, *mockGitClient, *mockForgeClient) {
	t.Helper()
	mgr, gitMock, forgeClient := reviewCloseEnv(t, taskID, func(g *mockGitClient) {
		g.isAncestorFn = func(_, ancestor, descendant string) (bool, error) {
			return !(ancestor == "squash-sha" && descendant == "origin/release/1.0"), nil
		}
		if picked {
			g.commitsMentioningFn = func(_, _, _ string) ([]string, error) { return []string{"picked-sha"}, nil }
		}
	})
	rule := mgr.flow.BranchTypes[gitflow.BranchTypeFeature]
	rule.TagOnClose, rule.TriggerPipelineOnClose, rule.CherryPickToOpenReleases = false, false, true
	mgr.flow.BranchTypes[gitflow.BranchTypeFeature] = rule
	mgr.flow.Release = &gitflow.ReleaseFlow{BranchPrefix: "release/", Steps: []gitflow.Step{{Kind: gitflow.StepCutBranch}, {Kind: gitflow.StepTag, On: "release_branch"}}}

	prepared := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	repo := filepath.Join(filepath.Dir(mgr.cfg.TasksRoot), "repos", "svc-a")
	if _, err := mgr.ensureReleaseDir("rel-1.0", false); err != nil {
		t.Fatal(err)
	}
	release := domain.Release{
		ID: "rel-1.0", Status: domain.ReleaseStatusPrepared, Checkpoint: "prepared", PreparedAt: &prepared, CreatedAt: prepared, UpdatedAt: prepared,
		TaskIDs:  []string{"OTHER-1"},
		Services: []domain.ReleaseService{{Name: "svc-a", RepoPath: repo, ReleaseBranch: "release/1.0", Version: "1.0.0", Tag: "v1.0.0", Status: domain.ReleaseStatusPrepared}},
	}
	for _, mutate := range mutateRelease {
		mutate(&release)
	}
	if _, err := mgr.writeReleaseManifest(release); err != nil {
		t.Fatal(err)
	}
	forgeClient.mrHistoryFn = func(context.Context, string, string) ([]forge.MRInfo, error) {
		return []forge.MRInfo{{Number: 7, State: "merged", SourceBranch: "feature/" + taskID, TargetBranch: "develop"}}, nil
	}
	forgeClient.mrReadinessByNumberFn = func(_ context.Context, n int, _, _ string) (forge.MRReadiness, error) {
		return forge.MRReadiness{Number: n, State: "merged", SourceBranch: "feature/" + taskID, TargetBranch: "develop", HeadSHA: "task-head", MergedSHA: "squash-sha"}, nil
	}
	return mgr, gitMock, forgeClient
}

func TestCloseTask_ReviewMerged_CherryPicksAcceptedMergeIntoOpenRelease(t *testing.T) {
	mgr, gitMock, _ := cherryPickCloseEnv(t, "IN-PICK", false)

	res, err := mgr.CloseTask(t.Context(), CloseTaskParams{TaskID: "IN-PICK"})
	if err != nil || !res.Success {
		t.Fatalf("CloseTask = %+v, %v; want success", res, err)
	}
	gitMock.mu.Lock()
	defer gitMock.mu.Unlock()
	if !slices.Equal(gitMock.cherryPickCalls, []string{"squash-sha"}) {
		t.Fatalf("CherryPick calls = %v, want the accepted merge squash-sha", gitMock.cherryPickCalls)
	}
	if len(gitMock.pushRefWithLeaseCalls) != 1 {
		t.Fatalf("lease pushes = %+v, want one push of the release branch", gitMock.pushRefWithLeaseCalls)
	}
	push := gitMock.pushRefWithLeaseCalls[0]
	if push.TargetRef != "refs/heads/release/1.0" || push.LeaseRef != "refs/heads/release/1.0" || push.LeaseOID != "origin-tip" {
		t.Fatalf("lease push = %+v, want release/1.0 leased on its observed tip", push)
	}
	proof, err := mgr.loadClosePostActionProof("IN-PICK")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(proofActions(t, proof, "svc-a"), closePostActionCherryPick) {
		t.Fatal("cherry_pick post-action not recorded in proof")
	}
}

func TestCloseTask_ReviewMerged_AlreadyPickedReleaseIsSkipped(t *testing.T) {
	mgr, gitMock, _ := cherryPickCloseEnv(t, "IN-PICKED", true)

	res, err := mgr.CloseTask(t.Context(), CloseTaskParams{TaskID: "IN-PICKED"})
	if err != nil || !res.Success {
		t.Fatalf("CloseTask = %+v, %v; want success", res, err)
	}
	gitMock.mu.Lock()
	defer gitMock.mu.Unlock()
	if len(gitMock.cherryPickCalls) != 0 || len(gitMock.pushRefWithLeaseCalls) != 0 {
		t.Fatalf("picks=%v pushes=%v, want an idempotent skip", gitMock.cherryPickCalls, gitMock.pushRefWithLeaseCalls)
	}
}

func TestCloseTask_CherryPick_FailedPreparedReleaseStillReceivesFix(t *testing.T) {
	mgr, gitMock, _ := cherryPickCloseEnv(t, "IN-PICK-FAILED", false, func(r *domain.Release) {
		r.Status = domain.ReleaseStatusFailed
		r.Error = &domain.ReleaseError{Code: "push", Message: "push failed", Recoverable: true}
	})

	if res, err := mgr.CloseTask(t.Context(), CloseTaskParams{TaskID: "IN-PICK-FAILED"}); err != nil || !res.Success {
		t.Fatalf("CloseTask = %+v, %v", res, err)
	}
	if !slices.Equal(gitMock.cherryPickCalls, []string{"squash-sha"}) {
		t.Fatalf("CherryPick calls = %v, want the recoverable release to receive the fix", gitMock.cherryPickCalls)
	}
}

func TestCloseTask_CherryPick_AcceptedReleaseIsReportedNotPicked(t *testing.T) {
	mgr, gitMock, _ := cherryPickCloseEnv(t, "IN-PICK-ACCEPTED", false, func(r *domain.Release) {
		r.Status = domain.ReleaseStatusMasterMerged
		r.Services[0].AcceptedMergeSHA = "accepted-sha"
	})

	res, err := mgr.CloseTask(t.Context(), CloseTaskParams{TaskID: "IN-PICK-ACCEPTED"})
	if err != nil {
		t.Fatal(err)
	}
	if len(gitMock.cherryPickCalls) != 0 || len(gitMock.pushRefWithLeaseCalls) != 0 {
		t.Fatalf("picked into an accepted release: picks=%v pushes=%v", gitMock.cherryPickCalls, gitMock.pushRefWithLeaseCalls)
	}
	reported := false
	for _, step := range res.Steps {
		reported = reported || (step.Status == StepStatusSkipped && strings.Contains(step.Message, "NOT picked") && strings.Contains(step.Message, "rel-1.0"))
	}
	if !reported {
		t.Fatalf("steps = %+v, want an explicit not-picked report for rel-1.0", res.Steps)
	}
}

func TestCloseTask_ReviewRequest_ForgeRemovedSourceNeedsExactEvidence(t *testing.T) {
	networkErr := errors.New("ls-remote: network unreachable")
	cases := []struct {
		name               string
		remove, postAction bool
		remoteSource       string
		remoteErr, fetch   error
		mrHead             string
		contained          bool
		want               string // empty: the merged MR is verified
	}{
		{name: "removal requested, source removed, exact evidence", remove: true, mrHead: "task-head", contained: true},
		{name: "no removal policy keeps retained-source guard", mrHead: "task-head", contained: true, want: "has no remote ref"},
		{name: "removal with post-actions keeps retained-source guard", remove: true, postAction: true, mrHead: "task-head", contained: true, want: "has no remote ref"},
		{name: "ls-remote failure is not a removed ref", remove: true, remoteErr: networkErr, mrHead: "task-head", contained: true, want: "network unreachable"},
		{name: "fetch failure blocks", remove: true, fetch: errors.New("fetch: host unreachable"), mrHead: "task-head", contained: true, want: "host unreachable"},
		{name: "MR head differs from local task head", remove: true, mrHead: "other-head", contained: true, want: "does not match"},
		{name: "merge not contained in fresh target", remove: true, mrHead: "task-head", want: "not contained"},
		{name: "source present but diverged", remove: true, remoteSource: "moved-head", mrHead: "task-head", contained: true, want: "diverged"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			taskID := "IN-REV-RM"
			mgr, _, forgeClient := reviewCloseEnv(t, taskID, func(g *mockGitClient) {
				g.remoteRefSHAFn = func(_ string, ref string) (string, error) {
					if ref == "refs/heads/feature/"+taskID {
						return tc.remoteSource, tc.remoteErr
					}
					return "", nil
				}
				g.fetchErr = tc.fetch
				g.isAncestorFn = func(_, _, _ string) (bool, error) { return tc.contained, nil }
			})
			rule := mgr.flow.BranchTypes["feature"]
			rule.TagOnClose, rule.TriggerPipelineOnClose = tc.postAction, tc.postAction
			rule.MR.RemoveSourceBranch = tc.remove
			mgr.flow.BranchTypes["feature"] = rule
			forgeClient.mrHistoryFn = func(context.Context, string, string) ([]forge.MRInfo, error) {
				return []forge.MRInfo{{Number: 7, State: "merged", SourceBranch: "feature/" + taskID, TargetBranch: "develop", URL: "https://gitlab.example/7"}}, nil
			}
			forgeClient.mrReadinessByNumberFn = func(_ context.Context, n int, _, _ string) (forge.MRReadiness, error) {
				return forge.MRReadiness{Number: n, State: "merged", SourceBranch: "feature/" + taskID, TargetBranch: "develop", HeadSHA: tc.mrHead, MergedSHA: "squash-sha", URL: "https://gitlab.example/7"}, nil
			}

			res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID})
			var review CloseTaskStep
			for _, step := range res.Steps {
				if step.Name == "svc-a:review-request" {
					review = step
				}
			}
			if tc.want == "" {
				if err != nil || !res.Success || !strings.Contains(review.Message, "merged MR verified at squash-sha") {
					t.Fatalf("CloseTask = %+v, err %v; want the merged MR verified at squash-sha", res, err)
				}
				return
			}
			blocked := err != nil && strings.Contains(err.Error(), tc.want)
			for _, step := range res.Steps {
				blocked = blocked || step.Status == StepStatusFailed && strings.Contains(step.Message, tc.want)
			}
			if res.Success || !blocked {
				t.Fatalf("CloseTask = %+v, err %v; want a failure containing %q", res, err, tc.want)
			}
		})
	}
}

func TestCloseTask_ReviewRequest_RemovalPolicyClassifiesBeforePublishingSource(t *testing.T) {
	const taskID = "IN-REV-PUB"
	branch := "feature/" + taskID
	mr := func(state, target string) forge.MRInfo {
		return forge.MRInfo{Number: 7, State: state, SourceBranch: branch, TargetBranch: target, URL: "https://gitlab.example/7"}
	}
	cases := []struct {
		name                string
		remove              bool
		history             []forge.MRInfo
		state, mrTarget     string
		mrHead, remoteHead  string
		ancestor            bool
		leaseErr            error
		want                string // substring of the review-request step message
		wantFailed          bool
		wantPush, wantPush2 int // git push calls, lease-guarded pushes
	}{
		{name: "merged with exact evidence verifies without publishing", remove: true, history: []forge.MRInfo{mr("merged", "develop")}, state: "merged", mrHead: "task-head", ancestor: true, want: "merged MR verified at squash-sha"},
		{name: "merged with another head publishes nothing", remove: true, history: []forge.MRInfo{mr("merged", "develop")}, state: "merged", mrHead: "other-head", ancestor: true, want: "does not match", wantFailed: true},
		{name: "merged into an unexpected target publishes nothing", remove: true, history: []forge.MRInfo{mr("merged", "develop")}, state: "merged", mrTarget: "master", mrHead: "task-head", ancestor: true, want: "identity changed", wantFailed: true},
		{name: "ambiguous history publishes nothing", remove: true, history: []forge.MRInfo{mr("merged", "develop"), {Number: 9, State: "opened", SourceBranch: branch, TargetBranch: "develop"}}, want: "ambiguous", wantFailed: true},
		{name: "first review publishes then creates", remove: true, want: "created", wantPush: 1},
		{name: "open MR at the local head needs no publication", remove: true, history: []forge.MRInfo{mr("opened", "develop")}, state: "open", mrHead: "task-head", remoteHead: "task-head", want: "waiting for merge"},
		{name: "open MR behind the local head is updated under lease", remove: true, history: []forge.MRInfo{mr("opened", "develop")}, state: "open", mrHead: "old-head", remoteHead: "old-head", ancestor: true, want: "waiting for merge", wantPush2: 1},
		{name: "open MR whose source vanished is not recreated", remove: true, history: []forge.MRInfo{mr("opened", "develop")}, state: "open", mrHead: "old-head", want: "absent on the remote", wantFailed: true},
		{name: "open MR with rewritten remote is not overwritten", remove: true, history: []forge.MRInfo{mr("opened", "develop")}, state: "open", mrHead: "other-head", remoteHead: "other-head", want: "fast-forward", wantFailed: true},
		{name: "lease rejected after a concurrent merge fails", remove: true, history: []forge.MRInfo{mr("opened", "develop")}, state: "open", mrHead: "old-head", remoteHead: "old-head", ancestor: true, leaseErr: errors.New("stale info"), want: "stale info", wantFailed: true, wantPush2: 1},
		{name: "no policy keeps the legacy publish-then-verify order", history: []forge.MRInfo{mr("merged", "develop")}, state: "merged", mrHead: "task-head", remoteHead: "task-head", ancestor: true, want: "merged MR verified at squash-sha", wantPush: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr, gitMock, forgeClient := reviewCloseEnv(t, taskID, func(g *mockGitClient) {
				g.remoteRefSHAFn = func(_ string, ref string) (string, error) {
					if ref == "refs/heads/"+branch {
						return tc.remoteHead, nil
					}
					return "", nil
				}
				g.isAncestorFn = func(_, _, _ string) (bool, error) { return tc.ancestor, nil }
				g.pushRefWithLeaseFn = func(_, _, _, _, _, _ string) error { return tc.leaseErr }
			})
			mgr.cfg.Close.PushSourceBeforeReview = true
			rule := mgr.flow.BranchTypes["feature"]
			rule.TagOnClose, rule.TriggerPipelineOnClose = false, false
			rule.MR.RemoveSourceBranch = tc.remove
			mgr.flow.BranchTypes["feature"] = rule
			if tc.history != nil {
				forgeClient.mrHistoryFn = func(context.Context, string, string) ([]forge.MRInfo, error) { return tc.history, nil }
			}
			target := tc.mrTarget
			if target == "" {
				target = "develop"
			}
			forgeClient.mrReadinessByNumberFn = func(_ context.Context, n int, _, _ string) (forge.MRReadiness, error) {
				return forge.MRReadiness{Number: n, State: tc.state, SourceBranch: branch, TargetBranch: target, HeadSHA: tc.mrHead, MergedSHA: "squash-sha", URL: "https://gitlab.example/7"}, nil
			}

			res, _ := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID})
			var review CloseTaskStep
			for _, step := range res.Steps {
				if step.Name == "svc-a:review-request" || (review.Name == "" && step.Status == StepStatusFailed) {
					review = step
				}
			}
			if !strings.Contains(review.Message, tc.want) || (review.Status == StepStatusFailed) != tc.wantFailed {
				t.Fatalf("steps = %+v; want %q (failed=%v)", res.Steps, tc.want, tc.wantFailed)
			}
			gitMock.mu.Lock()
			pushes, leases := len(gitMock.pushCalls), append([]pushRefWithLeaseCall(nil), gitMock.pushRefWithLeaseCalls...)
			gitMock.mu.Unlock()
			if pushes != tc.wantPush || len(leases) != tc.wantPush2 {
				t.Fatalf("git push calls = %d, lease pushes = %+v; want %d and %d", pushes, leases, tc.wantPush, tc.wantPush2)
			}
			if tc.wantPush2 == 1 && (leases[0].TargetRef != "refs/heads/"+branch || leases[0].ExactOID != "task-head" || leases[0].LeaseRef != "refs/heads/"+branch || leases[0].LeaseOID != "old-head") {
				t.Fatalf("lease push = %+v; want task-head onto %s leased at old-head", leases[0], branch)
			}
		})
	}
}
