package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/diss0x/wtui/internal/config"
	"github.com/diss0x/wtui/internal/domain"
	"github.com/diss0x/wtui/internal/git"
	"github.com/diss0x/wtui/internal/task"
)

// ── Test helpers ─────────────────────────────────────────────────────────────

// executeCommand runs a cobra command with the provided args, capturing stdout
// and stderr into separate buffers. The root command must be a freshly-built
// instance so tests do not share mutable state.
func executeCommand(root *cobra.Command, args ...string) (stdout, stderr string, err error) {
	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}
	root.SetOut(stdoutBuf)
	root.SetErr(stderrBuf)
	root.SetArgs(args)
	err = root.Execute()
	return stdoutBuf.String(), stderrBuf.String(), err
}

// mockManager is a test-double for task.Manager. All fields are optional
// function values; unset ones return zero values and no error.
type mockManager struct {
	initFn          func(ctx context.Context, params task.InitParams) error
	addFn           func(ctx context.Context, params task.AddParams) error
	listFn          func(ctx context.Context) ([]domain.Task, error)
	listServicesFn  func(ctx context.Context, taskID string) ([]domain.Service, error)
	removeFn        func(ctx context.Context, taskID string, force, deleteBranches bool) error
	generateSlnFn   func(ctx context.Context, taskID string) error
	discoverReposFn func(ctx context.Context) ([]domain.Repo, error)
	syncTaskFn      func(ctx context.Context, taskID string, lineCh chan<- string) error
	pushTaskFn      func(ctx context.Context, taskID string, lineCh chan<- string) error
	pushServiceFn   func(ctx context.Context, taskID, serviceName string, lineCh chan<- string) error
	stashServiceFn  func(ctx context.Context, taskID, serviceName string, pop bool) error
}

func (m *mockManager) Init(ctx context.Context, params task.InitParams) error {
	if m.initFn != nil {
		return m.initFn(ctx, params)
	}
	return nil
}

func (m *mockManager) Add(ctx context.Context, params task.AddParams) error {
	if m.addFn != nil {
		return m.addFn(ctx, params)
	}
	return nil
}

func (m *mockManager) List(ctx context.Context) ([]domain.Task, error) {
	if m.listFn != nil {
		return m.listFn(ctx)
	}
	return []domain.Task{}, nil
}

func (m *mockManager) ListServices(ctx context.Context, taskID string) ([]domain.Service, error) {
	if m.listServicesFn != nil {
		return m.listServicesFn(ctx, taskID)
	}
	return []domain.Service{}, nil
}

func (m *mockManager) Remove(ctx context.Context, taskID string, force, deleteBranches bool) error {
	if m.removeFn != nil {
		return m.removeFn(ctx, taskID, force, deleteBranches)
	}
	return nil
}

func (m *mockManager) GenerateSln(ctx context.Context, taskID string) error {
	if m.generateSlnFn != nil {
		return m.generateSlnFn(ctx, taskID)
	}
	return nil
}

func (m *mockManager) DiscoverRepos(ctx context.Context) ([]domain.Repo, error) {
	if m.discoverReposFn != nil {
		return m.discoverReposFn(ctx)
	}
	return []domain.Repo{}, nil
}

func (m *mockManager) SyncTask(ctx context.Context, taskID string, lineCh chan<- string) error {
	if m.syncTaskFn != nil {
		return m.syncTaskFn(ctx, taskID, lineCh)
	}
	close(lineCh)
	return nil
}

func (m *mockManager) PushTask(ctx context.Context, taskID string, lineCh chan<- string) error {
	if m.pushTaskFn != nil {
		return m.pushTaskFn(ctx, taskID, lineCh)
	}
	close(lineCh)
	return nil
}

func (m *mockManager) PushService(ctx context.Context, taskID, serviceName string, lineCh chan<- string) error {
	if m.pushServiceFn != nil {
		return m.pushServiceFn(ctx, taskID, serviceName, lineCh)
	}
	return nil
}

func (m *mockManager) CloneTask(ctx context.Context, src, dst string) error {
	return nil
}

