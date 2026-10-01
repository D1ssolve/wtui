package task

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/git"
)

func TestExecuteTaskCleanup_RemovesWorktreeRetainsBranchThenEmptyDir(t *testing.T) {
	mgr, gitMock, plan := setupExecutableTaskCleanup(t)
	worktree := filepath.Join(mgr.cfg.TasksRoot, "APP-1", "svc")
	taskDir := filepath.Join(mgr.cfg.TasksRoot, "APP-1")

	gitMock.deleteBranchIfUnchangedFn = func(_, _, _ string) error {
		t.Fatal("DeleteBranchIfUnchanged called without an atomic remote target guard")
		return nil
	}

	statusCh := make(chan string, 16)
	result, err := mgr.ExecuteTaskCleanup(t.Context(), plan, statusCh)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(worktree); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree still exists: %v", err)
	}
	if _, err := os.Stat(taskDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("task directory still exists: %v", err)
	}
	if len(gitMock.removeWorktreeCalls) != 1 || gitMock.removeWorktreeCalls[0].Force {
		t.Fatalf("removeWorktree calls = %+v", gitMock.removeWorktreeCalls)
	}
	if len(result.Completed) != 2 {
		t.Fatalf("completed = %q retained = %q", result.Completed, result.Retained)
	}
	if !strings.HasPrefix(result.Completed[0], "remove task worktree") ||
		!strings.HasPrefix(result.Completed[1], "remove task directory") {
		t.Fatalf("execution order = %q", result.Completed)
	}
	if len(result.Retained) != 1 || !strings.HasPrefix(result.Retained[0], "retain local task branch feature/APP-1") {
		t.Fatalf("retained = %q", result.Retained)
	}
	if len(result.Deferred) != 0 {
		t.Fatalf("deferred = %q", result.Deferred)
	}

	lines := drainTaskCleanupStatus(t, statusCh, 3)
	if len(lines) != 3 {
		t.Fatalf("status lines = %q", lines)
	}
}

func TestExecuteTaskCleanup_DoesNotCloseCallerStatusChannel(t *testing.T) {
	mgr, _, plan := setupExecutableTaskCleanup(t)
	statusCh := make(chan string, 16)
	if _, err := mgr.ExecuteTaskCleanup(t.Context(), plan, statusCh); err != nil {
		t.Fatal(err)
	}
	assertStatusChannelOpen(t, statusCh)
}

func TestExecuteTaskCleanup_StalePlanBlockedBeforeMutation(t *testing.T) {
	mgr, gitMock, plan := setupExecutableTaskCleanup(t)
	base := gitMock.remoteRefSHAFn
	gitMock.remoteRefSHAFn = func(repo, ref string) (string, error) {
		if ref == "refs/heads/master" {
			return strings.Repeat("f", 40), nil
		}
		return base(repo, ref)
	}

	_, err := mgr.ExecuteTaskCleanup(t.Context(), plan, nil)
	if !errors.Is(err, ErrTaskCleanupBlocked) {
		t.Fatalf("error = %v", err)
	}
	if len(gitMock.removeWorktreeCalls) != 0 {
		t.Fatal("mutation occurred before stale plan rejection")
	}
}

func TestExecuteTaskCleanup_BlockedPlanRejected(t *testing.T) {
	mgr, gitMock := taskCleanupTestManager(t)
	gitMock.listWorktreesRes[0].Locked = true
	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Blocked() {
		t.Fatal("plan should be blocked")
	}

	_, err = mgr.ExecuteTaskCleanup(t.Context(), plan, nil)
	if !errors.Is(err, ErrTaskCleanupBlocked) {
		t.Fatalf("error = %v", err)
	}
	if len(gitMock.removeWorktreeCalls) != 0 {
		t.Fatal("mutation occurred for blocked plan")
	}
}

