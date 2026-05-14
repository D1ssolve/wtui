package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"log/slog"

	"github.com/diss0x/wtui/internal/config"
	"github.com/diss0x/wtui/internal/discovery"
	"github.com/diss0x/wtui/internal/domain"
	"github.com/diss0x/wtui/internal/dotnet"
	"github.com/diss0x/wtui/internal/git"
	"github.com/diss0x/wtui/internal/sln"
)

type fakeRepoResolver struct {
	findAllRepos []domain.Repo
	refreshRepos []domain.Repo
	findAllCalls int
	refreshCalls int
}

type fakeFindAllRepoResolver struct {
	findAllRepos []domain.Repo
	findAllCalls int
}

func (f *fakeRepoResolver) Resolve(_ context.Context, _ string) (string, error) { return "", nil }

func (f *fakeRepoResolver) FindAll(_ context.Context) ([]domain.Repo, error) {
	f.findAllCalls++
	return f.findAllRepos, nil
}

func (f *fakeRepoResolver) Refresh(_ context.Context) ([]domain.Repo, error) {
	f.refreshCalls++
	return f.refreshRepos, nil
}

func (f *fakeFindAllRepoResolver) Resolve(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (f *fakeFindAllRepoResolver) FindAll(_ context.Context) ([]domain.Repo, error) {
	f.findAllCalls++
	return f.findAllRepos, nil
}

type mockGitClient struct {
	mu sync.Mutex

	isValidRepoErr   error
	baseBranchResult string
	baseBranchErr    error
	branchExistsRes  bool
	branchExistsErr  error

	remoteBranchExistsRes bool
	remoteBranchExistsErr error

	remoteBranchExistsFn func(repoPath, branch string) (bool, error)
	listWorktreesRes     []git.WorktreeEntry
	listWorktreesErr     error
	addWorktreeErr       error
	commonDirResult      string
	commonDirErr         error

	commonDirFn          func(path string) (string, error)
	removeWorktreeErr    error
	isDirtyRes           bool
	isDirtyErr           error
	fetchErr             error
	rebaseErr            error
	mergeErr             error
	pushErr              error
	fetchFn              func(path string) error
	rebaseFn             func(path, upstream string) error
	mergeFn              func(path, branch string) error
	pushFn               func(path string, lineCh chan<- string) error
	stashErr             error
	versionMajor         int
	versionMinor         int
	versionErr           error
	isDirtyFn            func(path string) (bool, error)
	revListAheadBehindFn func(path, originBranch string) (int, int, error)

	addWorktreeWithTrackingErr error

	addWorktreeCalls             []addWorktreeCall
	addWorktreeWithTrackingCalls []addWorktreeWithTrackingCall
	removeWorktreeCalls          []removeWorktreeCall
	fetchCalls                   []string
	rebaseCalls                  []rebaseCall
	mergeCalls                   []mergeCall
	pushCalls                    []string
	stashCalls                   []stashCall
}

type addWorktreeCall struct {
	RepoPath  string
	Dest      string
	Branch    string
	NewBranch bool
	Base      string
}

type addWorktreeWithTrackingCall struct {
	RepoPath     string
	Dest         string
	LocalBranch  string
	RemoteBranch string
}

type removeWorktreeCall struct {
	CommonDir    string
	WorktreePath string
	Force        bool
}

type stashCall struct {
	WorktreePath string
	Pop          bool
}

type rebaseCall struct {
	WorktreePath string
	Upstream     string
}

type mergeCall struct {
	WorktreePath string
	Branch       string
}

func (m *mockGitClient) IsValidRepo(_ context.Context, _ string) error {
	return m.isValidRepoErr
}

func (m *mockGitClient) BaseBranch(_ context.Context, _ string) (string, error) {
	return m.baseBranchResult, m.baseBranchErr
}

func (m *mockGitClient) BranchExists(_ context.Context, _, _ string) (bool, error) {
	return m.branchExistsRes, m.branchExistsErr
}

func (m *mockGitClient) ListWorktrees(_ context.Context, _ string) ([]git.WorktreeEntry, error) {
	return m.listWorktreesRes, m.listWorktreesErr
}

func (m *mockGitClient) AddWorktree(_ context.Context, repoPath, dest, branch string, newBranch bool, base string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addWorktreeCalls = append(m.addWorktreeCalls, addWorktreeCall{
		RepoPath:  repoPath,
		Dest:      dest,
		Branch:    branch,
		NewBranch: newBranch,
		Base:      base,
	})
	return m.addWorktreeErr
}

func (m *mockGitClient) CommonDir(_ context.Context, path string) (string, error) {
	if m.commonDirFn != nil {
		return m.commonDirFn(path)
	}
	return m.commonDirResult, m.commonDirErr
}

func (m *mockGitClient) RemoveWorktree(_ context.Context, commonDir, worktreePath string, force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeWorktreeCalls = append(m.removeWorktreeCalls, removeWorktreeCall{
		CommonDir:    commonDir,
		WorktreePath: worktreePath,
		Force:        force,
	})
	return m.removeWorktreeErr
}

func (m *mockGitClient) IsDirty(_ context.Context, path string) (bool, error) {
	if m.isDirtyFn != nil {
		return m.isDirtyFn(path)
	}
	return m.isDirtyRes, m.isDirtyErr
}

func (m *mockGitClient) Version(_ context.Context) (int, int, error) {
	return m.versionMajor, m.versionMinor, m.versionErr
}

func (m *mockGitClient) RevListCount(_ context.Context, _, _, _ string) (int, error) {
	return 0, nil
}

func (m *mockGitClient) RevListAheadBehind(_ context.Context, path, originBranch string) (int, int, error) {
	if m.revListAheadBehindFn != nil {
		return m.revListAheadBehindFn(path, originBranch)
	}
	return 0, 0, nil
}

func (m *mockGitClient) Fetch(_ context.Context, path string) error {
	m.mu.Lock()
	m.fetchCalls = append(m.fetchCalls, path)
	m.mu.Unlock()

	if m.fetchFn != nil {
		return m.fetchFn(path)
	}

	return m.fetchErr
}

func (m *mockGitClient) Rebase(_ context.Context, path, upstream string) error {
	m.mu.Lock()
	m.rebaseCalls = append(m.rebaseCalls, rebaseCall{WorktreePath: path, Upstream: upstream})
	m.mu.Unlock()

	if m.rebaseFn != nil {
		return m.rebaseFn(path, upstream)
	}

	return m.rebaseErr
}

func (m *mockGitClient) Merge(_ context.Context, path, branch string) error {
	m.mu.Lock()
	m.mergeCalls = append(m.mergeCalls, mergeCall{WorktreePath: path, Branch: branch})
	m.mu.Unlock()

	if m.mergeFn != nil {
		return m.mergeFn(path, branch)
	}

	return m.mergeErr
}

func (m *mockGitClient) Push(_ context.Context, path string, lineCh chan<- string) error {
	m.mu.Lock()
	m.pushCalls = append(m.pushCalls, path)
	m.mu.Unlock()

	if m.pushFn != nil {
		return m.pushFn(path, lineCh)
	}

	return m.pushErr
}

func (m *mockGitClient) Stash(_ context.Context, worktreePath string, pop bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stashCalls = append(m.stashCalls, stashCall{
		WorktreePath: worktreePath,
		Pop:          pop,
	})
	return m.stashErr
}

func (m *mockGitClient) GetWorktreeBranch(_ context.Context, _ string) (string, error) {
	return "", nil
}

func (m *mockGitClient) DeleteBranch(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockGitClient) RemoteBranchExists(_ context.Context, repoPath, branch string) (bool, error) {
	if m.remoteBranchExistsFn != nil {
		return m.remoteBranchExistsFn(repoPath, branch)
	}
	return m.remoteBranchExistsRes, m.remoteBranchExistsErr
}

func (m *mockGitClient) AddWorktreeWithTracking(_ context.Context, repoPath, dest, localBranch, remoteBranch string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addWorktreeWithTrackingCalls = append(m.addWorktreeWithTrackingCalls, addWorktreeWithTrackingCall{
		RepoPath:     repoPath,
		Dest:         dest,
		LocalBranch:  localBranch,
		RemoteBranch: remoteBranch,
	})
	return m.addWorktreeWithTrackingErr
}

type mockDotnetClient struct{}

func (m *mockDotnetClient) IsAvailable(_ context.Context) bool { return false }
func (m *mockDotnetClient) NewSln(_ context.Context, _, _ string) error {
	return errors.New("dotnet not available in tests")
}
func (m *mockDotnetClient) SlnAdd(_ context.Context, _, _, _ string) error {
	return errors.New("dotnet not available in tests")
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
}

func newTestManager(t *testing.T, tasksRoot, rootDir string, gitMock *mockGitClient) Manager {
	t.Helper()

	cfg := &config.Config{
		TasksRoot:    tasksRoot,
		RootDir:      rootDir,
		BranchPrefix: "feature/",
		Editor:       "code",
	}
	cfg.Effective()

	cfg.TasksRoot = tasksRoot
	cfg.RootDir = rootDir

	logger := newTestLogger()
	disc := discovery.New(cfg, gitMock, logger)
	slnMgr := sln.NewManager(&mockDotnetClient{}, logger)

	return New(cfg, gitMock, disc, slnMgr, logger)
}

func newTestManagerWithCfg(t *testing.T, cfg *config.Config, gitMock *mockGitClient) Manager {
	t.Helper()

	logger := newTestLogger()
	disc := discovery.New(cfg, gitMock, logger)
	slnMgr := sln.NewManager(&mockDotnetClient{}, logger)

	return New(cfg, gitMock, disc, slnMgr, logger)
}

func makeGitDir(t *testing.T, repoDir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755); err != nil {
		t.Fatalf("makeGitDir: %v", err)
	}
}

