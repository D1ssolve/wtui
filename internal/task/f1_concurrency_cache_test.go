package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
)

type staticResolver struct {
	paths map[string]string
}

func (r *staticResolver) Resolve(_ context.Context, token string) (string, error) {
	if path, ok := r.paths[token]; ok {
		return path, nil
	}
	return "", errors.New("not found")
}

func (r *staticResolver) FindAll(_ context.Context) ([]domain.Repo, error) {
	return nil, nil
}

func TestListServices_WorktreeCacheDedupesConcurrentMisses(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskID := "IN-CACHE"
	taskDir := filepath.Join(tasksRoot, taskID)

	serviceNames := []string{"svc-a", "svc-b", "svc-c", "svc-d"}
	for _, name := range serviceNames {
		if err := os.MkdirAll(filepath.Join(taskDir, name), 0o755); err != nil {
			t.Fatalf("setup: create service dir %s: %v", name, err)
		}
	}

	commonDir := filepath.Join(rootDir, "repos", "mono", ".git")
	if err := os.MkdirAll(commonDir, 0o755); err != nil {
		t.Fatalf("setup: create common dir: %v", err)
	}
	repoPath := filepath.Dir(commonDir)

	var listWorktreesCalls int
	var callsMu sync.Mutex

	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			base := filepath.Base(path)
			switch base {
			case "svc-a", "svc-b", "svc-c", "svc-d":
				return commonDir, nil
			default:
				return "", errors.New("not a git worktree")
			}
		},
		listWorktreesFn: func(receivedRepoPath string) ([]git.WorktreeEntry, error) {
			if receivedRepoPath != repoPath {
				t.Fatalf("ListWorktrees repoPath = %q, want %q", receivedRepoPath, repoPath)
			}

			callsMu.Lock()
			listWorktreesCalls++
			callsMu.Unlock()

			return []git.WorktreeEntry{
				{Path: filepath.Join(taskDir, "svc-a"), Branch: "refs/heads/feature/IN-CACHE-a"},
				{Path: filepath.Join(taskDir, "svc-b"), Branch: "refs/heads/feature/IN-CACHE-b"},
				{Path: filepath.Join(taskDir, "svc-c"), Branch: "refs/heads/feature/IN-CACHE-c"},
				{Path: filepath.Join(taskDir, "svc-d"), Branch: "refs/heads/feature/IN-CACHE-d"},
			}, nil
		},
	}

	cfg := &config.Config{TasksRoot: tasksRoot, RootDir: rootDir, BranchPrefix: "feature/", BaseBranch: "develop", Editor: "code", Concurrency: 4}
	if _, err := cfg.Effective(); err != nil {
		t.Fatalf("cfg.Effective(): %v", err)
	}
	mgr := newTestManagerWithCfg(t, cfg, gitMock)

	services, err := mgr.ListServices(context.Background(), taskID)
	if err != nil {
		t.Fatalf("ListServices returned error: %v", err)
	}

	if len(services) != 4 {
		t.Fatalf("len(services) = %d, want 4", len(services))
	}

	gotBranchByService := make(map[string]string, len(services))
	for _, svc := range services {
		gotBranchByService[svc.Name] = svc.Branch
	}

	if gotBranchByService["svc-a"] != "feature/IN-CACHE-a" {
		t.Fatalf("svc-a branch = %q", gotBranchByService["svc-a"])
	}
	if gotBranchByService["svc-b"] != "feature/IN-CACHE-b" {
		t.Fatalf("svc-b branch = %q", gotBranchByService["svc-b"])
	}
	if gotBranchByService["svc-c"] != "feature/IN-CACHE-c" {
		t.Fatalf("svc-c branch = %q", gotBranchByService["svc-c"])
	}
	if gotBranchByService["svc-d"] != "feature/IN-CACHE-d" {
		t.Fatalf("svc-d branch = %q", gotBranchByService["svc-d"])
	}

	callsMu.Lock()
	gotCalls := listWorktreesCalls
	callsMu.Unlock()
	if gotCalls != 1 {
		t.Fatalf("ListWorktrees calls = %d, want 1", gotCalls)
	}
}

