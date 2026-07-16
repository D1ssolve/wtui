package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
)

type recordingSlnGenerator struct {
	err   error
	calls []slnGenerateCall
}

type slnGenerateCall struct {
	taskDir  string
	taskID   string
	services []domain.Service
}

func (r *recordingSlnGenerator) Generate(_ context.Context, taskDir, taskID string, services []domain.Service) error {
	cloned := append([]domain.Service(nil), services...)
	r.calls = append(r.calls, slnGenerateCall{
		taskDir:  taskDir,
		taskID:   taskID,
		services: cloned,
	})
	return r.err
}

func newRemoveServiceManager(tasksRoot string, gitMock *mockGitClient, slnGen *recordingSlnGenerator) *manager {
	return &manager{
		cfg:    &config.Config{TasksRoot: tasksRoot},
		git:    gitMock,
		slnMgr: slnGen,
		logger: newTestLogger(),
	}
}

func TestRemoveService_Success_RegeneratesWorkspaceAndSlnForRemainingServices(t *testing.T) {
	tasksRoot := t.TempDir()
	taskID := "TASK-101"
	taskDir := filepath.Join(tasksRoot, taskID)
	if err := os.MkdirAll(filepath.Join(taskDir, "api"), 0o755); err != nil {
		t.Fatalf("setup api dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(taskDir, "worker"), 0o755); err != nil {
		t.Fatalf("setup worker dir: %v", err)
	}

	workspacePath := filepath.Join(taskDir, taskID+".code-workspace")
	if err := os.WriteFile(workspacePath, []byte("old workspace\n"), 0o644); err != nil {
		t.Fatalf("setup workspace file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, taskID+".sln"), []byte("old sln\n"), 0o644); err != nil {
		t.Fatalf("setup sln file: %v", err)
	}

	gitMock := &mockGitClient{
		commonDirResult: "/tmp/common",
		removeWorktreeFn: func(_ string, worktreePath string, _ bool) error {
			return os.RemoveAll(worktreePath)
		},
	}
	slnGen := &recordingSlnGenerator{}
	mgr := newRemoveServiceManager(tasksRoot, gitMock, slnGen)

	err := mgr.RemoveService(context.Background(), taskID, "api", false)
	if err != nil {
		t.Fatalf("RemoveService() error = %v", err)
	}

	workspaceData, err := os.ReadFile(workspacePath)
	if err != nil {
		t.Fatalf("read workspace file: %v", err)
	}

	workspace := string(workspaceData)
	if !strings.Contains(workspace, `"path": "worker"`) {
		t.Fatalf("workspace missing remaining service: %s", workspace)
	}
	if strings.Contains(workspace, `"path": "api"`) {
		t.Fatalf("workspace still contains removed service: %s", workspace)
	}

	if len(slnGen.calls) != 1 {
		t.Fatalf("sln Generate call count = %d, want 1", len(slnGen.calls))
	}

	call := slnGen.calls[0]
	if call.taskDir != taskDir {
		t.Fatalf("sln taskDir = %q, want %q", call.taskDir, taskDir)
	}
	if call.taskID != taskID {
		t.Fatalf("sln taskID = %q, want %q", call.taskID, taskID)
	}
	if len(call.services) != 1 {
		t.Fatalf("sln services len = %d, want 1", len(call.services))
	}
	if call.services[0].Name != "worker" {
		t.Fatalf("sln service name = %q, want worker", call.services[0].Name)
	}
	wantWorktree := filepath.Join(taskDir, "worker")
	if call.services[0].WorktreePath != wantWorktree {
		t.Fatalf("sln service worktree = %q, want %q", call.services[0].WorktreePath, wantWorktree)
	}
}

func TestRemoveService_LastService_RemovesGeneratedFiles(t *testing.T) {
	tasksRoot := t.TempDir()
	taskID := "TASK-102"
	taskDir := filepath.Join(tasksRoot, taskID)
	if err := os.MkdirAll(filepath.Join(taskDir, "api"), 0o755); err != nil {
		t.Fatalf("setup api dir: %v", err)
	}

	workspacePath := filepath.Join(taskDir, taskID+".code-workspace")
	slnPath := filepath.Join(taskDir, taskID+".sln")
	if err := os.WriteFile(workspacePath, []byte("workspace\n"), 0o644); err != nil {
		t.Fatalf("setup workspace file: %v", err)
	}
	if err := os.WriteFile(slnPath, []byte("sln\n"), 0o644); err != nil {
		t.Fatalf("setup sln file: %v", err)
	}

	gitMock := &mockGitClient{
		commonDirResult: "/tmp/common",
		removeWorktreeFn: func(_ string, worktreePath string, _ bool) error {
			return os.RemoveAll(worktreePath)
		},
	}
	slnGen := &recordingSlnGenerator{}
	mgr := newRemoveServiceManager(tasksRoot, gitMock, slnGen)

	err := mgr.RemoveService(context.Background(), taskID, "api", false)
	if err != nil {
		t.Fatalf("RemoveService() error = %v", err)
	}

	if _, err := os.Stat(workspacePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("workspace file stat err = %v, want not exist", err)
	}
	if _, err := os.Stat(slnPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sln file stat err = %v, want not exist", err)
	}
	if len(slnGen.calls) != 0 {
		t.Fatalf("sln Generate call count = %d, want 0", len(slnGen.calls))
	}
}

func TestRemoveService_LastService_MissingGeneratedFilesStillSucceeds(t *testing.T) {
	tasksRoot := t.TempDir()
	taskID := "TASK-103"
	taskDir := filepath.Join(tasksRoot, taskID)
	if err := os.MkdirAll(filepath.Join(taskDir, "api"), 0o755); err != nil {
		t.Fatalf("setup api dir: %v", err)
	}

	gitMock := &mockGitClient{
		commonDirResult: "/tmp/common",
		removeWorktreeFn: func(_ string, worktreePath string, _ bool) error {
			return os.RemoveAll(worktreePath)
		},
	}
	slnGen := &recordingSlnGenerator{}
	mgr := newRemoveServiceManager(tasksRoot, gitMock, slnGen)

	err := mgr.RemoveService(context.Background(), taskID, "api", false)
	if err != nil {
		t.Fatalf("RemoveService() error = %v", err)
	}

	if len(slnGen.calls) != 0 {
		t.Fatalf("sln Generate call count = %d, want 0", len(slnGen.calls))
	}
}

func TestRemoveService_RemoveWorktreeFailure_DoesNotTouchGeneratedFiles(t *testing.T) {
	tasksRoot := t.TempDir()
	taskID := "TASK-104"
	taskDir := filepath.Join(tasksRoot, taskID)
	if err := os.MkdirAll(filepath.Join(taskDir, "api"), 0o755); err != nil {
		t.Fatalf("setup api dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(taskDir, "worker"), 0o755); err != nil {
		t.Fatalf("setup worker dir: %v", err)
	}

	workspacePath := filepath.Join(taskDir, taskID+".code-workspace")
	slnPath := filepath.Join(taskDir, taskID+".sln")
	wantWorkspace := "workspace-before\n"
	wantSln := "sln-before\n"
	if err := os.WriteFile(workspacePath, []byte(wantWorkspace), 0o644); err != nil {
		t.Fatalf("setup workspace file: %v", err)
	}
	if err := os.WriteFile(slnPath, []byte(wantSln), 0o644); err != nil {
		t.Fatalf("setup sln file: %v", err)
	}

	gitMock := &mockGitClient{
		commonDirResult:   "/tmp/common",
		removeWorktreeErr: errors.New("boom remove worktree"),
	}
	slnGen := &recordingSlnGenerator{}
	mgr := newRemoveServiceManager(tasksRoot, gitMock, slnGen)

	err := mgr.RemoveService(context.Background(), taskID, "api", false)
	if err == nil {
		t.Fatal("RemoveService() error = nil, want error")
	}

	workspaceData, readErr := os.ReadFile(workspacePath)
	if readErr != nil {
		t.Fatalf("read workspace file: %v", readErr)
	}
	if string(workspaceData) != wantWorkspace {
		t.Fatalf("workspace content changed = %q, want %q", string(workspaceData), wantWorkspace)
	}

	slnData, readErr := os.ReadFile(slnPath)
	if readErr != nil {
		t.Fatalf("read sln file: %v", readErr)
	}
	if string(slnData) != wantSln {
		t.Fatalf("sln content changed = %q, want %q", string(slnData), wantSln)
	}

	if len(slnGen.calls) != 0 {
		t.Fatalf("sln Generate call count = %d, want 0", len(slnGen.calls))
	}
}

func TestRemoveService_DeleteBranchFailure_RegeneratesWorkspaceAndSlnAndReturnsError(t *testing.T) {
	tasksRoot := t.TempDir()
	taskID := "TASK-105"
	taskDir := filepath.Join(tasksRoot, taskID)
	if err := os.MkdirAll(filepath.Join(taskDir, "api"), 0o755); err != nil {
		t.Fatalf("setup api dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(taskDir, "worker"), 0o755); err != nil {
		t.Fatalf("setup worker dir: %v", err)
	}

	workspacePath := filepath.Join(taskDir, taskID+".code-workspace")
	slnPath := filepath.Join(taskDir, taskID+".sln")
	wantWorkspace := "workspace-before\n"
	wantSln := "sln-before\n"
	if err := os.WriteFile(workspacePath, []byte(wantWorkspace), 0o644); err != nil {
		t.Fatalf("setup workspace file: %v", err)
	}
	if err := os.WriteFile(slnPath, []byte(wantSln), 0o644); err != nil {
		t.Fatalf("setup sln file: %v", err)
	}

	gitMock := &mockGitClient{
		commonDirResult:      "/tmp/common",
		worktreeBranchResult: "feature/TASK-105",
		deleteBranchErr:      errors.New("boom delete branch"),
		removeWorktreeFn: func(_ string, worktreePath string, _ bool) error {
			return os.RemoveAll(worktreePath)
		},
	}
	slnGen := &recordingSlnGenerator{}
	mgr := newRemoveServiceManager(tasksRoot, gitMock, slnGen)

	err := mgr.RemoveService(context.Background(), taskID, "api", true)
	if err == nil {
		t.Fatal("RemoveService() error = nil, want error")
	}

	workspaceData, readErr := os.ReadFile(workspacePath)
	if readErr != nil {
		t.Fatalf("read workspace file: %v", readErr)
	}
	workspace := string(workspaceData)
	if !strings.Contains(workspace, `"path": "worker"`) {
		t.Fatalf("workspace missing remaining service: %s", workspace)
	}
	if strings.Contains(workspace, `"path": "api"`) {
		t.Fatalf("workspace still contains removed service: %s", workspace)
	}

	slnData, readErr := os.ReadFile(slnPath)
	if readErr != nil {
		t.Fatalf("read sln file: %v", readErr)
	}
	if string(slnData) != wantSln {
		t.Fatalf("sln file content changed unexpectedly = %q, want %q", string(slnData), wantSln)
	}

	if len(slnGen.calls) != 1 {
		t.Fatalf("sln Generate call count = %d, want 1", len(slnGen.calls))
	}
}
