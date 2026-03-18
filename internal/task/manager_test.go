package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
	versionMajor      int
	versionMinor      int
	versionErr        error

	// Call records.
	addWorktreeCalls    []addWorktreeCall
	removeWorktreeCalls []removeWorktreeCall
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

// TestInit_ContinuesWhenServiceNotFound verifies that Init does not return an
// error when a service token cannot be resolved (it logs a warning and continues).
func TestInit_ContinuesWhenServiceNotFound(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	gitMock := &mockGitClient{
		isValidRepoErr: errors.New("not a valid repo"),
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	// "nonexistent" service has no directory under rootDir — Resolve will return
	// ErrServiceNotFound. Init must not surface this as an error.
	err := mgr.Init(context.Background(), InitParams{
		TaskID:   "IN-003",
		Services: []string{"nonexistent"},
	})
	if err != nil {
		t.Errorf("Init returned unexpected error for missing service: %v", err)
	}

	// Task directory should still have been created.
	taskDir := filepath.Join(tasksRoot, "IN-003")
	if _, statErr := os.Stat(taskDir); statErr != nil {
		t.Errorf("task directory not created after skipping missing service: %v", statErr)
	}

	// AddWorktree should NOT have been called.
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

	if err := mgr.Remove(context.Background(), "IN-010", false); err != nil {
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

	err := mgr.Remove(context.Background(), "IN-011", false)
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
	if err := mgr.Remove(context.Background(), "IN-012", true); err != nil {
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
	if !containsString(content, `"svcA"`) {
		t.Errorf("workspace file missing svcA path: %s", content)
	}
	if !containsString(content, `"svcB"`) {
		t.Errorf("workspace file missing svcB path: %s", content)
	}

	// Settings must be present.
	if !containsString(content, `"workbench.editor.labelFormat": "medium"`) {
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

// ─── interface compliance ─────────────────────────────────────────────────────

// Compile-time assertion: mockGitClient must implement git.Client.
var _ git.Client = (*mockGitClient)(nil)

// Compile-time assertion: mockDotnetClient must implement dotnet.Client.
var _ dotnet.Client = (*mockDotnetClient)(nil)

// ─── helpers ─────────────────────────────────────────────────────────────────

func containsString(haystack, needle string) bool {
	return len(haystack) >= len(needle) && func() bool {
		for i := 0; i <= len(haystack)-len(needle); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	}()
}
