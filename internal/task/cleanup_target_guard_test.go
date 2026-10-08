package task

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/gitflow"
)

func TestExecuteTaskCleanup_DeletesLocalBranchWithTrackingRefGuard(t *testing.T) {
	mgr, gitMock, plan := setupExecutableTaskCleanup(t)
	gitMock.deleteBranchIfUnchangedFn = func(_, branch, expectedSHA string) error {
		if branch != "feature/APP-1" || expectedSHA != taskCleanupTaskSHA {
			t.Fatalf("delete branch = %q @ %q", branch, expectedSHA)
		}
		return nil
	}

	result, err := mgr.ExecuteTaskCleanup(t.Context(), plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(gitMock.deleteBranchIfUnchangedGuardCalls) != 1 {
		t.Fatalf("guard calls = %+v", gitMock.deleteBranchIfUnchangedGuardCalls)
	}
	if guards := gitMock.deleteBranchIfUnchangedGuardCalls[0]; len(guards) != 1 || guards[0].Ref != "refs/remotes/origin/master" || guards[0].OID != taskCleanupMasterSHA {
		t.Fatalf("guards = %+v", guards)
	}
	if len(result.Completed) < 2 || !strings.Contains(result.Completed[1], "delete local task branch feature/APP-1") {
		t.Fatalf("completed = %q", result.Completed)
	}
}

func TestExecuteTaskCleanup_MultipleTargetsGuardLocalBranchDeletion(t *testing.T) {
	mgr, gitMock, _ := setupExecutableTaskCleanup(t)
	mgr.flow.BranchTypes[gitflow.BranchTypeFeature] = gitflow.BranchTypeRule{
		Prefixes:      []string{"feature/"},
		MergeTargets:  []string{"master", "develop"},
		CloseStrategy: gitflow.CloseStrategyDirectMerge,
	}
	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Blocked() {
		t.Fatalf("plan blocked: %q", plan.Preview().Blockers)
	}
	_, err = mgr.ExecuteTaskCleanup(t.Context(), plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(gitMock.deleteBranchIfUnchangedGuardCalls) != 1 || len(gitMock.deleteBranchIfUnchangedGuardCalls[0]) != 2 {
		t.Fatalf("guard calls = %+v", gitMock.deleteBranchIfUnchangedGuardCalls)
	}
}

func TestExecuteTaskCleanup_MissingStoreRefBlocksLocalBranchDeletion(t *testing.T) {
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

	noop, err := mgr.executeTaskCleanupStep(t.Context(), plan, TaskCleanupExecutionSelection{}, *branchStep)
	if err == nil {
		t.Fatal("local branch deletion without atomic guard succeeded")
	}
	_ = noop
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

func TestExecuteReleaseCleanup_RemoteSelectionDeletesWithLease(t *testing.T) {
	mgr, gitMock := cleanupTestManager(t, "released")
	selection := DefaultReleaseCleanupSelection()
	selection.DeleteRemoteTaskBranches = true
	remoteDeletes := 0
	gitMock.deleteRemoteBranchIfUnchangedFn = func(_, _, _ string) error {
		remoteDeletes++
		return nil
	}
	gitMock.removeWorktreeFn = func(_, path string, _ bool) error {
		for i, entry := range gitMock.listWorktreesRes {
			if entry.Path == path {
				gitMock.listWorktreesRes = append(gitMock.listWorktreesRes[:i], gitMock.listWorktreesRes[i+1:]...)
				break
			}
		}
		return os.RemoveAll(path)
	}
	plan, err := mgr.PlanReleaseCleanup(t.Context(), "rel-1", selection)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Preview().Blockers) != 0 {
		t.Fatalf("plan blocked at planning: %q", plan.Preview().Blockers)
	}

	_, err = mgr.ExecuteReleaseCleanup(t.Context(), plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if remoteDeletes != 1 {
		t.Fatalf("remote deletes = %d, want 1", remoteDeletes)
	}
}

func TestExecuteReleaseCleanupStep_RemoteDeletionUsesLeaseWithValidTargets(t *testing.T) {
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
	if err != nil {
		t.Fatal(err)
	}
	if remoteDeletes != 1 {
		t.Fatalf("remote branch deletes = %d, want 1", remoteDeletes)
	}
}
