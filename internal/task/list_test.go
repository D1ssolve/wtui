package task

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
)

func TestList_IgnoresDefaultReleaseStorageAndInternals(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	for _, dir := range []string{"APP-100", "APP-100-release", ".releases", ".release-work", ".release-meta"} {
		if err := mkdirAll(filepath.Join(tasksRoot, dir)); err != nil {
			t.Fatalf("setup: create dir %s: %v", dir, err)
		}
	}

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})

	tasks, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("List error: %v", err)
	}

	ids := taskIDs(tasks)
	if !slices.Equal(ids, []string{"APP-100", "APP-100-release"}) {
		t.Fatalf("task IDs = %v, want %v", ids, []string{"APP-100", "APP-100-release"})
	}
}

func TestList_IgnoresConfiguredReleaseRootInsideTasksRoot(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	releaseRoot := filepath.Join(tasksRoot, "custom-releases")

	for _, dir := range []string{"APP-200", "APP-200-release", "custom-releases"} {
		if err := mkdirAll(filepath.Join(tasksRoot, dir)); err != nil {
			t.Fatalf("setup: create dir %s: %v", dir, err)
		}
	}

	cfg := &config.Config{
		TasksRoot:    tasksRoot,
		RootDir:      rootDir,
		BranchPrefix: "feature/",
		Editor:       "code",
		Release: &config.ReleaseConfig{
			RootDir: releaseRoot,
		},
	}
	if _, err := cfg.Effective(); err != nil {
		t.Fatalf("cfg.Effective(): %v", err)
	}
	cfg.TasksRoot = tasksRoot
	cfg.RootDir = rootDir

	mgr := newTestManagerWithCfg(t, cfg, &mockGitClient{})

	tasks, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("List error: %v", err)
	}

	ids := taskIDs(tasks)
	if !slices.Equal(ids, []string{"APP-200", "APP-200-release"}) {
		t.Fatalf("task IDs = %v, want %v", ids, []string{"APP-200", "APP-200-release"})
	}
}

func mkdirAll(path string) error {
	return os.MkdirAll(path, 0o755)
}

func taskIDs(tasks []domain.Task) []string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return ids
}

func TestListServices_ContextCanceledDuringAcquire_ReturnsError(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskDir := filepath.Join(tasksRoot, "IN-CANCEL")

	for i := 0; i < 10; i++ {
		if err := mkdirAll(filepath.Join(taskDir, fmt.Sprintf("svc-%d", i))); err != nil {
			t.Fatalf("setup: create service dir: %v", err)
		}
	}

	gitMock := &mockGitClient{}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	before := runtime.NumGoroutine()
	_, err := mgr.ListServices(ctx, "IN-CANCEL")
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	after := runtime.NumGoroutine()

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ListServices error = %v, want context.Canceled", err)
	}
	if after > before+2 {
		t.Errorf("possible goroutine leak: before=%d after=%d", before, after)
	}
}

func TestListServices_NoPartialSuccessWithNilError(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskDir := filepath.Join(tasksRoot, "IN-MID")

	for i := 0; i < 5; i++ {
		if err := mkdirAll(filepath.Join(taskDir, fmt.Sprintf("svc-%d", i))); err != nil {
			t.Fatalf("setup: create service dir: %v", err)
		}
	}

	cfg := &config.Config{
		TasksRoot:    tasksRoot,
		RootDir:      rootDir,
		BranchPrefix: "feature/",
		Editor:       "code",
		Concurrency:  1,
	}
	if _, err := cfg.Effective(); err != nil {
		t.Fatalf("cfg.Effective(): %v", err)
	}
	cfg.TasksRoot = tasksRoot
	cfg.RootDir = rootDir
	cfg.Concurrency = 1

	started := make(chan struct{}, 1)
	unblock := make(chan struct{})
	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-unblock
			return "", errors.New("canceled")
		},
	}

	mgr := newTestManagerWithCfg(t, cfg, gitMock)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	var services []domain.Service
	go func() {
		var err error
		services, err = mgr.ListServices(ctx, "IN-MID")
		errCh <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}

	cancel()
	close(unblock)

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatalf("ListServices returned nil error, want non-nil cancellation error")
		}
		if len(services) == 5 {
			t.Errorf("ListServices returned full result set with error: %d services", len(services))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ListServices did not return after cancellation")
	}
}