func TestInit_CreatesDirAndCallsAddWorktree(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	svcRepo := filepath.Join(rootDir, "myservice")
	makeGitDir(t, svcRepo)

	gitMock := &mockGitClient{
		isValidRepoErr:   nil,
		baseBranchResult: "main",
		branchExistsRes:  false,
		listWorktreesRes: nil,
	}

	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	params := InitParams{
		TaskID:       "IN-001",
		Services:     []string{"myservice"},
		BranchPrefix: "feature/",
	}

	if err := mgr.Init(context.Background(), params); err != nil {
		t.Fatalf("Init returned unexpected error: %v", err)
	}

	taskDir := filepath.Join(tasksRoot, "IN-001")
	if _, err := os.Stat(taskDir); err != nil {
		t.Fatalf("task directory not created: %v", err)
	}

	gitMock.mu.Lock()
	calls := gitMock.addWorktreeCalls
	gitMock.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 AddWorktree call, got %d", len(calls))
	}

	call := calls[0]
	wantBranch := "feature/IN-001"
	wantDest := filepath.Join(taskDir, "myservice")

	if call.Branch != wantBranch {
		t.Errorf("AddWorktree branch = %q, want %q", call.Branch, wantBranch)
	}
	if call.Dest != wantDest {
		t.Errorf("AddWorktree dest = %q, want %q", call.Dest, wantDest)
	}
	if !call.NewBranch {
		t.Errorf("AddWorktree newBranch = false, want true (branch did not exist)")
	}
	if call.Base != "main" {
		t.Errorf("AddWorktree base = %q, want %q", call.Base, "main")
	}
}

func TestInit_ErrTaskExists(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskDir := filepath.Join(tasksRoot, "IN-002")

	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("setup: create task dir: %v", err)
	}

	gitMock := &mockGitClient{}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	err := mgr.Init(context.Background(), InitParams{TaskID: "IN-002"})
	if !errors.Is(err, ErrTaskExists) {
		t.Errorf("Init error = %v, want ErrTaskExists", err)
	}
}

func TestInit_ContinuesWhenServiceNotFound(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	gitMock := &mockGitClient{
		isValidRepoErr: errors.New("not a valid repo"),
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	err := mgr.Init(context.Background(), InitParams{
		TaskID:   "IN-003",
		Services: []string{"nonexistent"},
	})

	if err == nil {
		t.Fatal("Init returned nil, want error when all services fail to resolve")
	}

	taskDir := filepath.Join(tasksRoot, "IN-003")
	if _, statErr := os.Stat(taskDir); !os.IsNotExist(statErr) {
		t.Errorf("task directory still exists after rollback, want it removed")
	}

	gitMock.mu.Lock()
	n := len(gitMock.addWorktreeCalls)
	gitMock.mu.Unlock()
	if n != 0 {
		t.Errorf("AddWorktree was called %d times, want 0", n)
	}
}

func TestInit_UsesExistingBranch(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	svcRepo := filepath.Join(rootDir, "svcA")
	makeGitDir(t, svcRepo)

	gitMock := &mockGitClient{
		isValidRepoErr:   nil,
		baseBranchResult: "main",
		branchExistsRes:  true,
		listWorktreesRes: nil,
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	if err := mgr.Init(context.Background(), InitParams{
		TaskID:   "IN-004",
		Services: []string{"svcA"},
	}); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	gitMock.mu.Lock()
	calls := gitMock.addWorktreeCalls
	gitMock.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 AddWorktree call, got %d", len(calls))
	}
	if calls[0].NewBranch {
		t.Error("AddWorktree newBranch = true, want false (branch already existed)")
	}
}

func TestAdd_ErrTaskNotFound(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	gitMock := &mockGitClient{}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	err := mgr.Add(context.Background(), AddParams{TaskID: "IN-999", Services: []string{"svc"}})
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("Add error = %v, want ErrTaskNotFound", err)
	}
}

func TestList_EmptyWhenTasksRootMissing(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, "does_not_exist")

	gitMock := &mockGitClient{}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	tasks, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("List returned unexpected error: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("List returned %d tasks, want 0", len(tasks))
	}
}

func TestList_ReturnsSortedTasks(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	for _, id := range []string{"IN-003", "IN-001", "IN-002"} {
		if err := os.MkdirAll(filepath.Join(tasksRoot, id), 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	gitMock := &mockGitClient{}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	tasks, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("List error: %v", err)
	}

	if len(tasks) != 3 {
		t.Fatalf("got %d tasks, want 3", len(tasks))
	}

	wantOrder := []string{"IN-001", "IN-002", "IN-003"}
	for i, want := range wantOrder {
		if tasks[i].ID != want {
			t.Errorf("tasks[%d].ID = %q, want %q", i, tasks[i].ID, want)
		}
	}
}

func TestListServices_ErrTaskNotFound(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	gitMock := &mockGitClient{}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	_, err := mgr.ListServices(context.Background(), "NOTFOUND")
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("ListServices error = %v, want ErrTaskNotFound", err)
	}
}