func TestListServices_BoundedConcurrencyHonorsConfig(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskID := "IN-LIST-CONC"
	taskDir := filepath.Join(tasksRoot, taskID)

	serviceNames := []string{"svc-a", "svc-b", "svc-c", "svc-d"}
	fakeCommonByWorktree := make(map[string]string, len(serviceNames))
	repoEntries := make(map[string][]git.WorktreeEntry, len(serviceNames))
	for _, name := range serviceNames {
		worktreePath := filepath.Join(taskDir, name)
		if err := os.MkdirAll(worktreePath, 0o755); err != nil {
			t.Fatalf("setup: create service dir %s: %v", name, err)
		}
		commonDir := filepath.Join(rootDir, "repos", name, ".git")
		if err := os.MkdirAll(commonDir, 0o755); err != nil {
			t.Fatalf("setup: create common dir %s: %v", commonDir, err)
		}
		fakeCommonByWorktree[worktreePath] = commonDir
		repoEntries[filepath.Dir(commonDir)] = []git.WorktreeEntry{{Path: worktreePath, Branch: "refs/heads/feature/IN-LIST-CONC"}}
	}

	started := make(chan struct{}, len(serviceNames))
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
			if entries, ok := repoEntries[repoPath]; ok {
				return entries, nil
			}
			return nil, nil
		},
	}

	cfg := &config.Config{TasksRoot: tasksRoot, RootDir: rootDir, BranchPrefix: "feature/", BaseBranch: "develop", Editor: "code", Concurrency: 2}
	if _, err := cfg.Effective(); err != nil {
		t.Fatalf("cfg.Effective(): %v", err)
	}
	mgr := newTestManagerWithCfg(t, cfg, gitMock)

	done := make(chan struct{})
	var listErr error
	go func() {
		_, listErr = mgr.ListServices(context.Background(), taskID)
		close(done)
	}()

	for range len(serviceNames) {
		<-started
		continueInspect <- struct{}{}
	}

	<-done
	if listErr != nil {
		t.Fatalf("ListServices returned error: %v", listErr)
	}

	metricMu.Lock()
	observedMax := maxActive
	metricMu.Unlock()
	if observedMax > 2 {
		t.Fatalf("max concurrent ListServices workers = %d, want <=2", observedMax)
	}
}

func TestAddWorktreesForServices_BoundedConcurrencyHonorsConfig(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskID := "IN-INIT-CONC"
	taskDir := filepath.Join(tasksRoot, taskID)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("setup: create task dir: %v", err)
	}

	serviceTokens := []string{"a", "b", "c", "d"}
	paths := make(map[string]string, len(serviceTokens))
	for _, token := range serviceTokens {
		paths[token] = filepath.Join(rootDir, "repos", "svc-"+token)
	}

	resolver := &staticResolver{paths: paths}

	started := make(chan struct{}, len(serviceTokens))
	continueAdd := make(chan struct{})

	var metricMu sync.Mutex
	active := 0
	maxActive := 0

	gitMock := &mockGitClient{
		branchExistsRes: false,
		listWorktreesFn: func(_ string) ([]git.WorktreeEntry, error) {
			return nil, nil
		},
		baseBranchResult: "develop",
		addWorktreeFn: func(_, _, _ string, _ bool, _ string) error {
			metricMu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			metricMu.Unlock()

			started <- struct{}{}
			<-continueAdd

			metricMu.Lock()
			active--
			metricMu.Unlock()

			return nil
		},
	}

	cfg := &config.Config{TasksRoot: tasksRoot, RootDir: rootDir, BranchPrefix: "feature/", BaseBranch: "develop", Editor: "code", Concurrency: 2}
	if _, err := cfg.Effective(); err != nil {
		t.Fatalf("cfg.Effective(): %v", err)
	}

	mgr := &manager{
		cfg:        cfg,
		git:        gitMock,
		discoverer: resolver,
		logger:     newTestLogger(),
	}

	done := make(chan []serviceResult, 1)
	go func() {
		done <- mgr.addWorktreesForServices(context.Background(), taskID, serviceTokens, taskDir, "feature/IN-INIT-CONC", "develop", nil, nil, nil)
	}()

	for range len(serviceTokens) {
		<-started
		continueAdd <- struct{}{}
	}

	results := <-done
	if len(results) != len(serviceTokens) {
		t.Fatalf("results length = %d, want %d", len(results), len(serviceTokens))
	}

	for _, result := range results {
		if result.err != nil {
			t.Fatalf("service %s error = %v, want nil", result.serviceName, result.err)
		}
		if !result.added {
			t.Fatalf("service %s added = false, want true", result.serviceName)
		}
	}

	metricMu.Lock()
	observedMax := maxActive
	metricMu.Unlock()
	if observedMax > 2 {
		t.Fatalf("max concurrent add workers = %d, want <=2", observedMax)
	}
}