func TestList_ConcurrencyCapHonored_Config2(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	taskIDs := []string{"IN-LIST-1", "IN-LIST-2"}
	servicesPerTask := 4

	fakeCommonByWorktree := make(map[string]string)
	repoEntries := make(map[string][]git.WorktreeEntry)

	for _, taskID := range taskIDs {
		taskDir := filepath.Join(tasksRoot, taskID)
		for i := 0; i < servicesPerTask; i++ {
			name := fmt.Sprintf("svc-%d", i)
			worktreePath := filepath.Join(taskDir, name)
			if err := os.MkdirAll(worktreePath, 0o755); err != nil {
				t.Fatalf("setup: create service dir %s: %v", name, err)
			}

			commonDir := filepath.Join(rootDir, "repos", taskID, name, ".git")
			if err := os.MkdirAll(commonDir, 0o755); err != nil {
				t.Fatalf("setup: create common dir %s: %v", name, err)
			}

			fakeCommonByWorktree[worktreePath] = commonDir
			repoEntries[filepath.Dir(commonDir)] = []git.WorktreeEntry{
				{Path: worktreePath, Branch: "refs/heads/feature/" + taskID},
			}
		}
	}

	started := make(chan struct{})
	continueInspect := make(chan struct{})

	var metricMu sync.Mutex
	active := 0
	maxActive := 0

	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			commonDir, ok := fakeCommonByWorktree[path]
			if !ok {
				return "", errors.New("not a git worktree")
			}

			metricMu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			metricMu.Unlock()

			started <- struct{}{}
			<-continueInspect

			metricMu.Lock()
			active--
			metricMu.Unlock()

			return commonDir, nil
		},
		listWorktreesFn: func(repoPath string) ([]git.WorktreeEntry, error) {
			return repoEntries[repoPath], nil
		},
	}

	cfg := &config.Config{
		TasksRoot:    tasksRoot,
		RootDir:      rootDir,
		BranchPrefix: "feature/",
		Editor:       "code",
		Concurrency:  2,
	}
	if _, err := cfg.Effective(); err != nil {
		t.Fatalf("cfg.Effective(): %v", err)
	}
	cfg.TasksRoot = tasksRoot
	cfg.RootDir = rootDir
	cfg.Concurrency = 2

	mgr := newTestManagerWithCfg(t, cfg, gitMock)

	done := make(chan struct{})
	var listErr error
	var tasks []domain.Task
	go func() {
		tasks, listErr = mgr.List(context.Background())
		close(done)
	}()

	total := len(taskIDs) * servicesPerTask
	for range total {
		<-started
		continueInspect <- struct{}{}
	}
	<-done

	if listErr != nil {
		t.Fatalf("List returned error: %v", listErr)
	}
	if len(tasks) != len(taskIDs) {
		t.Fatalf("len(tasks) = %d, want %d", len(tasks), len(taskIDs))
	}

	metricMu.Lock()
	observedMax := maxActive
	metricMu.Unlock()
	if observedMax > 2 {
		t.Fatalf("max concurrent List workers = %d, want <=2", observedMax)
	}
}

func TestList_NoMultiplicativeFanOut(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	taskIDs := []string{"IN-FAN-1", "IN-FAN-2", "IN-FAN-3"}
	servicesPerTask := 4

	fakeCommonByWorktree := make(map[string]string)
	repoEntries := make(map[string][]git.WorktreeEntry)

	for _, taskID := range taskIDs {
		taskDir := filepath.Join(tasksRoot, taskID)
		for i := 0; i < servicesPerTask; i++ {
			name := fmt.Sprintf("svc-%d", i)
			worktreePath := filepath.Join(taskDir, name)
			if err := os.MkdirAll(worktreePath, 0o755); err != nil {
				t.Fatalf("setup: create service dir %s: %v", name, err)
			}

			commonDir := filepath.Join(rootDir, "repos", taskID, name, ".git")
			if err := os.MkdirAll(commonDir, 0o755); err != nil {
				t.Fatalf("setup: create common dir %s: %v", name, err)
			}

			fakeCommonByWorktree[worktreePath] = commonDir
			repoEntries[filepath.Dir(commonDir)] = []git.WorktreeEntry{
				{Path: worktreePath, Branch: "refs/heads/feature/" + taskID},
			}
		}
	}

	started := make(chan struct{})
	continueInspect := make(chan struct{})

	var metricMu sync.Mutex
	active := 0
	maxActive := 0

	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			commonDir, ok := fakeCommonByWorktree[path]
			if !ok {
				return "", errors.New("not a git worktree")
			}

			metricMu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			metricMu.Unlock()

			started <- struct{}{}
			<-continueInspect

			metricMu.Lock()
			active--
			metricMu.Unlock()

			return commonDir, nil
		},
		listWorktreesFn: func(repoPath string) ([]git.WorktreeEntry, error) {
			return repoEntries[repoPath], nil
		},
	}

	cfg := &config.Config{
		TasksRoot:    tasksRoot,
		RootDir:      rootDir,
		BranchPrefix: "feature/",
		Editor:       "code",
		Concurrency:  2,
	}
	if _, err := cfg.Effective(); err != nil {
		t.Fatalf("cfg.Effective(): %v", err)
	}
	cfg.TasksRoot = tasksRoot
	cfg.RootDir = rootDir
	cfg.Concurrency = 2

	mgr := newTestManagerWithCfg(t, cfg, gitMock)

	done := make(chan struct{})
	var listErr error
	go func() {
		_, listErr = mgr.List(context.Background())
		close(done)
	}()

	total := len(taskIDs) * servicesPerTask
	for range total {
		<-started
		continueInspect <- struct{}{}
	}
	<-done

	if listErr != nil {
		t.Fatalf("List returned error: %v", listErr)
	}

	metricMu.Lock()
	observedMax := maxActive
	metricMu.Unlock()
	if observedMax > 2 {
		t.Fatalf("max concurrent List workers = %d, want <=2 (no multiplicative fan-out)", observedMax)
	}
}

func TestConcurrency_Fallback4_WhenConfigInvalid(t *testing.T) {
	tests := []struct {
		name string
		m    *manager
	}{
		{name: "zero concurrency", m: &manager{cfg: &config.Config{Concurrency: 0}}},
		{name: "negative concurrency", m: &manager{cfg: &config.Config{Concurrency: -1}}},
		{name: "nil config", m: &manager{cfg: nil}},
		{name: "nil manager", m: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.concurrency(); got != 4 {
				t.Fatalf("concurrency() = %d, want 4", got)
			}
		})
	}
}