func TestListServices_SkipsNonGitDirs(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	taskDir := filepath.Join(tasksRoot, "IN-LSS")
	for _, name := range []string{"service-a", "not-a-worktree"} {
		if err := os.MkdirAll(filepath.Join(taskDir, name), 0o755); err != nil {
			t.Fatalf("setup: create subdir %s: %v", name, err)
		}
	}

	fakeCommonDir := filepath.Join(rootDir, "service-a", ".git")
	if err := os.MkdirAll(fakeCommonDir, 0o755); err != nil {
		t.Fatalf("setup: create fakeCommonDir: %v", err)
	}

	gitMock := &mockGitClient{

		commonDirFn: func(path string) (string, error) {
			if filepath.Base(path) == "service-a" {
				return fakeCommonDir, nil
			}
			return "", errors.New("not a git repo")
		},
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	services, err := mgr.ListServices(context.Background(), "IN-LSS")
	if err != nil {
		t.Fatalf("ListServices returned unexpected error: %v", err)
	}

	if len(services) != 1 {
		t.Fatalf("ListServices returned %d services, want 1; got %v", len(services), services)
	}
	if services[0].Name != "service-a" {
		t.Errorf("services[0].Name = %q, want %q", services[0].Name, "service-a")
	}
}

func TestListServices_ChecksDirtyAndAheadBehindConcurrently(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskDir := filepath.Join(tasksRoot, "IN-LSS")
	svcDir := filepath.Join(taskDir, "service-a")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatalf("setup: create service dir: %v", err)
	}

	fakeCommonDir := filepath.Join(rootDir, "service-a", ".git")
	if err := os.MkdirAll(fakeCommonDir, 0o755); err != nil {
		t.Fatalf("setup: create fakeCommonDir: %v", err)
	}

	ready := make(chan string, 2)
	release := make(chan struct{})

	gitMock := &mockGitClient{
		commonDirResult: fakeCommonDir,
		listWorktreesRes: []git.WorktreeEntry{{
			Path:   svcDir,
			Branch: "refs/heads/feature/IN-LSS-service-a",
		}},
		isDirtyFn: func(path string) (bool, error) {
			if path != svcDir {
				t.Errorf("IsDirty path = %q, want %q", path, svcDir)
			}
			ready <- "dirty"
			<-release
			return true, nil
		},
		revListAheadBehindFn: func(path, originBranch string) (int, int, error) {
			if path != svcDir {
				t.Errorf("RevListAheadBehind path = %q, want %q", path, svcDir)
			}
			if originBranch != "origin/feature/IN-LSS-service-a" {
				t.Errorf("RevListAheadBehind origin = %q, want %q", originBranch, "origin/feature/IN-LSS-service-a")
			}
			ready <- "aheadBehind"
			<-release
			return 2, 3, nil
		},
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	done := make(chan []string, 1)
	go func() {
		var calls []string
		for range 2 {
			calls = append(calls, <-ready)
		}
		done <- calls
	}()

	errCh := make(chan error, 1)
	var services []domain.Service
	go func() {
		var err error
		services, err = mgr.ListServices(context.Background(), "IN-LSS")
		errCh <- err
	}()

	select {
	case <-done:
		close(release)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("ListServices did not start IsDirty and RevListAheadBehind concurrently")
	}

	if err := <-errCh; err != nil {
		t.Fatalf("ListServices returned unexpected error: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("ListServices returned %d services, want 1; got %v", len(services), services)
	}
	if !services[0].IsDirty {
		t.Errorf("IsDirty = false, want true")
	}
	if services[0].Ahead != 2 || services[0].Behind != 3 {
		t.Errorf("ahead/behind = %d/%d, want 2/3", services[0].Ahead, services[0].Behind)
	}
}

func TestRemove_CallsGitAndRemovesTaskDir(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	taskDir := filepath.Join(tasksRoot, "IN-010")
	svcDir := filepath.Join(taskDir, "myservice")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	fakeCommonDir := filepath.Join(rootDir, "myservice", ".git")
	if err := os.MkdirAll(fakeCommonDir, 0o755); err != nil {
		t.Fatalf("setup commonDir: %v", err)
	}

	gitMock := &mockGitClient{
		commonDirResult:   fakeCommonDir,
		removeWorktreeErr: nil,
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	if err := mgr.Remove(context.Background(), "IN-010", false, false); err != nil {
		t.Fatalf("Remove returned unexpected error: %v", err)
	}

	if _, err := os.Stat(taskDir); !os.IsNotExist(err) {
		t.Errorf("task directory still exists after Remove")
	}

	gitMock.mu.Lock()
	calls := gitMock.removeWorktreeCalls
	gitMock.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 RemoveWorktree call, got %d", len(calls))
	}
	if calls[0].WorktreePath != svcDir {
		t.Errorf("RemoveWorktree path = %q, want %q", calls[0].WorktreePath, svcDir)
	}
}

func TestRemove_WithoutForce_FailedWorktreePreservesTaskDir(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	taskDir := filepath.Join(tasksRoot, "IN-011")
	svcDir := filepath.Join(taskDir, "dirtyservice")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	fakeCommonDir := filepath.Join(rootDir, "dirtyservice", ".git")
	if err := os.MkdirAll(fakeCommonDir, 0o755); err != nil {
		t.Fatalf("setup commonDir: %v", err)
	}

	dirtyErr := errors.New("worktree is dirty")
	gitMock := &mockGitClient{
		commonDirResult:   fakeCommonDir,
		removeWorktreeErr: dirtyErr,
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	err := mgr.Remove(context.Background(), "IN-011", false, false)
	if err == nil {
		t.Fatal("Remove returned nil, want error for dirty worktree without force")
	}

	if _, statErr := os.Stat(taskDir); statErr != nil {
		t.Errorf("task directory was deleted despite worktree removal error")
	}
}

func TestRemove_WithForce_RemovesTaskDirDespiteErrors(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	taskDir := filepath.Join(tasksRoot, "IN-012")
	svcDir := filepath.Join(taskDir, "forcedservice")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	fakeCommonDir := filepath.Join(rootDir, "forcedservice", ".git")
	if err := os.MkdirAll(fakeCommonDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	gitMock := &mockGitClient{
		commonDirResult:   fakeCommonDir,
		removeWorktreeErr: errors.New("simulated git failure"),
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	if err := mgr.Remove(context.Background(), "IN-012", true, false); err != nil {
		t.Fatalf("Remove(force=true) returned unexpected error: %v", err)
	}

	if _, err := os.Stat(taskDir); !os.IsNotExist(err) {
		t.Errorf("task directory still exists after forced Remove")
	}
}

func TestValidateTaskID(t *testing.T) {
	tests := []struct {
		id      string
		wantErr bool
	}{
		{"IN-6748", false},
		{"PROJ-123", false},
		{"my-task", false},
		{"", true},
		{".", true},
		{"/etc/passwd", true},
		{"task/../etc", true},
		{"task<name", true},
		{"task>name", true},
		{"task:name", true},
		{`task"name`, true},
		{"task|name", true},
		{"task?name", true},
		{"task*name", true},
		{`task\name`, true},
	}

	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			err := validateTaskID(tc.id)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateTaskID(%q) error = %v, wantErr = %v", tc.id, err, tc.wantErr)
			}
		})
	}
}

func TestGenerateWorkspaceFile(t *testing.T) {
	taskDir := t.TempDir()
	taskID := "IN-WS-001"

	for _, name := range []string{"svcB", "svcA"} {
		if err := os.MkdirAll(filepath.Join(taskDir, name), 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	if err := generateWorkspaceFile(taskID, taskDir); err != nil {
		t.Fatalf("generateWorkspaceFile error: %v", err)
	}

	wsPath := filepath.Join(taskDir, taskID+".code-workspace")
	data, err := os.ReadFile(wsPath)
	if err != nil {
		t.Fatalf("read workspace file: %v", err)
	}

	content := string(data)

	if !strings.Contains(content, `"svcA"`) {
		t.Errorf("workspace file missing svcA path: %s", content)
	}
	if !strings.Contains(content, `"svcB"`) {
		t.Errorf("workspace file missing svcB path: %s", content)
	}

	if !strings.Contains(content, `"workbench.editor.labelFormat": "medium"`) {
		t.Errorf("workspace file missing settings: %s", content)
	}
}

func TestBuildServicesFromSubdirs(t *testing.T) {
	taskDir := t.TempDir()

	for _, name := range []string{"svc1", "svc2"} {
		if err := os.MkdirAll(filepath.Join(taskDir, name), 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	if err := os.WriteFile(filepath.Join(taskDir, "some.sln"), []byte(""), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	services := buildServicesFromSubdirs(taskDir)
	if len(services) != 2 {
		t.Fatalf("got %d services, want 2", len(services))
	}
}

func TestList_StaleFlag_NormalTask(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskDir := filepath.Join(tasksRoot, "IN-STALE")

	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	gitMock := &mockGitClient{}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	tasks, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Stale {
		t.Errorf("task.Stale = true for a normal existing task dir, want false")
	}
}

func TestSyncTask_FetchFailureReturnsFirstError(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskDir := filepath.Join(tasksRoot, "IN-SYNC")

	servicePaths := map[string]string{
		"svc-a": filepath.Join(taskDir, "svc-a"),
		"svc-b": filepath.Join(taskDir, "svc-b"),
	}
	for _, path := range servicePaths {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("setup: create service dir %s: %v", path, err)
		}
	}

	fakeCommonDirs := map[string]string{
		servicePaths["svc-a"]: filepath.Join(rootDir, "repos", "svc-a", ".git"),
		servicePaths["svc-b"]: filepath.Join(rootDir, "repos", "svc-b", ".git"),
	}
	for _, path := range fakeCommonDirs {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("setup: create fake common dir %s: %v", path, err)
		}
	}

	fetchErr := errors.New("fetch failed")
	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			commonDir, ok := fakeCommonDirs[path]
			if !ok {
				return "", errors.New("not a git worktree")
			}
			return commonDir, nil
		},
		listWorktreesRes: []git.WorktreeEntry{
			{Path: servicePaths["svc-a"], Branch: "refs/heads/feature/IN-SYNC"},
			{Path: servicePaths["svc-b"], Branch: "refs/heads/feature/IN-SYNC"},
		},
		fetchFn: func(path string) error {
			if filepath.Base(path) == "svc-b" {
				return fetchErr
			}
			return nil
		},
	}
	gitMock.listWorktreesRes = []git.WorktreeEntry{
		{Path: servicePaths["svc-a"], Branch: "refs/heads/feature/IN-SYNC"},
		{Path: servicePaths["svc-b"], Branch: "refs/heads/feature/IN-SYNC"},
	}

	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)
	lineCh := make(chan string, 16)

	err := mgr.SyncTask(context.Background(), "IN-SYNC", SyncStrategyRebase, lineCh)
	if !errors.Is(err, fetchErr) {
		t.Fatalf("SyncTask error = %v, want %v", err, fetchErr)
	}

	var lines []string
	for line := range lineCh {
		lines = append(lines, line)
	}

	assertContainsLine(t, lines, "[svc-a] fetching...")
	assertContainsLine(t, lines, "[svc-a] rebasing onto origin/develop...")
	assertContainsLine(t, lines, "[svc-a] done.")
	assertContainsLine(t, lines, "[svc-b] fetching...")
	assertContainsLine(t, lines, "[svc-b] fetch error: fetch failed")

	gitMock.mu.Lock()
	fetchCalls := append([]string(nil), gitMock.fetchCalls...)
	rebaseCalls := append([]rebaseCall(nil), gitMock.rebaseCalls...)
	gitMock.mu.Unlock()

	if len(fetchCalls) != 2 {
		t.Fatalf("Fetch call count = %d, want 2", len(fetchCalls))
	}
	if len(rebaseCalls) != 1 {
		t.Fatalf("Rebase call count = %d, want 1", len(rebaseCalls))
	}
	if rebaseCalls[0].WorktreePath != servicePaths["svc-a"] {
		t.Errorf("Rebase worktree path = %q, want %q", rebaseCalls[0].WorktreePath, servicePaths["svc-a"])
	}
	if rebaseCalls[0].Upstream != "origin/develop" {
		t.Errorf("Rebase upstream = %q, want %q", rebaseCalls[0].Upstream, "origin/develop")
	}
}

func TestSyncTask_RebasesOntoConfigBaseBranch(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskDir := filepath.Join(tasksRoot, "IN-BB")

	servicePaths := map[string]string{
		"svc-x": filepath.Join(taskDir, "svc-x"),
		"svc-y": filepath.Join(taskDir, "svc-y"),
	}
	for _, path := range servicePaths {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("setup: create service dir %s: %v", path, err)
		}
	}

	fakeCommonDirs := map[string]string{
		servicePaths["svc-x"]: filepath.Join(rootDir, "repos", "svc-x", ".git"),
		servicePaths["svc-y"]: filepath.Join(rootDir, "repos", "svc-y", ".git"),
	}
	for _, path := range fakeCommonDirs {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("setup: create fake common dir %s: %v", path, err)
		}
	}

	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			commonDir, ok := fakeCommonDirs[path]
			if !ok {
				return "", errors.New("not a git worktree")
			}
			return commonDir, nil
		},
		listWorktreesRes: []git.WorktreeEntry{
			{Path: servicePaths["svc-x"], Branch: "refs/heads/feature/IN-BB"},
			{Path: servicePaths["svc-y"], Branch: "refs/heads/feature/IN-BB"},
		},
	}

	cfg := &config.Config{
		TasksRoot:    tasksRoot,
		RootDir:      rootDir,
		BranchPrefix: "feature/",
		BaseBranch:   "develop",
		Editor:       "code",
	}
	cfg.Effective()
	cfg.TasksRoot = tasksRoot
	cfg.RootDir = rootDir
	cfg.BaseBranch = "develop"

	mgr := newTestManagerWithCfg(t, cfg, gitMock)
	lineCh := make(chan string, 32)

	if err := mgr.SyncTask(context.Background(), "IN-BB", SyncStrategyRebase, lineCh); err != nil {
		t.Fatalf("SyncTask returned unexpected error: %v", err)
	}

	for range lineCh {
	}

	gitMock.mu.Lock()
	rebaseCalls := append([]rebaseCall(nil), gitMock.rebaseCalls...)
	gitMock.mu.Unlock()

	if len(rebaseCalls) != 2 {
		t.Fatalf("Rebase call count = %d, want 2", len(rebaseCalls))
	}
	for _, call := range rebaseCalls {
		if call.Upstream != "origin/develop" {
			t.Errorf("Rebase upstream = %q, want %q", call.Upstream, "origin/develop")
		}
	}
}

