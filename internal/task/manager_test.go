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

	"log/slog"

	"github.com/diss0x/wtui/internal/config"
	"github.com/diss0x/wtui/internal/discovery"
	"github.com/diss0x/wtui/internal/dotnet"
	"github.com/diss0x/wtui/internal/git"
	"github.com/diss0x/wtui/internal/sln"
)

// ─── Mock git.Client ─────────────────────────────────────────────────────────

// mockGitClient implements git.Client with configurable return values and
// call recording for assertion in tests.
type mockGitClient struct {
	mu sync.Mutex

	// Per-method return values.
	isValidRepoErr   error
	baseBranchResult string
	baseBranchErr    error
	branchExistsRes  bool
	branchExistsErr  error
	listWorktreesRes []git.WorktreeEntry
	listWorktreesErr error
	addWorktreeErr   error
	commonDirResult  string
	commonDirErr     error
	// commonDirFn overrides commonDirResult/commonDirErr when set.
	// Receives the worktreePath argument; allows per-path test behaviour.
	commonDirFn       func(path string) (string, error)
	removeWorktreeErr error
	isDirtyRes        bool
	isDirtyErr        error
	fetchErr          error
	rebaseErr         error
	pushErr           error
	fetchFn           func(path string) error
	rebaseFn          func(path, upstream string) error
	pushFn            func(path string, lineCh chan<- string) error
	stashErr          error
	versionMajor      int
	versionMinor      int
	versionErr        error

	// Call records.
	addWorktreeCalls    []addWorktreeCall
	removeWorktreeCalls []removeWorktreeCall
	fetchCalls          []string
	rebaseCalls         []rebaseCall
	pushCalls           []string
	stashCalls          []stashCall
}

type addWorktreeCall struct {
	RepoPath  string
	Dest      string
	Branch    string
	NewBranch bool
	Base      string
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

func (m *mockGitClient) IsDirty(_ context.Context, _ string) (bool, error) {
	return m.isDirtyRes, m.isDirtyErr
}

func (m *mockGitClient) Version(_ context.Context) (int, int, error) {
	return m.versionMajor, m.versionMinor, m.versionErr
}

func (m *mockGitClient) RevListCount(_ context.Context, _, _, _ string) (int, error) {
	return 0, nil
}

func (m *mockGitClient) RevListAheadBehind(_ context.Context, _, _ string) (int, int, error) {
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

// ─── Mock dotnet.Client ───────────────────────────────────────────────────────

// mockDotnetClient implements dotnet.Client. IsAvailable always returns false so
// that sln.Manager.Generate is a guaranteed no-op in tests.
type mockDotnetClient struct{}

func (m *mockDotnetClient) IsAvailable(_ context.Context) bool { return false }
func (m *mockDotnetClient) NewSln(_ context.Context, _, _ string) error {
	return errors.New("dotnet not available in tests")
}
func (m *mockDotnetClient) SlnAdd(_ context.Context, _, _, _ string) error {
	return errors.New("dotnet not available in tests")
}

// ─── Test helpers ─────────────────────────────────────────────────────────────

// newTestLogger returns a slog.Logger that discards all output. Test failures
// should not be obscured by log noise.
func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError, // only surface errors in tests
	}))
}

// newTestManager constructs a Manager wired with the given git client and a real
// *discovery.Discoverer that points at rootDir.
func newTestManager(t *testing.T, tasksRoot, rootDir string, gitMock *mockGitClient) Manager {
	t.Helper()

	cfg := &config.Config{
		TasksRoot:    tasksRoot,
		RootDir:      rootDir,
		BranchPrefix: "feature/",
		Editor:       "code",
	}
	cfg.Effective()
	// Override with provided values after Effective() sets defaults.
	cfg.TasksRoot = tasksRoot
	cfg.RootDir = rootDir

	logger := newTestLogger()
	disc := discovery.New(cfg, gitMock, logger)
	slnMgr := sln.NewManager(&mockDotnetClient{}, logger)

	return New(cfg, gitMock, disc, slnMgr, logger)
}