func TestAdd_BoundedConcurrencyHonorsConfig(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskID := "IN-ADD-CONC"
	taskDir := filepath.Join(tasksRoot, taskID)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("setup: create task dir: %v", err)
	}

	serviceNames := []string{"svc-a", "svc-b", "svc-c"}
	for _, name := range serviceNames {
		repoDir := filepath.Join(rootDir, name)
		if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755); err != nil {
			t.Fatalf("setup: create repo dir %s: %v", repoDir, err)
		}
	}

	started := make(chan struct{}, len(serviceNames))
	continueAdd := make(chan struct{})

	var metricMu sync.Mutex
	active := 0
	maxActive := 0

	gitMock := &mockGitClient{
		listWorktreesFn: func(_ string) ([]git.WorktreeEntry, error) {
			return nil, nil
		},
		baseBranchResult: "develop",
		branchExistsRes:  false,
		addWorktreeFn: func(_, _, _ string, _ bool, _ string) error {
			metricMu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			metricMu.Unlock()

			started <- struct{}{}
			<-continueAdd

			metricMu.Lock()
			active--
			metricMu.Unlock()
			return nil
		},
	}

	cfg := &config.Config{TasksRoot: tasksRoot, RootDir: rootDir, BranchPrefix: "feature/", BaseBranch: "develop", Editor: "code", Concurrency: 1}
	if _, err := cfg.Effective(); err != nil {
		t.Fatalf("cfg.Effective(): %v", err)
	}
	mgr := newTestManagerWithCfg(t, cfg, gitMock)

	done := make(chan error, 1)
	go func() {
		_, err := mgr.Add(context.Background(), AddParams{TaskID: taskID, Services: serviceNames})
		done <- err
	}()

	for range len(serviceNames) {
		<-started
		continueAdd <- struct{}{}
	}

	if err := <-done; err != nil {
		t.Fatalf("Add returned error: %v", err)
	}

	metricMu.Lock()
	observedMax := maxActive
	metricMu.Unlock()
	if observedMax > 1 {
		t.Fatalf("max concurrent Add workers = %d, want <=1", observedMax)
	}
}

func TestGitCache_MutateReturnedSlice_SecondCallUnaffected(t *testing.T) {
	cache := newGitCache()
	repoPath := "/repo/mutate"

	var calls int
	mock := &mockGitClient{
		listWorktreesFn: func(receivedRepoPath string) ([]git.WorktreeEntry, error) {
			if receivedRepoPath != repoPath {
				t.Fatalf("repoPath = %q, want %q", receivedRepoPath, repoPath)
			}
			calls++
			return []git.WorktreeEntry{{Path: "/wt/one", Branch: "refs/heads/feature/one"}}, nil
		},
	}

	first, err := cache.listWorktrees(context.Background(), mock, repoPath)
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("len(first) = %d, want 1", len(first))
	}

	first[0].Path = "/wt/mutated"

	second, err := cache.listWorktrees(context.Background(), mock, repoPath)
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if len(second) != 1 {
		t.Fatalf("len(second) = %d, want 1", len(second))
	}
	if second[0].Path == "/wt/mutated" {
		t.Fatalf("cached slice mutated by caller: got %q", second[0].Path)
	}
	if second[0].Path != "/wt/one" {
		t.Fatalf("second path = %q, want %q", second[0].Path, "/wt/one")
	}
	if calls != 1 {
		t.Fatalf("ListWorktrees calls = %d, want 1", calls)
	}
}