func TestSyncTask_RebasesOntoMainWhenBaseBranchEmpty(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskDir := filepath.Join(tasksRoot, "IN-EMPTY-BB")

	svcPath := filepath.Join(taskDir, "svc-z")
	if err := os.MkdirAll(svcPath, 0o755); err != nil {
		t.Fatalf("setup: create service dir: %v", err)
	}

	fakeCommonDir := filepath.Join(rootDir, "repos", "svc-z", ".git")
	if err := os.MkdirAll(fakeCommonDir, 0o755); err != nil {
		t.Fatalf("setup: create fake common dir: %v", err)
	}

	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			if path == svcPath {
				return fakeCommonDir, nil
			}
			return "", errors.New("not a git worktree")
		},
		listWorktreesRes: []git.WorktreeEntry{
			{Path: svcPath, Branch: "refs/heads/feature/IN-EMPTY-BB"},
		},
	}

	cfg := &config.Config{
		TasksRoot:    tasksRoot,
		RootDir:      rootDir,
		BranchPrefix: "feature/",
		BaseBranch:   "",
		Editor:       "code",
	}

	cfg.Effective()
	cfg.TasksRoot = tasksRoot
	cfg.RootDir = rootDir
	cfg.BaseBranch = ""

	mgr := newTestManagerWithCfg(t, cfg, gitMock)
	lineCh := make(chan string, 16)

	if err := mgr.SyncTask(context.Background(), "IN-EMPTY-BB", SyncStrategyRebase, lineCh); err != nil {
		t.Fatalf("SyncTask returned unexpected error: %v", err)
	}

	for range lineCh {
	}

	gitMock.mu.Lock()
	rebaseCalls := append([]rebaseCall(nil), gitMock.rebaseCalls...)
	gitMock.mu.Unlock()

	if len(rebaseCalls) != 1 {
		t.Fatalf("Rebase call count = %d, want 1", len(rebaseCalls))
	}
	if rebaseCalls[0].Upstream != "origin/develop" {
		t.Errorf("Rebase upstream = %q, want %q (empty BaseBranch must fall back to develop)",
			rebaseCalls[0].Upstream, "origin/develop")
	}
}

func TestPushService_StreamsLines(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	worktreePath := filepath.Join(tasksRoot, "IN-PUSH", "svcA")
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	gitMock := &mockGitClient{
		pushFn: func(path string, lineCh chan<- string) error {
			lineCh <- "Enumerating objects: 3, done."
			lineCh <- "To origin/main"
			return nil
		},
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	lineCh := make(chan string, 8)
	err := mgr.PushService(context.Background(), "IN-PUSH", "svcA", lineCh)
	if err != nil {
		t.Fatalf("PushService returned unexpected error: %v", err)
	}

	var lines []string
	for i := 0; i < 4; i++ {
		lines = append(lines, <-lineCh)
	}

	wantLines := []string{
		"[svcA] pushing...",
		"Enumerating objects: 3, done.",
		"To origin/main",
		"[svcA] pushed.",
	}
	for i, want := range wantLines {
		if lines[i] != want {
			t.Errorf("line %d = %q, want %q", i, lines[i], want)
		}
	}

	gitMock.mu.Lock()
	pushCalls := append([]string(nil), gitMock.pushCalls...)
	gitMock.mu.Unlock()

	if len(pushCalls) != 1 {
		t.Fatalf("expected 1 Push call, got %d", len(pushCalls))
	}
	if pushCalls[0] != worktreePath {
		t.Errorf("Push path = %q, want %q", pushCalls[0], worktreePath)
	}
}

func TestPushService_ErrServiceNotFound(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskDir := filepath.Join(tasksRoot, "IN-PUSH-MISSING")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})

	lineCh := make(chan string, 1)
	err := mgr.PushService(context.Background(), "IN-PUSH-MISSING", "svcA", lineCh)
	if !errors.Is(err, ErrServiceNotFound) {
		t.Errorf("PushService error = %v, want ErrServiceNotFound", err)
	}
}

func TestStashService_Stash(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	worktreePath := filepath.Join(tasksRoot, "IN-STASH", "svcA")
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	gitMock := &mockGitClient{}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	if err := mgr.StashService(context.Background(), "IN-STASH", "svcA", false); err != nil {
		t.Fatalf("StashService returned unexpected error: %v", err)
	}

	gitMock.mu.Lock()
	calls := gitMock.stashCalls
	gitMock.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 Stash call, got %d", len(calls))
	}
	if calls[0].WorktreePath != worktreePath {
		t.Errorf("Stash worktreePath = %q, want %q", calls[0].WorktreePath, worktreePath)
	}
	if calls[0].Pop {
		t.Errorf("Stash pop = true, want false")
	}
}

func TestStashService_Pop(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	worktreePath := filepath.Join(tasksRoot, "IN-POP", "svcA")
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	gitMock := &mockGitClient{}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	if err := mgr.StashService(context.Background(), "IN-POP", "svcA", true); err != nil {
		t.Fatalf("StashService returned unexpected error: %v", err)
	}

	gitMock.mu.Lock()
	calls := gitMock.stashCalls
	gitMock.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 Stash call, got %d", len(calls))
	}
	if calls[0].WorktreePath != worktreePath {
		t.Errorf("Stash worktreePath = %q, want %q", calls[0].WorktreePath, worktreePath)
	}
	if !calls[0].Pop {
		t.Errorf("Stash pop = false, want true")
	}
}