func (m *mockManager) StashService(ctx context.Context, taskID, serviceName string, pop bool) error {
	if m.stashServiceFn != nil {
		return m.stashServiceFn(ctx, taskID, serviceName, pop)
	}
	return nil
}

func (m *mockManager) RemoveService(_ context.Context, _, _ string, _ bool) error { return nil }

// Compile-time assertion: mockManager must implement task.Manager.
var _ task.Manager = (*mockManager)(nil)

// buildTestRoot constructs a root command that bypasses PersistentPreRunE (which
// requires real config/logger/git) and injects the provided mock manager.
// The version subcommand is also included for version tests.
func buildTestRoot(mock task.Manager, version string) *cobra.Command {
	// Inject the mock into the package-level var.
	mgr = mock

	root := &cobra.Command{
		Use:          "wtui",
		SilenceUsage: true, // suppress usage on error to keep test output clean
	}

	// Register the same persistent flags as buildRootCmd so flag-plumbing tests work.
	root.PersistentFlags().StringVar(&cfgFile, "config", "", "path to config file")
	root.PersistentFlags().StringVar(&rootDir, "root", "", "override config root_dir")
	root.PersistentFlags().StringVar(&tasksRoot, "tasks-root", "", "override config tasks_root")
	root.PersistentFlags().BoolVar(&initConfig, "init-config", false, "write default config.yaml and exit")

	// Override PersistentPreRunE so tests don't need real config / git.
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		// version subcommand sets its own PersistentPreRunE to no-op; respect that.
		return nil
	}

	root.AddCommand(
		newInitCmd(),
		newAddCmd(),
		newListCmd(),
		newRemoveCmd(),
		newSlnCmd(),
		newVersionCmd(version),
		newConfigCmd(),
	)

	return root
}

// ── list tests ───────────────────────────────────────────────────────────────

func TestList_EmptyManager_PrintsNoTasks(t *testing.T) {
	mock := &mockManager{
		listFn: func(_ context.Context) ([]domain.Task, error) {
			return []domain.Task{}, nil
		},
	}
	root := buildTestRoot(mock, "dev")
	stdout, _, err := executeCommand(root, "list")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !strings.Contains(stdout, "No tasks.") {
		t.Errorf("expected 'No tasks.' in output, got: %q", stdout)
	}
}

func TestList_WithTasks_PrintsSortedTaskIDs(t *testing.T) {
	mock := &mockManager{
		listFn: func(_ context.Context) ([]domain.Task, error) {
			return []domain.Task{
				{ID: "IN-9999"},
				{ID: "IN-0001"},
				{ID: "IN-5000"},
			}, nil
		},
	}
	root := buildTestRoot(mock, "dev")
	stdout, _, err := executeCommand(root, "list")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), stdout)
	}
	if lines[0] != "IN-0001" || lines[1] != "IN-5000" || lines[2] != "IN-9999" {
		t.Errorf("expected sorted order IN-0001, IN-5000, IN-9999; got: %v", lines)
	}
}

func TestList_WithTaskID_EmptyServices_PrintsNoServices(t *testing.T) {
	mock := &mockManager{
		listServicesFn: func(_ context.Context, taskID string) ([]domain.Service, error) {
			return []domain.Service{}, nil
		},
	}
	root := buildTestRoot(mock, "dev")
	stdout, _, err := executeCommand(root, "list", "IN-6748")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !strings.Contains(stdout, "No services.") {
		t.Errorf("expected 'No services.' in output, got: %q", stdout)
	}
}

func TestList_WithTaskID_PrintsSortedServiceNames(t *testing.T) {
	mock := &mockManager{
		listServicesFn: func(_ context.Context, taskID string) ([]domain.Service, error) {
			return []domain.Service{
				{Name: "zebra"},
				{Name: "alpha"},
				{Name: "mango"},
			}, nil
		},
	}
	root := buildTestRoot(mock, "dev")
	stdout, _, err := executeCommand(root, "list", "IN-6748")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), stdout)
	}
	if lines[0] != "alpha" || lines[1] != "mango" || lines[2] != "zebra" {
		t.Errorf("expected sorted order alpha, mango, zebra; got: %v", lines)
	}
}

