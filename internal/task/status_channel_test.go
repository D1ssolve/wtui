package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

// Regression tests: SyncTask, SyncService, PushTask, PushService, and
// CloseTask must never close the caller-owned status channel and must safely
// accept a nil channel. Channel closure moved to the TUI command creators
// (see internal/tui/commands.go).

func TestSyncTask_NilLineChannel_DoesNotPanic(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})

	if err := mgr.SyncTask(context.Background(), "", SyncStrategyMerge, nil); err == nil {
		t.Fatal("SyncTask with empty task ID: want error, got nil")
	}
	if err := mgr.SyncTask(context.Background(), "IN-NIL", SyncStrategyNoop, nil); err != nil {
		t.Fatalf("SyncTask noop with nil channel: %v", err)
	}
}

func TestSyncTask_LeavesCallerChannelOpen(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})

	lineCh := make(chan string, 4)
	if err := mgr.SyncTask(context.Background(), "IN-OPEN", SyncStrategyNoop, lineCh); err != nil {
		t.Fatalf("SyncTask noop: %v", err)
	}
	<-lineCh // "sync skipped."

	// Manager must not close the caller-owned channel: sending after return
	// must succeed and the caller drains its own channel.
	lineCh <- "caller line"
	if got := <-lineCh; got != "caller line" {
		t.Fatalf("drained line = %q, want caller line", got)
	}
}

func TestSyncTask_ErrorPath_LeavesCallerChannelOpen(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})

	lineCh := make(chan string, 4)
	if err := mgr.SyncTask(context.Background(), "", SyncStrategyMerge, lineCh); err == nil {
		t.Fatal("SyncTask with empty task ID: want error, got nil")
	}
	lineCh <- "caller line"
}

func TestSyncService_NilLineChannel_DoesNotPanic(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})

	if err := mgr.SyncService(context.Background(), "", "svc-a", SyncStrategyMerge, nil); err == nil {
		t.Fatal("SyncService with empty task ID: want error, got nil")
	}

	taskDir := filepath.Join(tasksRoot, "IN-NIL")
	if err := os.MkdirAll(filepath.Join(taskDir, "svc-a"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := mgr.SyncService(context.Background(), "IN-NIL", "svc-a", SyncStrategyNoop, nil); err != nil {
		t.Fatalf("SyncService noop with nil channel: %v", err)
	}
}

func TestSyncService_LeavesCallerChannelOpen(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})

	taskDir := filepath.Join(tasksRoot, "IN-OPEN")
	if err := os.MkdirAll(filepath.Join(taskDir, "svc-a"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	lineCh := make(chan string, 4)
	if err := mgr.SyncService(context.Background(), "IN-OPEN", "svc-a", SyncStrategyNoop, lineCh); err != nil {
		t.Fatalf("SyncService noop: %v", err)
	}
	<-lineCh // "sync skipped."

	lineCh <- "caller line"
	if got := <-lineCh; got != "caller line" {
		t.Fatalf("drained line = %q, want caller line", got)
	}
}

func TestPushTask_NilLineChannel_DoesNotPanic(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})

	if err := mgr.PushTask(context.Background(), "", nil); err == nil {
		t.Fatal("PushTask with empty task ID: want error, got nil")
	}

	if err := os.MkdirAll(filepath.Join(tasksRoot, "IN-NIL"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := mgr.PushTask(context.Background(), "IN-NIL", nil); err != nil {
		t.Fatalf("PushTask empty task with nil channel: %v", err)
	}
}

func TestPushTask_LeavesCallerChannelOpen(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})

	if err := os.MkdirAll(filepath.Join(tasksRoot, "IN-OPEN"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	lineCh := make(chan string, 4)
	if err := mgr.PushTask(context.Background(), "IN-OPEN", lineCh); err != nil {
		t.Fatalf("PushTask empty task: %v", err)
	}

	lineCh <- "caller line"
	if got := <-lineCh; got != "caller line" {
		t.Fatalf("drained line = %q, want caller line", got)
	}
}

func recvStatusLine(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case line := <-ch:
		return line
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for status line")
		return ""
	}
}

func assertChanOpen(t *testing.T, ch chan string) {
	t.Helper()
	select {
	case ch <- "caller line":
	default:
		t.Fatal("caller-owned status channel was closed by manager")
	}
}

func TestPushService_NilLineChannel_DoesNotPanic(t *testing.T) {
	if err := pushServiceErrorOnEmptyTaskID(t); err == nil {
		t.Fatal("PushService with empty task ID: want error, got nil")
	}

	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	worktreePath := filepath.Join(tasksRoot, "IN-NIL", "svc-a")
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{
		worktreeBranchResult: "feature/IN-NIL",
	})
	if err := mgr.PushService(context.Background(), "IN-NIL", "svc-a", nil); err != nil {
		t.Fatalf("PushService with nil channel: %v", err)
	}
}

func pushServiceErrorOnEmptyTaskID(t *testing.T) error {
	t.Helper()
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})
	return mgr.PushService(context.Background(), "", "svc-a", nil)
}