func TestExecuteTaskCleanup_NonEmptyTaskDirStopsAfterCompletedSteps(t *testing.T) {
	mgr, gitMock, plan := setupExecutableTaskCleanup(t)
	gitMock.deleteBranchIfUnchangedFn = func(_, _, _ string) error {
		t.Fatal("DeleteBranchIfUnchanged called without an atomic remote target guard")
		return nil
	}
	notesPath := filepath.Join(mgr.cfg.TasksRoot, "APP-1", "notes.md")
	if err := os.WriteFile(notesPath, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := mgr.ExecuteTaskCleanup(t.Context(), plan, nil)
	if err == nil {
		t.Fatal("expected error for non-empty task directory")
	}
	if len(result.Completed) != 1 || !strings.HasPrefix(result.Completed[0], "remove task worktree") {
		t.Fatalf("completed = %q", result.Completed)
	}
	if len(result.Retained) != 1 || !strings.HasPrefix(result.Retained[0], "retain local task branch") {
		t.Fatalf("retained = %q", result.Retained)
	}
	if _, statErr := os.Stat(notesPath); statErr != nil {
		t.Fatalf("task content removed: %v", statErr)
	}
}

func TestExecuteTaskCleanupStep_MissingResourcesNoOp(t *testing.T) {
	mgr, gitMock := taskCleanupTestManager(t)
	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(mgr.cfg.TasksRoot, "APP-1")); err != nil {
		t.Fatal(err)
	}
	gitMock.listWorktreesRes = nil
	gitMock.branchExistsRes = false
	gitMock.deleteBranchIfUnchangedFn = func(_, _, _ string) error {
		t.Fatal("DeleteBranchIfUnchanged called for absent branch")
		return nil
	}

	for _, step := range plan.steps {
		noop, err := mgr.executeTaskCleanupStep(t.Context(), plan, step)
		if err != nil {
			t.Fatalf("step %q: %v", step.description, err)
		}
		if !noop {
			t.Fatalf("step %q should be a proven-absent no-op", step.description)
		}
	}
	if len(gitMock.removeWorktreeCalls) != 0 {
		t.Fatal("RemoveWorktree called for absent worktree")
	}
}

func TestExecuteTaskCleanupStep_UnregisteredWorktreePathBlocks(t *testing.T) {
	mgr, _ := taskCleanupTestManager(t)
	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	// The path exists on disk but nothing is registered there: the identity
	// proven by the plan is gone and must not be adopted.
	gitMock := mgr.git.(*mockGitClient)
	gitMock.listWorktreesRes = nil

	step := plan.steps[0]
	if step.kind != cleanupTaskWorktree {
		t.Fatalf("step kind = %d", step.kind)
	}
	if _, err := mgr.executeTaskCleanupStep(t.Context(), plan, step); err == nil {
		t.Fatal("unregistered replacement path allowed removal")
	}
	if len(gitMock.removeWorktreeCalls) != 0 {
		t.Fatal("RemoveWorktree called for unregistered path")
	}
}