// ── init tests ───────────────────────────────────────────────────────────────

func TestInit_ErrTaskExists_ExitsCode1(t *testing.T) {
	mock := &mockManager{
		initFn: func(_ context.Context, _ task.InitParams) error {
			return errors.Join(task.ErrTaskExists, errors.New("IN-6748"))
		},
	}
	root := buildTestRoot(mock, "dev")
	_, _, err := executeCommand(root, "init", "IN-6748", "collection")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	code := exitCode(err)
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestInit_Success_PrintsConfirmation(t *testing.T) {
	var capturedParams task.InitParams
	mock := &mockManager{
		initFn: func(_ context.Context, params task.InitParams) error {
			capturedParams = params
			return nil
		},
	}
	root := buildTestRoot(mock, "dev")
	stdout, _, err := executeCommand(root, "init", "IN-6748", "collection", "databridge")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !strings.Contains(stdout, "IN-6748") {
		t.Errorf("expected task ID in output, got: %q", stdout)
	}
	if capturedParams.TaskID != "IN-6748" {
		t.Errorf("expected TaskID=IN-6748, got: %q", capturedParams.TaskID)
	}
	if len(capturedParams.Services) != 2 {
		t.Errorf("expected 2 services, got %d", len(capturedParams.Services))
	}
}

func TestInit_BranchPrefixFlag(t *testing.T) {
	var capturedParams task.InitParams
	mock := &mockManager{
		initFn: func(_ context.Context, params task.InitParams) error {
			capturedParams = params
			return nil
		},
	}
	root := buildTestRoot(mock, "dev")
	_, _, err := executeCommand(root, "init", "--branch-prefix", "bugfix/", "IN-100", "svc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedParams.BranchPrefix != "bugfix/" {
		t.Errorf("expected BranchPrefix=bugfix/, got: %q", capturedParams.BranchPrefix)
	}
}

func TestInit_BaseFlag(t *testing.T) {
	var capturedParams task.InitParams
	mock := &mockManager{
		initFn: func(_ context.Context, params task.InitParams) error {
			capturedParams = params
			return nil
		},
	}
	root := buildTestRoot(mock, "dev")
	_, _, err := executeCommand(root, "init", "--base", "develop", "IN-100", "svc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedParams.BaseBranch != "develop" {
		t.Errorf("expected BaseBranch=develop, got: %q", capturedParams.BaseBranch)
	}
}

// ── add tests ────────────────────────────────────────────────────────────────

func TestAdd_ErrTaskNotFound_ExitsCode1(t *testing.T) {
	mock := &mockManager{
		addFn: func(_ context.Context, _ task.AddParams) error {
			return errors.Join(task.ErrTaskNotFound, errors.New("IN-9999"))
		},
	}
	root := buildTestRoot(mock, "dev")
	_, _, err := executeCommand(root, "add", "IN-9999", "collection")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	code := exitCode(err)
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

// ── remove tests ─────────────────────────────────────────────────────────────

func TestRemove_ErrTaskNotFound_ExitsCode1(t *testing.T) {
	mock := &mockManager{
		removeFn: func(_ context.Context, taskID string, force, _ bool) error {
			return errors.Join(task.ErrTaskNotFound, errors.New(taskID))
		},
	}
	root := buildTestRoot(mock, "dev")
	_, _, err := executeCommand(root, "remove", "IN-6748")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	code := exitCode(err)
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func TestRemove_GitExecError_ExitsCode2(t *testing.T) {
	mock := &mockManager{
		removeFn: func(_ context.Context, taskID string, force, _ bool) error {
			return &git.ExecError{
				Argv:     []string{"git", "worktree", "remove", "/path"},
				ExitCode: 128,
				Stderr:   "fatal: working tree has modifications",
			}
		},
	}
	root := buildTestRoot(mock, "dev")
	_, _, err := executeCommand(root, "remove", "IN-6748")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	code := exitCode(err)
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
}

func TestRemove_ForceFlag_PassedToManager(t *testing.T) {
	var capturedForce bool
	mock := &mockManager{
		removeFn: func(_ context.Context, _ string, force, _ bool) error {
			capturedForce = force
			return nil
		},
	}
	root := buildTestRoot(mock, "dev")
	_, _, err := executeCommand(root, "remove", "--force", "IN-6748")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !capturedForce {
		t.Error("expected force=true, got false")
	}
}

func TestRemove_Success(t *testing.T) {
	mock := &mockManager{
		removeFn: func(_ context.Context, _ string, _, _ bool) error { return nil },
	}
	root := buildTestRoot(mock, "dev")
	stdout, _, err := executeCommand(root, "remove", "IN-6748")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "IN-6748") {
		t.Errorf("expected task ID in output, got: %q", stdout)
	}
}

// ── version tests ─────────────────────────────────────────────────────────────

func TestVersion_PrintsVersionString(t *testing.T) {
	root := buildTestRoot(&mockManager{}, "v1.2.3")
	stdout, _, err := executeCommand(root, "version")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "v1.2.3") {
		t.Errorf("expected 'v1.2.3' in output, got: %q", stdout)
	}
}

func TestVersion_DevDefault(t *testing.T) {
	root := buildTestRoot(&mockManager{}, "dev")
	stdout, _, err := executeCommand(root, "version")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "dev") {
		t.Errorf("expected 'dev' in output, got: %q", stdout)
	}
}