// newTestManagerWithCfg constructs a Manager using the provided config directly.
// It is used by tests that need fine-grained control over config fields such as
// BaseBranch. The caller is responsible for setting all required fields.
func newTestManagerWithCfg(t *testing.T, cfg *config.Config, gitMock *mockGitClient) Manager {
	t.Helper()

	logger := newTestLogger()
	disc := discovery.New(cfg, gitMock, logger)
	slnMgr := sln.NewManager(&mockDotnetClient{}, logger)

	return New(cfg, gitMock, disc, slnMgr, logger)
}

// makeGitDir creates a fake .git directory under repoDir so the discoverer can
// find it via the direct-child or depth-walk check.
func makeGitDir(t *testing.T, repoDir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755); err != nil {
		t.Fatalf("makeGitDir: %v", err)
	}
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestInit_CreatesDirAndCallsAddWorktree verifies that Init creates the task
// directory and calls git.AddWorktree with correct arguments.
func TestInit_CreatesDirAndCallsAddWorktree(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	// Create a fake service repo.
	svcRepo := filepath.Join(rootDir, "myservice")
	makeGitDir(t, svcRepo)

	gitMock := &mockGitClient{
		isValidRepoErr:   nil,
		baseBranchResult: "main",
		branchExistsRes:  false, // branch doesn't exist → newBranch=true
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

	// Task directory must exist.
	taskDir := filepath.Join(tasksRoot, "IN-001")
	if _, err := os.Stat(taskDir); err != nil {
		t.Fatalf("task directory not created: %v", err)
	}

	// AddWorktree must have been called once.
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

// TestInit_ErrTaskExists verifies that Init returns ErrTaskExists when the task
// directory already exists.
func TestInit_ErrTaskExists(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskDir := filepath.Join(tasksRoot, "IN-002")

	// Pre-create the task directory.
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

// TestInit_ContinuesWhenServiceNotFound verifies that Init rolls back the task
// directory and returns an error when the only requested service cannot be
// resolved. (Previously this was a no-op; now a fully-failed init is an error.)
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
	// All services failed → Init must return an error.
	if err == nil {
		t.Fatal("Init returned nil, want error when all services fail to resolve")
	}

	// Task directory must have been rolled back.
	taskDir := filepath.Join(tasksRoot, "IN-003")
	if _, statErr := os.Stat(taskDir); !os.IsNotExist(statErr) {
		t.Errorf("task directory still exists after rollback, want it removed")
	}

	// AddWorktree must NOT have been called (resolve failed before reaching git).
	gitMock.mu.Lock()
	n := len(gitMock.addWorktreeCalls)
	gitMock.mu.Unlock()
	if n != 0 {
		t.Errorf("AddWorktree was called %d times, want 0", n)
	}
}

// TestInit_UsesExistingBranch verifies that when BranchExists returns true, Init
// calls AddWorktree with newBranch=false (check out existing branch, no -b flag).
func TestInit_UsesExistingBranch(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	svcRepo := filepath.Join(rootDir, "svcA")
	makeGitDir(t, svcRepo)

	gitMock := &mockGitClient{
		isValidRepoErr:   nil,
		baseBranchResult: "main",
		branchExistsRes:  true, // branch already exists
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

// TestAdd_ErrTaskNotFound verifies that Add returns ErrTaskNotFound when the
// task directory does not exist.
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

// TestList_EmptyWhenTasksRootMissing verifies that List returns an empty slice
// (not an error) when the TasksRoot directory does not exist.
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

// TestList_ReturnsSortedTasks verifies that List returns tasks alphabetically
// sorted by ID when multiple task directories exist.
func TestList_ReturnsSortedTasks(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	// Create task directories in reverse alphabetical order to confirm sorting.
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

// TestListServices_ErrTaskNotFound verifies that ListServices returns ErrTaskNotFound
// when the task directory does not exist.
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

// TestListServices_SkipsNonGitDirs verifies that ListServices excludes plain
// directories that are not git worktrees (i.e. CommonDir returns an error for
// them) while still including valid worktree directories.
func TestListServices_SkipsNonGitDirs(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	// Task directory with two subdirs: one valid worktree, one plain dir.
	taskDir := filepath.Join(tasksRoot, "IN-LSS")
	for _, name := range []string{"service-a", "not-a-worktree"} {
		if err := os.MkdirAll(filepath.Join(taskDir, name), 0o755); err != nil {
			t.Fatalf("setup: create subdir %s: %v", name, err)
		}
	}

	// Fake commonDir path returned for the valid worktree.
	fakeCommonDir := filepath.Join(rootDir, "service-a", ".git")
	if err := os.MkdirAll(fakeCommonDir, 0o755); err != nil {
		t.Fatalf("setup: create fakeCommonDir: %v", err)
	}

	gitMock := &mockGitClient{
		// commonDirFn returns success for service-a and an error for not-a-worktree.
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

// TestRemove_CallsGitAndRemovesTaskDir verifies that Remove calls git worktree
// remove for each service and then os.RemoveAll deletes the task directory.
func TestRemove_CallsGitAndRemovesTaskDir(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	// Create a task directory with one service subdirectory.
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

	// Task directory must be gone.
	if _, err := os.Stat(taskDir); !os.IsNotExist(err) {
		t.Errorf("task directory still exists after Remove")
	}

	// RemoveWorktree must have been called once.
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

// TestRemove_WithoutForce_FailedWorktreePreservesTaskDir verifies that when
// RemoveWorktree fails and force=false, the task directory is NOT deleted and an
// error is returned.
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

	// Task directory must still exist because removal failed.
	if _, statErr := os.Stat(taskDir); statErr != nil {
		t.Errorf("task directory was deleted despite worktree removal error")
	}
}

// TestRemove_WithForce_RemovesTaskDirDespiteErrors verifies that with force=true
// os.RemoveAll is called even when git worktree remove fails.
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

	// With force=true, Remove must succeed and clean up the task dir.
	if err := mgr.Remove(context.Background(), "IN-012", true, false); err != nil {
		t.Fatalf("Remove(force=true) returned unexpected error: %v", err)
	}

	if _, err := os.Stat(taskDir); !os.IsNotExist(err) {
		t.Errorf("task directory still exists after forced Remove")
	}
}

// TestValidateTaskID tests the task ID validator for forbidden characters.
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

// TestGenerateWorkspaceFile verifies that generateWorkspaceFile produces a valid
// JSON .code-workspace file with relative folder paths for each service subdir.
func TestGenerateWorkspaceFile(t *testing.T) {
	taskDir := t.TempDir()
	taskID := "IN-WS-001"

	// Create service subdirectories.
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

	// Workspace file must reference both services.
	if !strings.Contains(content, `"svcA"`) {
		t.Errorf("workspace file missing svcA path: %s", content)
	}
	if !strings.Contains(content, `"svcB"`) {
		t.Errorf("workspace file missing svcB path: %s", content)
	}

	// Settings must be present.
	if !strings.Contains(content, `"workbench.editor.labelFormat": "medium"`) {
		t.Errorf("workspace file missing settings: %s", content)
	}
}

// TestBuildServicesFromSubdirs verifies that buildServicesFromSubdirs correctly
// maps directory names to domain.Service entries.
func TestBuildServicesFromSubdirs(t *testing.T) {
	taskDir := t.TempDir()

	for _, name := range []string{"svc1", "svc2"} {
		if err := os.MkdirAll(filepath.Join(taskDir, name), 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	// Also create a non-directory file to ensure it is skipped.
	if err := os.WriteFile(filepath.Join(taskDir, "some.sln"), []byte(""), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	services := buildServicesFromSubdirs(taskDir)
	if len(services) != 2 {
		t.Fatalf("got %d services, want 2", len(services))
	}
}

// TestList_StaleFlag verifies that Task.Stale is false for a normal task dir
// (the stale=true path for a race-removed dir is not directly testable in a
// unit test without racy filesystem manipulation, but we assert the happy path).
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

// ─── CloneTask tests ──────────────────────────────────────────────────────────

// TestCloneTask_CallsAddWorktreePerService verifies that CloneTask resolves src
// services and calls git.AddWorktree once per service, using the dst branch name
// and the service's BaseBranch (defaulting to "HEAD" when empty).
func TestCloneTask_CallsAddWorktreePerService(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	// Create the src task directory with two service subdirs so that ListServices
	// can discover them as git worktrees.
	srcTaskDir := filepath.Join(tasksRoot, "IN-SRC")
	for _, svcName := range []string{"svcA", "svcB"} {
		if err := os.MkdirAll(filepath.Join(srcTaskDir, svcName), 0o755); err != nil {
			t.Fatalf("setup: create src service dir %s: %v", svcName, err)
		}
	}

	// Fake repo paths returned by CommonDir for each service.
	fakeCommonDirA := filepath.Join(rootDir, "svcA", ".git")
	fakeCommonDirB := filepath.Join(rootDir, "svcB", ".git")
	for _, p := range []string{fakeCommonDirA, fakeCommonDirB} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("setup: create fakeCommonDir %s: %v", p, err)
		}
	}

	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			switch filepath.Base(path) {
			case "svcA":
				return fakeCommonDirA, nil
			case "svcB":
				return fakeCommonDirB, nil
			default:
				return "", errors.New("not a git worktree")
			}
		},
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	if err := mgr.CloneTask(context.Background(), "IN-SRC", "IN-DST"); err != nil {
		t.Fatalf("CloneTask returned unexpected error: %v", err)
	}

	// Dst task directory must exist.
	dstDir := filepath.Join(tasksRoot, "IN-DST")
	if _, err := os.Stat(dstDir); err != nil {
		t.Fatalf("dst task directory not created: %v", err)
	}

	// AddWorktree must have been called once per service.
	gitMock.mu.Lock()
	calls := gitMock.addWorktreeCalls
	gitMock.mu.Unlock()

	if len(calls) != 2 {
		t.Fatalf("expected 2 AddWorktree calls, got %d", len(calls))
	}

	// All calls must use newBranch=true with the dst branch name.
	wantBranch := "feature/IN-DST"
	for _, call := range calls {
		if call.Branch != wantBranch {
			t.Errorf("AddWorktree branch = %q, want %q", call.Branch, wantBranch)
		}
		if !call.NewBranch {
			t.Error("AddWorktree newBranch = false, want true (clone always creates new branches)")
		}
		// BaseBranch not populated by ListServices, so base should default to "HEAD".
		if call.Base != "HEAD" {
			t.Errorf("AddWorktree base = %q, want %q (BaseBranch empty → fallback)", call.Base, "HEAD")
		}
	}
}

// TestCloneTask_ErrTaskExists verifies that CloneTask returns ErrTaskExists when
// the dst directory already exists.
func TestCloneTask_ErrTaskExists(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	// Create both src and dst directories.
	for _, id := range []string{"IN-SRC2", "IN-DST2"} {
		if err := os.MkdirAll(filepath.Join(tasksRoot, id), 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	gitMock := &mockGitClient{}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	err := mgr.CloneTask(context.Background(), "IN-SRC2", "IN-DST2")
	if !errors.Is(err, ErrTaskExists) {
		t.Errorf("CloneTask error = %v, want ErrTaskExists", err)
	}
}

// TestCloneTask_ErrSrcNotFound verifies that CloneTask returns ErrTaskNotFound
// when the src task directory does not exist.
func TestCloneTask_ErrSrcNotFound(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	gitMock := &mockGitClient{}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	err := mgr.CloneTask(context.Background(), "IN-NOSRC", "IN-NEWDST")
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("CloneTask error = %v, want ErrTaskNotFound", err)
	}
}

// TestSyncTask_FetchFailureReturnsFirstError verifies that SyncTask runs all
// service goroutines, emits progress lines, and returns the first fetch error.
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

	err := mgr.SyncTask(context.Background(), "IN-SYNC", lineCh)
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

// TestSyncTask_RebasesOntoConfigBaseBranch verifies that SyncTask uses
// cfg.BaseBranch (here "develop") as the upstream for git.Rebase, producing
// "origin/develop" for every service in the task.
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
	cfg.BaseBranch = "develop" // ensure Effective() did not overwrite with default

	mgr := newTestManagerWithCfg(t, cfg, gitMock)
	lineCh := make(chan string, 32)

	if err := mgr.SyncTask(context.Background(), "IN-BB", lineCh); err != nil {
		t.Fatalf("SyncTask returned unexpected error: %v", err)
	}

	// Drain the channel (it is closed by SyncTask).
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

// TestSyncTask_RebasesOntoMainWhenBaseBranchEmpty verifies that SyncTask falls
// back to "origin/develop" when cfg.BaseBranch is the empty string (the in-code
// guard: `if baseBranch == "" { baseBranch = "develop" }`).
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

	// Deliberately set BaseBranch to "" to exercise the in-code fallback.
	cfg := &config.Config{
		TasksRoot:    tasksRoot,
		RootDir:      rootDir,
		BranchPrefix: "feature/",
		BaseBranch:   "", // empty — SyncTask must fall back to "develop"
		Editor:       "code",
	}
	// Call Effective() but then reset BaseBranch to "" to simulate a config
	// that somehow bypasses the default (e.g. set programmatically after load).
	cfg.Effective()
	cfg.TasksRoot = tasksRoot
	cfg.RootDir = rootDir
	cfg.BaseBranch = "" // force empty after Effective() to test the in-code guard (fallback = "develop")

	mgr := newTestManagerWithCfg(t, cfg, gitMock)
	lineCh := make(chan string, 16)

	if err := mgr.SyncTask(context.Background(), "IN-EMPTY-BB", lineCh); err != nil {
		t.Fatalf("SyncTask returned unexpected error: %v", err)
	}

	// Drain the channel.
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

// TestPushService_StreamsLines verifies that PushService emits progress lines,
// forwards streamed git output, and pushes the expected worktree path.
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

// TestPushService_ErrServiceNotFound verifies that missing service worktrees
// return an ErrServiceNotFound-wrapped error.
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

// TestStashService_Stash verifies that pop=false calls git.Stash with pop=false
// for the target service worktree.
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

// TestStashService_Pop verifies that pop=true calls git.Stash with pop=true
// for the target service worktree.
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

// TestStashService_ErrServiceNotFound verifies that missing service worktrees
// return an ErrServiceNotFound-wrapped error.
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

// Compile-time assertion: mockGitClient must implement git.Client.
var _ git.Client = (*mockGitClient)(nil)

// TestInit_RollsBackWhenNoWorktreesAdded verifies that Init removes the task
// directory and returns an error when services were requested but none could be
// added (e.g. all service tokens are invalid).
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

	// Task directory must have been rolled back.
	taskDir := filepath.Join(tasksRoot, "IN-ROLLBACK")
	if _, statErr := os.Stat(taskDir); !os.IsNotExist(statErr) {
		t.Errorf("task directory still exists after rollback, want it removed")
	}
}

// TestInit_NoServicesRequested verifies that Init with an empty Services slice
// succeeds without rollback (zero services requested = zero expected).
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

// TestAdd_ReturnsErrorWhenNoWorktreesAdded verifies that Add returns an error
// when all requested services fail but does NOT remove the pre-existing task dir.
func TestAdd_ReturnsErrorWhenNoWorktreesAdded(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	// Pre-create the task directory.
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

	// Task directory must still exist (no rollback on Add).
	if _, statErr := os.Stat(taskDir); statErr != nil {
		t.Errorf("task directory was removed by Add, want it preserved: %v", statErr)
	}
}

// Compile-time assertion: mockDotnetClient must implement dotnet.Client.
var _ dotnet.Client = (*mockDotnetClient)(nil)

// ─── helpers ─────────────────────────────────────────────────────────────────

func assertContainsLine(t *testing.T, lines []string, want string) {
	t.Helper()

	if slices.Contains(lines, want) {
		return
	}

	t.Fatalf("lines %v do not contain %q", lines, want)
}