func TestGitCache_RaceClean(t *testing.T) {
	cache := newGitCache()
	repoPath := "/repo/race"

	mock := &mockGitClient{
		listWorktreesFn: func(receivedRepoPath string) ([]git.WorktreeEntry, error) {
			return []git.WorktreeEntry{
				{Path: "/wt/a", Branch: "refs/heads/feature/a"},
				{Path: "/wt/b", Branch: "refs/heads/feature/b"},
			}, nil
		},
	}

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			entries, err := cache.listWorktrees(context.Background(), mock, repoPath)
			if err != nil {
				t.Errorf("listWorktrees error: %v", err)
				return
			}
			if len(entries) != 2 {
				t.Errorf("len(entries) = %d, want 2", len(entries))
			}
			if len(entries) > 0 {
				entries[0].Path = "/wt/raced"
			}
		}()
	}
	wg.Wait()
}

func TestSyncTask_BoundedConcurrencyHonorsConfig(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskID := "IN-SYNC-CONC"
	taskDir := filepath.Join(tasksRoot, taskID)

	serviceNames := []string{"svc-a", "svc-b", "svc-c"}
	servicePaths := make(map[string]string, len(serviceNames))
	fakeCommonByWorktree := make(map[string]string, len(serviceNames))
	for _, name := range serviceNames {
		worktreePath := filepath.Join(taskDir, name)
		if err := os.MkdirAll(worktreePath, 0o755); err != nil {
			t.Fatalf("setup: create service dir %s: %v", name, err)
		}
		servicePaths[name] = worktreePath
		commonDir := filepath.Join(rootDir, "repos", name, ".git")
		if err := os.MkdirAll(commonDir, 0o755); err != nil {
			t.Fatalf("setup: create common dir %s: %v", commonDir, err)
		}
		fakeCommonByWorktree[worktreePath] = commonDir
	}

	started := make(chan struct{}, len(serviceNames))
	continueFetch := make(chan struct{})

	var metricMu sync.Mutex
	active := 0
	maxActive := 0

	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			commonDir, ok := fakeCommonByWorktree[path]
			if !ok {
				return "", errors.New("not a git worktree")
			}
			return commonDir, nil
		},
		listWorktreesRes: []git.WorktreeEntry{
			{Path: servicePaths["svc-a"], Branch: "refs/heads/feature/IN-SYNC-CONC"},
			{Path: servicePaths["svc-b"], Branch: "refs/heads/feature/IN-SYNC-CONC"},
			{Path: servicePaths["svc-c"], Branch: "refs/heads/feature/IN-SYNC-CONC"},
		},
		fetchFn: func(_ string) error {
			metricMu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			metricMu.Unlock()

			started <- struct{}{}
			<-continueFetch

			metricMu.Lock()
			active--
			metricMu.Unlock()

			return nil
		},
		revListAheadBehindFn: func(_, _ string) (int, int, error) {
			return 0, 1, nil
		},
	}

	cfg := &config.Config{TasksRoot: tasksRoot, RootDir: rootDir, BranchPrefix: "feature/", BaseBranch: "develop", Editor: "code", Concurrency: 1}
	if _, err := cfg.Effective(); err != nil {
		t.Fatalf("cfg.Effective(): %v", err)
	}
	mgr := newTestManagerWithCfg(t, cfg, gitMock)

	lineCh := make(chan string, 64)
	done := make(chan error, 1)
	go func() {
		done <- mgr.SyncTask(context.Background(), taskID, SyncStrategyMerge, lineCh)
	}()

	for range len(serviceNames) {
		<-started
		continueFetch <- struct{}{}
	}

	if err := <-done; err != nil {
		t.Fatalf("SyncTask returned error: %v", err)
	}
	for range lineCh {
	}

	metricMu.Lock()
	observedMax := maxActive
	metricMu.Unlock()
	if observedMax > 1 {
		t.Fatalf("max concurrent sync workers = %d, want <=1", observedMax)
	}
}