// ── exit code mapping tests ───────────────────────────────────────────────────

func TestExitCode_Nil_Returns0(t *testing.T) {
	if got := exitCode(nil); got != 0 {
		t.Errorf("exitCode(nil) = %d, want 0", got)
	}
}

func TestExitCode_ErrTaskNotFound_Returns1(t *testing.T) {
	err := fmt.Errorf("wrap: %w", task.ErrTaskNotFound)
	if got := exitCode(err); got != 1 {
		t.Errorf("exitCode(ErrTaskNotFound) = %d, want 1", got)
	}
}

func TestExitCode_ErrTaskExists_Returns1(t *testing.T) {
	if got := exitCode(task.ErrTaskExists); got != 1 {
		t.Errorf("exitCode(ErrTaskExists) = %d, want 1", got)
	}
}

func TestExitCode_ErrServiceNotFound_Returns1(t *testing.T) {
	if got := exitCode(task.ErrServiceNotFound); got != 1 {
		t.Errorf("exitCode(ErrServiceNotFound) = %d, want 1", got)
	}
}

func TestExitCode_GitExecError_Returns2(t *testing.T) {
	err := &git.ExecError{Argv: []string{"git", "worktree", "add"}, ExitCode: 1}
	if got := exitCode(err); got != 2 {
		t.Errorf("exitCode(git.ExecError) = %d, want 2", got)
	}
}

func TestExitCode_GitExecErrorWrapped_Returns2(t *testing.T) {
	execErr := &git.ExecError{Argv: []string{"git", "status"}, ExitCode: 128}
	wrapped := fmt.Errorf("some context: %w", execErr)
	if got := exitCode(wrapped); got != 2 {
		t.Errorf("exitCode(wrapped git.ExecError) = %d, want 2", got)
	}
}

func TestExitCode_UnknownError_Returns3(t *testing.T) {
	err := errors.New("some filesystem error")
	if got := exitCode(err); got != 3 {
		t.Errorf("exitCode(unknown) = %d, want 3", got)
	}
}

// ── --root flag tests ─────────────────────────────────────────────────────────

// TestRootFlag_OverridesConfigRootDir verifies that --root causes cfg.RootDir
// to be updated during dependency setup. We test this indirectly by hooking
// into a command that exposes cfg state, or by simply verifying the flag plumbing
// doesn't error. A more complete integration would require the full setup pipeline;
// here we confirm the flag is accepted without parse errors.
func TestRootFlag_IsAccepted(t *testing.T) {
	// Override mgr so PersistentPreRunE doesn't try to load real config.
	// The buildTestRoot helper bypasses PersistentPreRunE entirely.
	mock := &mockManager{
		listFn: func(_ context.Context) ([]domain.Task, error) {
			return []domain.Task{}, nil
		},
	}
	root := buildTestRoot(mock, "dev")
	// --root is a persistent flag; it must be parsed without error.
	_, _, err := executeCommand(root, "--root", "/tmp/test-root", "list")
	if err != nil {
		t.Fatalf("unexpected error with --root flag: %v", err)
	}
}

