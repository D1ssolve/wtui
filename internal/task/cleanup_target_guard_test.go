package task

import (
	"errors"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/gitflow"
)

// Tracking-ref guards are never used as remote locks: local branches are
// always retained and DeleteBranchIfUnchanged is never consulted.
func TestExecuteTaskCleanup_RetainsLocalBranchWithoutConsultingTrackingRefs(t *testing.T) {
	mgr, gitMock, plan := setupExecutableTaskCleanup(t)
	gitMock.deleteBranchIfUnchangedFn = func(_, _, _ string) error {
		t.Fatal("DeleteBranchIfUnchanged called without an atomic remote target guard")
		return nil
	}

	result, err := mgr.ExecuteTaskCleanup(t.Context(), plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(gitMock.deleteBranchIfUnchangedGuardCalls) != 0 {
		t.Fatalf("tracking-ref guards used as remote lock: %+v", gitMock.deleteBranchIfUnchangedGuardCalls)
	}
	if len(result.Retained) != 1 || !strings.Contains(result.Retained[0], "retain local task branch feature/APP-1") {
		t.Fatalf("retained = %q", result.Retained)
	}
}

func TestExecuteTaskCleanup_MultipleTargetsStillRetainBranch(t *testing.T) {
	mgr, gitMock, _ := setupExecutableTaskCleanup(t)
	mgr.flow.BranchTypes[gitflow.BranchTypeFeature] = gitflow.BranchTypeRule{
		Prefixes:      []string{"feature/"},
		MergeTargets:  []string{"master", "develop"},
		CloseStrategy: gitflow.CloseStrategyDirectMerge,
	}
	gitMock.deleteBranchIfUnchangedFn = func(_, _, _ string) error {
		t.Fatal("DeleteBranchIfUnchanged called without an atomic remote target guard")
		return nil
	}

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Blocked() {
		t.Fatalf("plan blocked: %q", plan.Preview().Blockers)
	}
	result, err := mgr.ExecuteTaskCleanup(t.Context(), plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Retained) != 1 || !strings.Contains(result.Retained[0], "retain local task branch feature/APP-1") {
		t.Fatalf("retained = %q", result.Retained)
	}
}

// A target without a local mirror can no longer be "guarded"; retention does
// not depend on tracking refs, so the branch is still only reported retained.
func TestExecuteTaskCleanup_MissingStoreRefStillRetainsBranch(t *testing.T) {
	mgr, gitMock, plan := setupExecutableTaskCleanup(t)
	deletes := 0
	gitMock.deleteBranchIfUnchangedFn = func(_, _, _ string) error {
		deletes++
		return nil
	}

	var branchStep *releaseCleanupStep
	for i := range plan.steps {
		if plan.steps[i].kind == cleanupLocalTaskBranch {
			branchStep = &plan.steps[i]
		}
	}
	if branchStep == nil {
		t.Fatal("plan has no local branch step")
	}
	branchStep.targets[0].storeRef = ""

	noop, err := mgr.executeTaskCleanupStep(t.Context(), plan, *branchStep)
	if err != nil {
		t.Fatalf("retention step failed: %v", err)
	}
	if !noop {
		t.Fatal("retention step must not report a mutation")
	}
	if deletes != 0 {
		t.Fatalf("local delete called %d times without atomic guard", deletes)
	}
}

func TestExecuteTaskCleanup_TargetStoreRefTamperBlocksAsStale(t *testing.T) {
	mgr, gitMock, plan := setupExecutableTaskCleanup(t)
	plan.proofs[0].Targets[0].storeRef = "refs/remotes/origin/tampered"

	_, err := mgr.ExecuteTaskCleanup(t.Context(), plan, nil)
	if !errors.Is(err, ErrTaskCleanupBlocked) {
		t.Fatalf("error = %v, want stale plan block", err)
	}
	if len(gitMock.removeWorktreeCalls) != 0 {
		t.Fatal("mutation occurred for tampered target fingerprint")
	}
}

func TestExecuteReleaseCleanup_RemoteSelectionBlockedBeforeAnyMutation(t *testing.T) {
	mgr, gitMock := cleanupTestManager(t, "released")
	selection := DefaultReleaseCleanupSelection()
	selection.DeleteRemoteTaskBranches = true
	plan, err := mgr.PlanReleaseCleanup(t.Context(), "rel-1", selection)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Preview().Blockers) != 0 {
		t.Fatalf("plan blocked at planning: %q", plan.Preview().Blockers)
	}

	_, err = mgr.ExecuteReleaseCleanup(t.Context(), plan, nil)
	if !errors.Is(err, ErrReleaseCleanupBlocked) || !errors.Is(err, ErrRemoteAtomicGuardUnsupported) {
		t.Fatalf("error = %v, want unsupported remote deletion block", err)
	}
	if len(gitMock.removeWorktreeCalls) != 0 || gitMock.deleteBranchCalls != 0 {
		t.Fatal("mutation occurred before unsupported remote deletion block")
	}
}

func TestExecuteReleaseCleanupStep_RemoteDeletionFailsClosedWithValidTargets(t *testing.T) {
	mgr, gitMock := cleanupTestManager(t, "released")
	gitMock.listWorktreesRes = nil
	gitMock.resolveRefFn = func(_, _ string) (string, error) { return taskCleanupTaskSHA, nil }
	gitMock.remoteRefSHAFn = func(_, _ string) (string, error) { return taskCleanupTaskSHA, nil }
	gitMock.isAncestorFn = func(_, _, _ string) (bool, error) { return true, nil }
	remoteDeletes := 0
	gitMock.deleteRemoteBranchIfUnchangedFn = func(_, _, _ string) error {
		remoteDeletes++
		return nil
	}
	step := releaseCleanupStep{
		kind: cleanupRemoteTaskBranch, repoPath: mgr.cfg.RootDir, branch: "feature/APP-1", expectedSHA: taskCleanupTaskSHA,
		targets: []releaseCleanupTarget{{ref: "refs/heads/develop", plannedSHA: taskCleanupDevelopSHA, storeRef: "refs/remotes/origin/develop"}},
	}

	err := mgr.executeReleaseCleanupStep(t.Context(), ReleaseCleanupPlan{}, step)
	if !errors.Is(err, ErrRemoteAtomicGuardUnsupported) {
		t.Fatalf("error = %v, want unsupported atomic guard blocker", err)
	}
	if remoteDeletes != 0 {
		t.Fatalf("remote branch deleted without atomic target guard: %d calls", remoteDeletes)
	}
}