func TestStashService_ErrServiceNotFound(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskDir := filepath.Join(tasksRoot, "IN-MISSING-SVC")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})

	err := mgr.StashService(context.Background(), "IN-MISSING-SVC", "svcA", false)
	if !errors.Is(err, ErrServiceNotFound) {
		t.Errorf("StashService error = %v, want ErrServiceNotFound", err)
	}
}

var _ git.Client = (*mockGitClient)(nil)

func TestManagerReposCachedUsesFindAll(t *testing.T) {
	resolver := &fakeRepoResolver{findAllRepos: []domain.Repo{{Name: "api", Path: "/repo/api"}}}
	mgr := New(&config.Config{}, &mockGitClient{}, resolver, nil, slog.Default())

	got, err := mgr.Repos(context.Background(), false)
	if err != nil {
		t.Fatalf("Repos(false): %v", err)
	}
	if resolver.findAllCalls != 1 || resolver.refreshCalls != 0 {
		t.Fatalf("calls findAll=%d refresh=%d", resolver.findAllCalls, resolver.refreshCalls)
	}
	if len(got) != 1 || got[0].Name != "api" {
		t.Fatalf("repos = %#v", got)
	}
}

func TestManagerReposForcedUsesRefreshWhenAvailable(t *testing.T) {
	resolver := &fakeRepoResolver{refreshRepos: []domain.Repo{{Name: "fresh", Path: "/repo/fresh"}}}
	mgr := New(&config.Config{}, &mockGitClient{}, resolver, nil, slog.Default())

	got, err := mgr.Repos(context.Background(), true)
	if err != nil {
		t.Fatalf("Repos(true): %v", err)
	}
	if resolver.findAllCalls != 0 || resolver.refreshCalls != 1 {
		t.Fatalf("calls findAll=%d refresh=%d", resolver.findAllCalls, resolver.refreshCalls)
	}
	if len(got) != 1 || got[0].Name != "fresh" {
		t.Fatalf("repos = %#v", got)
	}
}

func TestManagerReposForcedFallsBackToFindAll(t *testing.T) {
	resolver := &fakeFindAllRepoResolver{findAllRepos: []domain.Repo{{Name: "cached", Path: "/repo/cached"}}}
	mgr := New(&config.Config{}, &mockGitClient{}, resolver, nil, slog.Default())

	got, err := mgr.Repos(context.Background(), true)
	if err != nil {
		t.Fatalf("Repos(true): %v", err)
	}
	if resolver.findAllCalls != 1 {
		t.Fatalf("findAll calls = %d, want 1", resolver.findAllCalls)
	}
	if len(got) != 1 || got[0].Name != "cached" {
		t.Fatalf("repos = %#v", got)
	}
}

func TestSyncStrategy_String(t *testing.T) {
	tests := []struct {
		strategy SyncStrategy
		want     string
	}{
		{SyncStrategyMerge, "merge"},
		{SyncStrategyRebase, "rebase"},
		{SyncStrategyNoop, "noop"},
		{SyncStrategy(99), "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.strategy.String(); got != tc.want {
				t.Errorf("SyncStrategy(%d).String() = %q, want %q", tc.strategy, got, tc.want)
			}
		})
	}
}

func TestSyncTask_NoopStrategyDoesNotFetchOrMergeOrRebase(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskDir := filepath.Join(tasksRoot, "IN-NOOP")
	svcPath := filepath.Join(taskDir, "svc-noop")
	if err := os.MkdirAll(svcPath, 0o755); err != nil {
		t.Fatalf("setup: create service dir: %v", err)
	}

	gitMock := &mockGitClient{}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)
	lineCh := make(chan string, 16)

	if err := mgr.SyncTask(context.Background(), "IN-NOOP", SyncStrategyNoop, lineCh); err != nil {
		t.Fatalf("SyncTask returned unexpected error: %v", err)
	}

	var lines []string
	for line := range lineCh {
		lines = append(lines, line)
	}

	assertContainsLine(t, lines, "sync skipped.")

	gitMock.mu.Lock()
	fetchCalls := append([]string(nil), gitMock.fetchCalls...)
	mergeCalls := append([]mergeCall(nil), gitMock.mergeCalls...)
	rebaseCalls := append([]rebaseCall(nil), gitMock.rebaseCalls...)
	gitMock.mu.Unlock()

	if len(fetchCalls) != 0 {
		t.Fatalf("Fetch call count = %d, want 0", len(fetchCalls))
	}
	if len(mergeCalls) != 0 {
		t.Fatalf("Merge call count = %d, want 0", len(mergeCalls))
	}
	if len(rebaseCalls) != 0 {
		t.Fatalf("Rebase call count = %d, want 0", len(rebaseCalls))
	}
}

func TestSyncTask_MergesWithMergeStrategy(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskDir := filepath.Join(tasksRoot, "IN-MERGE")

	svcPath := filepath.Join(taskDir, "svc-merge")
	if err := os.MkdirAll(svcPath, 0o755); err != nil {
		t.Fatalf("setup: create service dir: %v", err)
	}

	fakeCommonDir := filepath.Join(rootDir, "repos", "svc-merge", ".git")
	if err := os.MkdirAll(fakeCommonDir, 0o755); err != nil {
		t.Fatalf("setup: create fake common dir: %v", err)
	}

	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			if path == svcPath {
				return fakeCommonDir, nil
			}
			return "", errors.New("not a git worktree")
		},
		listWorktreesRes: []git.WorktreeEntry{
			{Path: svcPath, Branch: "refs/heads/feature/IN-MERGE"},
		},
	}

	cfg := &config.Config{
		TasksRoot:    tasksRoot,
		RootDir:      rootDir,
		BranchPrefix: "feature/",
		BaseBranch:   "main",
		Editor:       "code",
	}
	cfg.Effective()
	cfg.TasksRoot = tasksRoot
	cfg.RootDir = rootDir
	cfg.BaseBranch = "main"

	mgr := newTestManagerWithCfg(t, cfg, gitMock)
	lineCh := make(chan string, 16)

	if err := mgr.SyncTask(context.Background(), "IN-MERGE", SyncStrategyMerge, lineCh); err != nil {
		t.Fatalf("SyncTask returned unexpected error: %v", err)
	}

	for range lineCh {
	}

	gitMock.mu.Lock()
	mergeCalls := append([]mergeCall(nil), gitMock.mergeCalls...)
	rebaseCalls := append([]rebaseCall(nil), gitMock.rebaseCalls...)
	gitMock.mu.Unlock()

	if len(mergeCalls) != 1 {
		t.Fatalf("Merge call count = %d, want 1", len(mergeCalls))
	}
	if mergeCalls[0].WorktreePath != svcPath {
		t.Errorf("Merge worktree path = %q, want %q", mergeCalls[0].WorktreePath, svcPath)
	}
	if mergeCalls[0].Branch != "origin/main" {
		t.Errorf("Merge branch = %q, want %q", mergeCalls[0].Branch, "origin/main")
	}

	if len(rebaseCalls) != 0 {
		t.Errorf("Rebase call count = %d, want 0 (merge strategy should not call rebase)", len(rebaseCalls))
	}
}

func TestSyncTask_RebasesWithRebaseStrategy(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskDir := filepath.Join(tasksRoot, "IN-REBASE")

	svcPath := filepath.Join(taskDir, "svc-rebase")
	if err := os.MkdirAll(svcPath, 0o755); err != nil {
		t.Fatalf("setup: create service dir: %v", err)
	}

	fakeCommonDir := filepath.Join(rootDir, "repos", "svc-rebase", ".git")
	if err := os.MkdirAll(fakeCommonDir, 0o755); err != nil {
		t.Fatalf("setup: create fake common dir: %v", err)
	}

	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			if path == svcPath {
				return fakeCommonDir, nil
			}
			return "", errors.New("not a git worktree")
		},
		listWorktreesRes: []git.WorktreeEntry{
			{Path: svcPath, Branch: "refs/heads/feature/IN-REBASE"},
		},
	}

	cfg := &config.Config{
		TasksRoot:    tasksRoot,
		RootDir:      rootDir,
		BranchPrefix: "feature/",
		BaseBranch:   "develop",
		Editor:       "code",
	}
	cfg.Effective()
	cfg.TasksRoot = tasksRoot
	cfg.RootDir = rootDir
	cfg.BaseBranch = "develop"

	mgr := newTestManagerWithCfg(t, cfg, gitMock)
	lineCh := make(chan string, 16)

	if err := mgr.SyncTask(context.Background(), "IN-REBASE", SyncStrategyRebase, lineCh); err != nil {
		t.Fatalf("SyncTask returned unexpected error: %v", err)
	}

	for range lineCh {
	}

	gitMock.mu.Lock()
	mergeCalls := append([]mergeCall(nil), gitMock.mergeCalls...)
	rebaseCalls := append([]rebaseCall(nil), gitMock.rebaseCalls...)
	gitMock.mu.Unlock()

	if len(rebaseCalls) != 1 {
		t.Fatalf("Rebase call count = %d, want 1", len(rebaseCalls))
	}
	if rebaseCalls[0].WorktreePath != svcPath {
		t.Errorf("Rebase worktree path = %q, want %q", rebaseCalls[0].WorktreePath, svcPath)
	}
	if rebaseCalls[0].Upstream != "origin/develop" {
		t.Errorf("Rebase upstream = %q, want %q", rebaseCalls[0].Upstream, "origin/develop")
	}

	if len(mergeCalls) != 0 {
		t.Errorf("Merge call count = %d, want 0 (rebase strategy should not call merge)", len(mergeCalls))
	}
}