func TestExecuteTaskCleanup_DeferredRemoteReportedRetained(t *testing.T) {
	mgr, gitMock := taskCleanupTestManager(t)
	base := gitMock.remoteRefSHAFn
	gitMock.remoteRefSHAFn = func(repo, ref string) (string, error) {
		if ref == "refs/heads/feature/APP-1" {
			return strings.Repeat("9", 40), nil
		}
		return base(repo, ref)
	}
	gitMock.listWorktreesFn = func(string) ([]git.WorktreeEntry, error) {
		return gitMock.listWorktreesRes, nil
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
	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Blocked() || len(plan.Preview().DeferredRemote) != 1 {
		t.Fatalf("blocked = %v deferred = %+v", plan.Blocked(), plan.Preview().DeferredRemote)
	}

	result, err := mgr.ExecuteTaskCleanup(t.Context(), plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Deferred) != 1 {
		t.Fatalf("deferred = %q", result.Deferred)
	}
	if len(result.Remote) != 0 {
		t.Fatalf("retained remote candidates = %+v, want none for deferred-only plan", result.Remote)
	}
}

func TestExecuteTaskCleanup_RemovesGeneratedFilesBeforeTaskDirectory(t *testing.T) {
	mgr, gitMock, plan := setupExecutableTaskCleanup(t)
	gitMock.deleteBranchIfUnchangedFn = func(_, _, _ string) error {
		t.Fatal("DeleteBranchIfUnchanged called without an atomic remote target guard")
		return nil
	}
	taskDir := filepath.Join(mgr.cfg.TasksRoot, "APP-1")
	for _, name := range []string{"APP-1.sln", "APP-1.code-workspace"} {
		if err := os.WriteFile(filepath.Join(taskDir, name), []byte("generated"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	result, err := mgr.ExecuteTaskCleanup(t.Context(), plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(taskDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("task directory still exists: %v", statErr)
	}
	if len(result.Completed) != 2 {
		t.Fatalf("completed = %q", result.Completed)
	}
	if len(result.Retained) != 1 {
		t.Fatalf("retained = %q", result.Retained)
	}
	if len(result.Remote) != 1 || result.Remote[0].Branch != "feature/APP-1" {
		t.Fatalf("retained remote candidates = %+v", result.Remote)
	}
}

func TestExecuteTaskCleanup_UnknownFilesPreserveRemoteRetentionReport(t *testing.T) {
	mgr, gitMock, plan := setupExecutableTaskCleanup(t)
	gitMock.branchExistsRes = true
	gitMock.deleteBranchIfUnchangedFn = func(_, _, _ string) error { return nil }
	notesPath := filepath.Join(mgr.cfg.TasksRoot, "APP-1", "notes.md")
	if err := os.WriteFile(notesPath, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := mgr.ExecuteTaskCleanup(t.Context(), plan, nil)
	if err == nil {
		t.Fatal("expected error for unknown files in task directory")
	}
	if _, statErr := os.Stat(notesPath); statErr != nil {
		t.Fatalf("unknown file removed: %v", statErr)
	}
	if len(result.Remote) != 1 {
		t.Fatalf("remote retention report lost after directory failure: %+v", result.Remote)
	}
}

func TestExecuteTaskCleanup_GitStepFailureReportsNoRemoteCandidates(t *testing.T) {
	mgr, gitMock, plan := setupExecutableTaskCleanup(t)
	gitMock.removeWorktreeFn = func(_, _ string, _ bool) error { return errors.New("remove worktree failed") }

	result, err := mgr.ExecuteTaskCleanup(t.Context(), plan, nil)
	if err == nil {
		t.Fatal("expected git step failure")
	}
	if len(result.Completed) != 0 || len(result.Retained) != 0 {
		t.Fatalf("completed = %q retained = %q", result.Completed, result.Retained)
	}
	if len(result.Remote) != 0 {
		t.Fatalf("remote candidates reported after mutation failure: %+v", result.Remote)
	}
}

func TestExecuteTaskCleanup_StalePlanReportsNoRemoteCandidates(t *testing.T) {
	mgr, gitMock, plan := setupExecutableTaskCleanup(t)
	base := gitMock.remoteRefSHAFn
	gitMock.remoteRefSHAFn = func(repo, ref string) (string, error) {
		if ref == "refs/heads/master" {
			return strings.Repeat("f", 40), nil
		}
		return base(repo, ref)
	}

	result, err := mgr.ExecuteTaskCleanup(t.Context(), plan, nil)
	if !errors.Is(err, ErrTaskCleanupBlocked) {
		t.Fatalf("error = %v", err)
	}
	if len(result.Remote) != 0 {
		t.Fatalf("remote candidates reported for stale plan: %+v", result.Remote)
	}
}

func setupExecutableTaskCleanup(t *testing.T) (*manager, *mockGitClient, TaskCleanupPlan) {
	t.Helper()
	mgr, gitMock := taskCleanupTestManager(t)
	worktree := filepath.Join(mgr.cfg.TasksRoot, "APP-1", "svc")
	gitMock.listWorktreesFn = func(string) ([]git.WorktreeEntry, error) {
		return gitMock.listWorktreesRes, nil
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
	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Blocked() {
		t.Fatalf("plan blocked: %q", plan.Preview().Blockers)
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("fixture worktree missing: %v", err)
	}
	return mgr, gitMock, plan
}

func drainTaskCleanupStatus(t *testing.T, statusCh chan string, want int) []string {
	t.Helper()
	lines := make([]string, 0, want)
	for len(lines) < want {
		select {
		case line := <-statusCh:
			lines = append(lines, line)
		case <-time.After(2 * time.Second):
			t.Fatalf("received %d/%d status lines", len(lines), want)
		}
	}
	return lines
}

func assertStatusChannelOpen(t *testing.T, statusCh chan string) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("manager closed caller-owned status channel: %v", r)
		}
	}()
	statusCh <- "probe"
}