func TestPushService_LeavesCallerChannelOpen(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	worktreePath := filepath.Join(tasksRoot, "IN-OPEN", "svc-a")
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{
		worktreeBranchResult: "feature/IN-OPEN",
		pushFn: func(_ string, lineCh chan<- string) error {
			lineCh <- "git push output"
			return nil
		},
	})

	lineCh := make(chan string, 8)
	if err := mgr.PushService(context.Background(), "IN-OPEN", "svc-a", lineCh); err != nil {
		t.Fatalf("PushService: %v", err)
	}
	if got := recvStatusLine(t, lineCh); got != "[svc-a] pushing..." {
		t.Fatalf("first line = %q, want [svc-a] pushing...", got)
	}
	if got := recvStatusLine(t, lineCh); got != "git push output" {
		t.Fatalf("second line = %q, want git push output", got)
	}
	if got := recvStatusLine(t, lineCh); got != "[svc-a] pushed." {
		t.Fatalf("third line = %q, want [svc-a] pushed.", got)
	}
	assertChanOpen(t, lineCh)
	if got := recvStatusLine(t, lineCh); got != "caller line" {
		t.Fatalf("drained line = %q, want caller line", got)
	}
}

func TestPushService_ErrorPath_LeavesCallerChannelOpen(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	if err := os.MkdirAll(filepath.Join(tasksRoot, "IN-ERR"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})

	lineCh := make(chan string, 4)
	err := mgr.PushService(context.Background(), "IN-ERR", "svc-a", lineCh)
	if !errors.Is(err, ErrServiceNotFound) {
		t.Fatalf("PushService error = %v, want ErrServiceNotFound", err)
	}
	assertChanOpen(t, lineCh)
}

func closeChannelTestManager(t *testing.T, taskID string, gitMock *mockGitClient) Manager {
	t.Helper()
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	svcPath := filepath.Join(tasksRoot, taskID, "svc-a")
	if err := os.MkdirAll(svcPath, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	fakeCommonDir := filepath.Join(rootDir, "repos", "svc-a", ".git")
	if err := os.MkdirAll(fakeCommonDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if gitMock.commonDirFn == nil {
		gitMock.commonDirFn = func(string) (string, error) { return fakeCommonDir, nil }
	}
	if gitMock.listWorktreesRes == nil {
		gitMock.listWorktreesRes = []git.WorktreeEntry{{Path: svcPath, Branch: "refs/heads/feature/" + taskID}}
	}
	if gitMock.repoStatusFn == nil {
		gitMock.repoStatusFn = func(string) (git.RawStatus, error) {
			return git.RawStatus{Branch: "feature/" + taskID}, nil
		}
	}
	cfg := newCloseTestConfig(rootDir, tasksRoot)
	flow, err := gitflow.EffectiveConfig(cfg.GitFlow)
	if err != nil {
		t.Fatalf("flow: %v", err)
	}
	return newTestManagerWithDeps(t, cfg, gitMock, flow, nil)
}

func TestCloseTask_NilStatusChannel_DoesNotPanic(t *testing.T) {
	mgr := closeChannelTestManager(t, "IN-CLOSE-NIL", &mockGitClient{})

	if _, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: "IN-MISSING"}); err == nil {
		t.Fatal("CloseTask with missing task: want error, got nil")
	}
	if _, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: "IN-CLOSE-NIL", DryRun: true}); err != nil {
		t.Fatalf("CloseTask dry-run with nil channel: %v", err)
	}
}

func TestCloseTask_LeavesCallerChannelOpen(t *testing.T) {
	mgr := closeChannelTestManager(t, "IN-CLOSE-OPEN", &mockGitClient{
		isAncestorFn: func(_, _, _ string) (bool, error) { return true, nil },
	})

	statusCh := make(chan string, 16)
	res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: "IN-CLOSE-OPEN", StatusCh: statusCh})
	if err != nil {
		t.Fatalf("CloseTask: %v", err)
	}
	if !res.Success {
		t.Fatal("result.Success = false, want true")
	}
	recvStatusLine(t, statusCh)
	assertChanOpen(t, statusCh)
}

func TestCloseTask_DryRun_LeavesCallerChannelOpen(t *testing.T) {
	mgr := closeChannelTestManager(t, "IN-CLOSE-DRYCH", &mockGitClient{})

	statusCh := make(chan string, 16)
	res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: "IN-CLOSE-DRYCH", DryRun: true, StatusCh: statusCh})
	if err != nil {
		t.Fatalf("CloseTask dry-run: %v", err)
	}
	if !res.Success {
		t.Fatal("result.Success = false, want true")
	}
	recvStatusLine(t, statusCh)
	assertChanOpen(t, statusCh)
}

func TestCloseTask_ErrorPath_LeavesCallerChannelOpen(t *testing.T) {
	mgr := closeChannelTestManager(t, "IN-CLOSE-ERRCH", &mockGitClient{})

	statusCh := make(chan string, 16)
	_, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: "IN-MISSING", StatusCh: statusCh})
	if err == nil {
		t.Fatal("CloseTask with missing task: want error, got nil")
	}
	recvStatusLine(t, statusCh)
	assertChanOpen(t, statusCh)
}

func TestCloseTask_Hotfix_LeavesCallerChannelOpen(t *testing.T) {
	mgr, _, f := hotfixManager(t)
	mgr.cfg.Tag.Enabled = false
	f.requests = append(f.requests, forge.MRReadiness{Number: 2, State: "merged", SourceBranch: "hotfix/H", TargetBranch: "develop", HeadSHA: "source", MergedSHA: "merge"})

	plan, err := mgr.PlanCloseTask(context.Background(), "H")
	if err != nil {
		t.Fatalf("PlanCloseTask: %v", err)
	}
	statusCh := make(chan string, 32)
	res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: "H", Fingerprint: plan.Fingerprint, StatusCh: statusCh})
	if err != nil {
		t.Fatalf("CloseTask hotfix: %v", err)
	}
	if !res.Success {
		t.Fatal("result.Success = false, want true")
	}
	recvStatusLine(t, statusCh)
	assertChanOpen(t, statusCh)
}