func TestInit_RollsBackWhenNoWorktreesAdded(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	gitMock := &mockGitClient{
		isValidRepoErr: errors.New("not a valid repo"),
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	err := mgr.Init(context.Background(), InitParams{
		TaskID:   "IN-ROLLBACK",
		Services: []string{"nonexistent"},
	})
	if err == nil {
		t.Fatal("Init returned nil, want error when no worktrees could be added")
	}

	taskDir := filepath.Join(tasksRoot, "IN-ROLLBACK")
	if _, statErr := os.Stat(taskDir); !os.IsNotExist(statErr) {
		t.Errorf("task directory still exists after rollback, want it removed")
	}
}

func TestInit_NoServicesRequested(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})

	if err := mgr.Init(context.Background(), InitParams{TaskID: "IN-EMPTY"}); err != nil {
		t.Fatalf("Init with no services returned unexpected error: %v", err)
	}

	taskDir := filepath.Join(tasksRoot, "IN-EMPTY")
	if _, err := os.Stat(taskDir); err != nil {
		t.Errorf("task directory not created: %v", err)
	}
}

func TestAdd_ReturnsErrorWhenNoWorktreesAdded(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	taskDir := filepath.Join(tasksRoot, "IN-ADD-FAIL")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	gitMock := &mockGitClient{
		isValidRepoErr: errors.New("not a valid repo"),
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	err := mgr.Add(context.Background(), AddParams{
		TaskID:   "IN-ADD-FAIL",
		Services: []string{"nonexistent"},
	})
	if err == nil {
		t.Fatal("Add returned nil, want error when no worktrees could be added")
	}

	if _, statErr := os.Stat(taskDir); statErr != nil {
		t.Errorf("task directory was removed by Add, want it preserved: %v", statErr)
	}
}

func TestInit_RemoteBranchConflict_ReturnsError(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	svcRepo := filepath.Join(rootDir, "myservice")
	makeGitDir(t, svcRepo)

	gitMock := &mockGitClient{
		isValidRepoErr:        nil,
		baseBranchResult:      "main",
		branchExistsRes:       false,
		remoteBranchExistsRes: true,
		listWorktreesRes:      nil,
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	err := mgr.Init(context.Background(), InitParams{
		TaskID:       "IN-CONFLICT",
		Services:     []string{"myservice"},
		BranchPrefix: "feature/",
	})

	if err == nil {
		t.Fatal("Init returned nil, want ErrRemoteBranchConflict")
	}

	var conflictErr *ErrRemoteBranchConflict
	if !errors.As(err, &conflictErr) {
		t.Fatalf("Init error = %v, want ErrRemoteBranchConflict", err)
	}

	if conflictErr.TaskID != "IN-CONFLICT" {
		t.Errorf("TaskID = %q, want %q", conflictErr.TaskID, "IN-CONFLICT")
	}
	if conflictErr.ServiceName != "myservice" {
		t.Errorf("ServiceName = %q, want %q", conflictErr.ServiceName, "myservice")
	}
	if conflictErr.BranchName != "feature/IN-CONFLICT" {
		t.Errorf("BranchName = %q, want %q", conflictErr.BranchName, "feature/IN-CONFLICT")
	}
}

func TestInit_RemoteBranchConflict_ReturnsErrorAfterPartialSuccess(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	for _, service := range []string{"okservice", "conflictservice"} {
		makeGitDir(t, filepath.Join(rootDir, service))
	}

	gitMock := &mockGitClient{
		isValidRepoErr:   nil,
		baseBranchResult: "main",
		branchExistsRes:  false,
		listWorktreesRes: nil,
		remoteBranchExistsFn: func(repoPath, branch string) (bool, error) {
			return filepath.Base(repoPath) == "conflictservice", nil
		},
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	err := mgr.Init(context.Background(), InitParams{
		TaskID:       "IN-PARTIAL-CONFLICT",
		Services:     []string{"okservice", "conflictservice"},
		BranchPrefix: "feature/",
	})

	if err == nil {
		t.Fatal("Init returned nil, want ErrRemoteBranchConflict after partial success")
	}
	var conflictErr *ErrRemoteBranchConflict
	if !errors.As(err, &conflictErr) {
		t.Fatalf("Init error = %v, want ErrRemoteBranchConflict", err)
	}
	if conflictErr.ServiceName != "conflictservice" {
		t.Errorf("ServiceName = %q, want %q", conflictErr.ServiceName, "conflictservice")
	}

	if _, statErr := os.Stat(filepath.Join(tasksRoot, "IN-PARTIAL-CONFLICT")); statErr != nil {
		t.Errorf("task directory not preserved after partial success: %v", statErr)
	}

	gitMock.mu.Lock()
	addCalls := gitMock.addWorktreeCalls
	gitMock.mu.Unlock()
	if len(addCalls) != 1 || addCalls[0].Branch != "feature/IN-PARTIAL-CONFLICT" {
		t.Fatalf("AddWorktree calls = %+v, want one successful service add", addCalls)
	}
}

func TestInit_RemoteBranchConflict_RetryWithStrategySucceeds(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	for _, svc := range []string{"okservice", "conflictservice"} {
		makeGitDir(t, filepath.Join(rootDir, svc))
	}

	gitMock := &mockGitClient{
		isValidRepoErr:   nil,
		baseBranchResult: "main",
		branchExistsRes:  false,
		listWorktreesRes: nil,
		remoteBranchExistsFn: func(repoPath, branch string) (bool, error) {
			return filepath.Base(repoPath) == "conflictservice", nil
		},
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	// First call: okservice succeeds, conflictservice hits remote branch conflict.
	err := mgr.Init(context.Background(), InitParams{
		TaskID:       "IN-RETRY",
		Services:     []string{"okservice", "conflictservice"},
		BranchPrefix: "feature/",
	})
	var conflictErr *ErrRemoteBranchConflict
	if !errors.As(err, &conflictErr) {
		t.Fatalf("first Init expected ErrRemoteBranchConflict, got %v", err)
	}
	if conflictErr.ServiceName != "conflictservice" {
		t.Fatalf("conflictErr.ServiceName = %q, want %q", conflictErr.ServiceName, "conflictservice")
	}

	// Task directory must still exist (partial success preserved it).
	if _, statErr := os.Stat(filepath.Join(tasksRoot, "IN-RETRY")); statErr != nil {
		t.Fatalf("task directory should be preserved after partial success: %v", statErr)
	}

	// Second call: retry with StrategyFetchAndSwitch for conflictservice.
	err = mgr.Init(context.Background(), InitParams{
		TaskID:       "IN-RETRY",
		Services:     []string{"okservice", "conflictservice"},
		BranchPrefix: "feature/",
		RemoteBranchStrategies: map[string]RemoteBranchStrategy{
			"conflictservice": StrategyFetchAndSwitch,
		},
	})
	if err != nil {
		t.Fatalf("retry Init returned unexpected error: %v", err)
	}

	gitMock.mu.Lock()
	trackingCalls := gitMock.addWorktreeWithTrackingCalls
	gitMock.mu.Unlock()
	if len(trackingCalls) != 1 || trackingCalls[0].LocalBranch != "feature/IN-RETRY" {
		t.Fatalf("expected 1 AddWorktreeWithTracking call for conflictservice, got %+v", trackingCalls)
	}
}

func TestInit_RemoteBranchConflict_StrategyFetchAndSwitch(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	svcRepo := filepath.Join(rootDir, "myservice")
	makeGitDir(t, svcRepo)

	gitMock := &mockGitClient{
		isValidRepoErr:        nil,
		baseBranchResult:      "main",
		branchExistsRes:       false,
		remoteBranchExistsRes: true,
		listWorktreesRes:      nil,
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	err := mgr.Init(context.Background(), InitParams{
		TaskID:       "IN-FETCH",
		Services:     []string{"myservice"},
		BranchPrefix: "feature/",
		RemoteBranchStrategies: map[string]RemoteBranchStrategy{
			"myservice": StrategyFetchAndSwitch,
		},
	})
	if err != nil {
		t.Fatalf("Init returned unexpected error: %v", err)
	}

	gitMock.mu.Lock()
	calls := gitMock.addWorktreeWithTrackingCalls
	gitMock.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 AddWorktreeWithTracking call, got %d", len(calls))
	}

	call := calls[0]
	wantBranch := "feature/IN-FETCH"
	if call.LocalBranch != wantBranch {
		t.Errorf("LocalBranch = %q, want %q", call.LocalBranch, wantBranch)
	}
	if call.RemoteBranch != wantBranch {
		t.Errorf("RemoteBranch = %q, want %q", call.RemoteBranch, wantBranch)
	}

	gitMock.mu.Lock()
	addCalls := gitMock.addWorktreeCalls
	gitMock.mu.Unlock()

	if len(addCalls) != 0 {
		t.Errorf("AddWorktree was called %d times, want 0 (should use AddWorktreeWithTracking)", len(addCalls))
	}
}

func TestInit_RemoteBranchConflict_StrategyNewBranch(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	svcRepo := filepath.Join(rootDir, "myservice")
	makeGitDir(t, svcRepo)

	gitMock := &mockGitClient{
		isValidRepoErr:        nil,
		baseBranchResult:      "main",
		branchExistsRes:       false,
		remoteBranchExistsRes: true,
		listWorktreesRes:      nil,
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	err := mgr.Init(context.Background(), InitParams{
		TaskID:       "IN-NEWBRANCH",
		Services:     []string{"myservice"},
		BranchPrefix: "feature/",
		RemoteBranchStrategies: map[string]RemoteBranchStrategy{
			"myservice": StrategyNewBranch,
		},
		BranchSuffixes: map[string]string{
			"myservice": "-v2",
		},
	})
	if err != nil {
		t.Fatalf("Init returned unexpected error: %v", err)
	}

	gitMock.mu.Lock()
	calls := gitMock.addWorktreeCalls
	gitMock.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 AddWorktree call, got %d", len(calls))
	}

	call := calls[0]
	wantBranch := "feature/IN-NEWBRANCH-v2"
	if call.Branch != wantBranch {
		t.Errorf("Branch = %q, want %q", call.Branch, wantBranch)
	}
	if !call.NewBranch {
		t.Error("NewBranch = false, want true (should create new branch)")
	}
	if call.Base != "main" {
		t.Errorf("Base = %q, want %q", call.Base, "main")
	}
}

func TestInit_RemoteBranchConflict_StrategyCancel(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	svcRepo := filepath.Join(rootDir, "myservice")
	makeGitDir(t, svcRepo)

	gitMock := &mockGitClient{
		isValidRepoErr:        nil,
		baseBranchResult:      "main",
		branchExistsRes:       false,
		remoteBranchExistsRes: true,
		listWorktreesRes:      nil,
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	err := mgr.Init(context.Background(), InitParams{
		TaskID:       "IN-CANCEL",
		Services:     []string{"myservice"},
		BranchPrefix: "feature/",
		RemoteBranchStrategies: map[string]RemoteBranchStrategy{
			"myservice": StrategyCancel,
		},
	})

	if err == nil {
		t.Fatal("Init returned nil, want error when all services are cancelled")
	}

	gitMock.mu.Lock()
	addCalls := gitMock.addWorktreeCalls
	trackingCalls := gitMock.addWorktreeWithTrackingCalls
	gitMock.mu.Unlock()

	if len(addCalls) != 0 {
		t.Errorf("AddWorktree was called %d times, want 0 (cancelled)", len(addCalls))
	}
	if len(trackingCalls) != 0 {
		t.Errorf("AddWorktreeWithTracking was called %d times, want 0 (cancelled)", len(trackingCalls))
	}
}

func TestInit_RemoteBranchConflict_NetworkError_FailOpen(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	svcRepo := filepath.Join(rootDir, "myservice")
	makeGitDir(t, svcRepo)

	gitMock := &mockGitClient{
		isValidRepoErr:        nil,
		baseBranchResult:      "main",
		branchExistsRes:       false,
		remoteBranchExistsRes: false,
		remoteBranchExistsErr: errors.New("network error"),
		listWorktreesRes:      nil,
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	err := mgr.Init(context.Background(), InitParams{
		TaskID:       "IN-NETWORK",
		Services:     []string{"myservice"},
		BranchPrefix: "feature/",
	})
	if err != nil {
		t.Fatalf("Init returned unexpected error: %v", err)
	}

	gitMock.mu.Lock()
	calls := gitMock.addWorktreeCalls
	gitMock.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 AddWorktree call, got %d", len(calls))
	}

	call := calls[0]
	if !call.NewBranch {
		t.Error("NewBranch = false, want true (should create new branch)")
	}
}

func TestInit_RemoteBranchConflict_NoRemoteBranch(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	svcRepo := filepath.Join(rootDir, "myservice")
	makeGitDir(t, svcRepo)

	gitMock := &mockGitClient{
		isValidRepoErr:        nil,
		baseBranchResult:      "main",
		branchExistsRes:       false,
		remoteBranchExistsRes: false,
		listWorktreesRes:      nil,
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	err := mgr.Init(context.Background(), InitParams{
		TaskID:       "IN-NORMAL",
		Services:     []string{"myservice"},
		BranchPrefix: "feature/",
	})
	if err != nil {
		t.Fatalf("Init returned unexpected error: %v", err)
	}

	gitMock.mu.Lock()
	calls := gitMock.addWorktreeCalls
	gitMock.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 AddWorktree call, got %d", len(calls))
	}

	call := calls[0]
	if !call.NewBranch {
		t.Error("NewBranch = false, want true (should create new branch)")
	}
	if call.Base != "main" {
		t.Errorf("Base = %q, want %q", call.Base, "main")
	}
}

func TestInit_RemoteBranchConflict_LocalBranchExists(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	svcRepo := filepath.Join(rootDir, "myservice")
	makeGitDir(t, svcRepo)

	gitMock := &mockGitClient{
		isValidRepoErr:        nil,
		baseBranchResult:      "main",
		branchExistsRes:       true,
		remoteBranchExistsRes: true,
		listWorktreesRes:      nil,
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	err := mgr.Init(context.Background(), InitParams{
		TaskID:       "IN-EXISTING",
		Services:     []string{"myservice"},
		BranchPrefix: "feature/",
	})
	if err != nil {
		t.Fatalf("Init returned unexpected error: %v", err)
	}

	gitMock.mu.Lock()
	calls := gitMock.addWorktreeCalls
	gitMock.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 AddWorktree call, got %d", len(calls))
	}

	call := calls[0]
	if call.NewBranch {
		t.Error("NewBranch = true, want false (should use existing branch)")
	}
}

func TestAdd_RemoteBranchConflict_ReturnsError(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	taskDir := filepath.Join(tasksRoot, "ADD-CONFLICT")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	svcRepo := filepath.Join(rootDir, "myservice")
	makeGitDir(t, svcRepo)

	gitMock := &mockGitClient{
		isValidRepoErr:        nil,
		baseBranchResult:      "main",
		branchExistsRes:       false,
		remoteBranchExistsRes: true,
		listWorktreesRes:      nil,
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	err := mgr.Add(context.Background(), AddParams{
		TaskID:   "ADD-CONFLICT",
		Services: []string{"myservice"},
	})

	if err == nil {
		t.Fatal("Add returned nil, want ErrRemoteBranchConflict")
	}

	var conflictErr *ErrRemoteBranchConflict
	if !errors.As(err, &conflictErr) {
		t.Fatalf("Add error = %v, want ErrRemoteBranchConflict", err)
	}

	if conflictErr.TaskID != "ADD-CONFLICT" {
		t.Errorf("TaskID = %q, want %q", conflictErr.TaskID, "ADD-CONFLICT")
	}
	if conflictErr.ServiceName != "myservice" {
		t.Errorf("ServiceName = %q, want %q", conflictErr.ServiceName, "myservice")
	}
}

func TestAdd_RemoteBranchConflict_ReturnsErrorAfterPartialSuccess(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskDir := filepath.Join(tasksRoot, "ADD-PARTIAL-CONFLICT")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	for _, service := range []string{"okservice", "conflictservice"} {
		makeGitDir(t, filepath.Join(rootDir, service))
	}

	gitMock := &mockGitClient{
		isValidRepoErr:   nil,
		baseBranchResult: "main",
		branchExistsRes:  false,
		listWorktreesRes: nil,
		remoteBranchExistsFn: func(repoPath, branch string) (bool, error) {
			return filepath.Base(repoPath) == "conflictservice", nil
		},
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	err := mgr.Add(context.Background(), AddParams{
		TaskID:   "ADD-PARTIAL-CONFLICT",
		Services: []string{"okservice", "conflictservice"},
	})

	if err == nil {
		t.Fatal("Add returned nil, want ErrRemoteBranchConflict after partial success")
	}
	var conflictErr *ErrRemoteBranchConflict
	if !errors.As(err, &conflictErr) {
		t.Fatalf("Add error = %v, want ErrRemoteBranchConflict", err)
	}
	if conflictErr.ServiceName != "conflictservice" {
		t.Errorf("ServiceName = %q, want %q", conflictErr.ServiceName, "conflictservice")
	}
	if _, statErr := os.Stat(taskDir); statErr != nil {
		t.Errorf("task directory not preserved after partial success: %v", statErr)
	}

	gitMock.mu.Lock()
	addCalls := gitMock.addWorktreeCalls
	gitMock.mu.Unlock()
	if len(addCalls) != 1 || addCalls[0].Branch != "feature/ADD-PARTIAL-CONFLICT" {
		t.Fatalf("AddWorktree calls = %+v, want one successful service add", addCalls)
	}
}

func TestAdd_RemoteBranchConflict_StrategyFetchAndSwitch(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	taskDir := filepath.Join(tasksRoot, "ADD-FETCH")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	svcRepo := filepath.Join(rootDir, "myservice")
	makeGitDir(t, svcRepo)

	gitMock := &mockGitClient{
		isValidRepoErr:        nil,
		baseBranchResult:      "main",
		branchExistsRes:       false,
		remoteBranchExistsRes: true,
		listWorktreesRes:      nil,
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	err := mgr.Add(context.Background(), AddParams{
		TaskID:   "ADD-FETCH",
		Services: []string{"myservice"},
		RemoteBranchStrategies: map[string]RemoteBranchStrategy{
			"myservice": StrategyFetchAndSwitch,
		},
	})
	if err != nil {
		t.Fatalf("Add returned unexpected error: %v", err)
	}

	gitMock.mu.Lock()
	calls := gitMock.addWorktreeWithTrackingCalls
	gitMock.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 AddWorktreeWithTracking call, got %d", len(calls))
	}

	call := calls[0]
	wantBranch := "feature/ADD-FETCH"
	if call.LocalBranch != wantBranch {
		t.Errorf("LocalBranch = %q, want %q", call.LocalBranch, wantBranch)
	}
	if call.RemoteBranch != wantBranch {
		t.Errorf("RemoteBranch = %q, want %q", call.RemoteBranch, wantBranch)
	}

	gitMock.mu.Lock()
	addCalls := gitMock.addWorktreeCalls
	gitMock.mu.Unlock()

	if len(addCalls) != 0 {
		t.Errorf("AddWorktree was called %d times, want 0 (should use AddWorktreeWithTracking)", len(addCalls))
	}
}

func TestAdd_RemoteBranchConflict_StrategyNewBranch(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	taskDir := filepath.Join(tasksRoot, "ADD-NEWBRANCH")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	svcRepo := filepath.Join(rootDir, "myservice")
	makeGitDir(t, svcRepo)

	gitMock := &mockGitClient{
		isValidRepoErr:        nil,
		baseBranchResult:      "main",
		branchExistsRes:       false,
		remoteBranchExistsRes: true,
		listWorktreesRes:      nil,
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	err := mgr.Add(context.Background(), AddParams{
		TaskID:   "ADD-NEWBRANCH",
		Services: []string{"myservice"},
		RemoteBranchStrategies: map[string]RemoteBranchStrategy{
			"myservice": StrategyNewBranch,
		},
		BranchSuffixes: map[string]string{
			"myservice": "-v2",
		},
	})
	if err != nil {
		t.Fatalf("Add returned unexpected error: %v", err)
	}

	gitMock.mu.Lock()
	calls := gitMock.addWorktreeCalls
	gitMock.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 AddWorktree call, got %d", len(calls))
	}

	call := calls[0]
	wantBranch := "feature/ADD-NEWBRANCH-v2"
	if call.Branch != wantBranch {
		t.Errorf("Branch = %q, want %q", call.Branch, wantBranch)
	}
	if !call.NewBranch {
		t.Error("NewBranch = false, want true (should create new branch)")
	}
	if call.Base != "main" {
		t.Errorf("Base = %q, want %q", call.Base, "main")
	}
}

func TestAdd_RemoteBranchConflict_StrategyCancel(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	taskDir := filepath.Join(tasksRoot, "ADD-CANCEL")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	svcRepo := filepath.Join(rootDir, "myservice")
	makeGitDir(t, svcRepo)

	gitMock := &mockGitClient{
		isValidRepoErr:        nil,
		baseBranchResult:      "main",
		branchExistsRes:       false,
		remoteBranchExistsRes: true,
		listWorktreesRes:      nil,
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	err := mgr.Add(context.Background(), AddParams{
		TaskID:   "ADD-CANCEL",
		Services: []string{"myservice"},
		RemoteBranchStrategies: map[string]RemoteBranchStrategy{
			"myservice": StrategyCancel,
		},
	})

	if err == nil {
		t.Fatal("Add returned nil, want error when all services are cancelled")
	}

	gitMock.mu.Lock()
	addCalls := gitMock.addWorktreeCalls
	trackingCalls := gitMock.addWorktreeWithTrackingCalls
	gitMock.mu.Unlock()

	if len(addCalls) != 0 {
		t.Errorf("AddWorktree was called %d times, want 0 (cancelled)", len(addCalls))
	}
	if len(trackingCalls) != 0 {
		t.Errorf("AddWorktreeWithTracking was called %d times, want 0 (cancelled)", len(trackingCalls))
	}
}

func TestAdd_RemoteBranchConflict_NoRemoteBranch(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	taskDir := filepath.Join(tasksRoot, "ADD-NORMAL")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	svcRepo := filepath.Join(rootDir, "myservice")
	makeGitDir(t, svcRepo)

	gitMock := &mockGitClient{
		isValidRepoErr:        nil,
		baseBranchResult:      "main",
		branchExistsRes:       false,
		remoteBranchExistsRes: false,
		listWorktreesRes:      nil,
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	err := mgr.Add(context.Background(), AddParams{
		TaskID:   "ADD-NORMAL",
		Services: []string{"myservice"},
	})
	if err != nil {
		t.Fatalf("Add returned unexpected error: %v", err)
	}

	gitMock.mu.Lock()
	calls := gitMock.addWorktreeCalls
	gitMock.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 AddWorktree call, got %d", len(calls))
	}

	call := calls[0]
	if !call.NewBranch {
		t.Error("NewBranch = false, want true (should create new branch)")
	}
	if call.Base != "main" {
		t.Errorf("Base = %q, want %q", call.Base, "main")
	}
}

func TestAdd_RemoteBranchConflict_LocalBranchExists(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	taskDir := filepath.Join(tasksRoot, "ADD-EXISTING")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	svcRepo := filepath.Join(rootDir, "myservice")
	makeGitDir(t, svcRepo)

	gitMock := &mockGitClient{
		isValidRepoErr:        nil,
		baseBranchResult:      "main",
		branchExistsRes:       true,
		remoteBranchExistsRes: true,
		listWorktreesRes:      nil,
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	err := mgr.Add(context.Background(), AddParams{
		TaskID:   "ADD-EXISTING",
		Services: []string{"myservice"},
	})
	if err != nil {
		t.Fatalf("Add returned unexpected error: %v", err)
	}

	gitMock.mu.Lock()
	calls := gitMock.addWorktreeCalls
	gitMock.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 AddWorktree call, got %d", len(calls))
	}

	call := calls[0]
	if call.NewBranch {
		t.Error("NewBranch = true, want false (should use existing branch)")
	}
}

func TestAdd_RemoteBranchConflict_NetworkError_FailOpen(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	taskDir := filepath.Join(tasksRoot, "ADD-NETWORK")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	svcRepo := filepath.Join(rootDir, "myservice")
	makeGitDir(t, svcRepo)

	gitMock := &mockGitClient{
		isValidRepoErr:        nil,
		baseBranchResult:      "main",
		branchExistsRes:       false,
		remoteBranchExistsRes: false,
		remoteBranchExistsErr: errors.New("network error"),
		listWorktreesRes:      nil,
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	err := mgr.Add(context.Background(), AddParams{
		TaskID:   "ADD-NETWORK",
		Services: []string{"myservice"},
	})
	if err != nil {
		t.Fatalf("Add returned unexpected error: %v", err)
	}

	gitMock.mu.Lock()
	calls := gitMock.addWorktreeCalls
	gitMock.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 AddWorktree call, got %d", len(calls))
	}

	call := calls[0]
	if !call.NewBranch {
		t.Error("NewBranch = false, want true (should create new branch)")
	}
}

var _ dotnet.Client = (*mockDotnetClient)(nil)

func assertContainsLine(t *testing.T, lines []string, want string) {
	t.Helper()

	if slices.Contains(lines, want) {
		return
	}

	t.Fatalf("lines %v do not contain %q", lines, want)
}