// ── sln command tests ─────────────────────────────────────────────────────────

func TestSln_Success(t *testing.T) {
	var capturedTaskID string
	mock := &mockManager{
		generateSlnFn: func(_ context.Context, taskID string) error {
			capturedTaskID = taskID
			return nil
		},
	}
	root := buildTestRoot(mock, "dev")
	stdout, _, err := executeCommand(root, "sln", "IN-6748")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedTaskID != "IN-6748" {
		t.Errorf("expected taskID=IN-6748, got: %q", capturedTaskID)
	}
	if !strings.Contains(stdout, "IN-6748") {
		t.Errorf("expected task ID in output, got: %q", stdout)
	}
}

func TestSln_ErrTaskNotFound_ExitsCode1(t *testing.T) {
	mock := &mockManager{
		generateSlnFn: func(_ context.Context, taskID string) error {
			return task.ErrTaskNotFound
		},
	}
	root := buildTestRoot(mock, "dev")
	_, _, err := executeCommand(root, "sln", "IN-9999")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if code := exitCode(err); code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

// ── open command tests ────────────────────────────────────────────────────────

// executeCommandWithStdin is like executeCommand but also injects a custom
// stdin reader. Useful for commands that read interactive prompts.
func executeCommandWithStdin(root *cobra.Command, stdin *bytes.Buffer, args ...string) (stdout, stderr string, err error) {
	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}
	root.SetOut(stdoutBuf)
	root.SetErr(stderrBuf)
	root.SetIn(stdin)
	root.SetArgs(args)
	err = root.Execute()
	return stdoutBuf.String(), stderrBuf.String(), err
}

// ── config list tests ─────────────────────────────────────────────────────────

func TestConfigList_PrintsEffectiveConfig(t *testing.T) {
	mock := &mockManager{}
	root := buildTestRoot(mock, "dev")

	// Populate the package-level cfg that config list reads.
	cfg = &config.Config{
		RootDir:          "/test/root",
		TasksRoot:        "/test/tasks",
		BranchPrefix:     "feature/",
		Editor:           "code",
		DiscoveryDepth:   4,
		OutputPanelLines: 6,
		LogLevel:         "INFO",
	}
	t.Cleanup(func() { cfg = nil })

	stdout, _, err := executeCommand(root, "config", "list")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !strings.Contains(stdout, "root_dir:") {
		t.Errorf("expected root_dir in output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "editor:") {
		t.Errorf("expected editor in output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "log_level:") {
		t.Errorf("expected log_level in output, got: %q", stdout)
	}
}

// ── config set tests ──────────────────────────────────────────────────────────

func TestConfigSet_UpdatesKeyInFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	// Write initial config.
	if err := os.WriteFile(cfgPath, []byte("editor: code\nlog_level: INFO\n"), 0o644); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	mock := &mockManager{}
	root := buildTestRoot(mock, "dev")

	// Override cfgFile so config set knows where to write.
	cfgFile = cfgPath
	t.Cleanup(func() { cfgFile = "" })

	stdout, _, err := executeCommand(root, "config", "set", "editor", "rider")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !strings.Contains(stdout, "editor") {
		t.Errorf("expected confirmation in output, got: %q", stdout)
	}

	// Verify the file was updated.
	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if reloaded.Editor != "rider" {
		t.Errorf("Editor: got %q, want rider", reloaded.Editor)
	}
	// Other keys must be preserved.
	if reloaded.LogLevel != "INFO" {
		t.Errorf("LogLevel: got %q, want INFO", reloaded.LogLevel)
	}
}

func TestConfigSet_UnknownKey_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	mock := &mockManager{}
	root := buildTestRoot(mock, "dev")

	cfgFile = cfgPath
	t.Cleanup(func() { cfgFile = "" })

	_, _, err := executeCommand(root, "config", "set", "unknown_key", "value")
	if err == nil {
		t.Fatal("expected error for unknown key, got nil")
	}
}
