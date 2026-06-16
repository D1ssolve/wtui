package task

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDiscoverServicesFromTaskDir_SortsAndFiltersDirectories(t *testing.T) {
	taskDir := t.TempDir()

	for _, name := range []string{"worker", "api", ".git"} {
		if err := os.MkdirAll(filepath.Join(taskDir, name), 0o755); err != nil {
			t.Fatalf("setup directory %s: %v", name, err)
		}
	}

	if err := os.WriteFile(filepath.Join(taskDir, "README.md"), []byte("ignore"), 0o644); err != nil {
		t.Fatalf("setup file: %v", err)
	}

	services, err := discoverServicesFromTaskDir(taskDir)
	if err != nil {
		t.Fatalf("discoverServicesFromTaskDir error: %v", err)
	}

	gotNames := make([]string, 0, len(services))
	gotPaths := make([]string, 0, len(services))
	for _, svc := range services {
		gotNames = append(gotNames, svc.Name)
		gotPaths = append(gotPaths, svc.RepoPath)
	}

	if !reflect.DeepEqual(gotNames, []string{"api", "worker"}) {
		t.Fatalf("service names = %v, want [api worker]", gotNames)
	}

	wantPaths := []string{filepath.Join(taskDir, "api"), filepath.Join(taskDir, "worker")}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("repo paths = %v, want %v", gotPaths, wantPaths)
	}
}

func TestDiscoverServicesFromTaskDir_ReadDirFailure_ReturnsError(t *testing.T) {
	missingDir := filepath.Join(t.TempDir(), "missing")

	services, err := discoverServicesFromTaskDir(missingDir)
	if err == nil {
		t.Fatalf("expected error for missing dir, got nil")
	}
	if services != nil {
		t.Fatalf("services = %v, want nil", services)
	}
}

func TestRemoveGeneratedTaskFiles_FilesExist_RemovesWorkspaceAndSolution(t *testing.T) {
	taskDir := t.TempDir()
	taskID := "APP-123"

	workspacePath := filepath.Join(taskDir, taskID+".code-workspace")
	solutionPath := filepath.Join(taskDir, taskID+".sln")

	if err := os.WriteFile(workspacePath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("setup workspace file: %v", err)
	}
	if err := os.WriteFile(solutionPath, []byte(""), 0o644); err != nil {
		t.Fatalf("setup solution file: %v", err)
	}

	if err := removeGeneratedTaskFiles(taskDir, taskID); err != nil {
		t.Fatalf("removeGeneratedTaskFiles error: %v", err)
	}

	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		t.Fatalf("workspace still exists, stat error = %v", err)
	}
	if _, err := os.Stat(solutionPath); !os.IsNotExist(err) {
		t.Fatalf("solution still exists, stat error = %v", err)
	}
}

func TestRemoveGeneratedTaskFiles_FilesMissing_IgnoresMissing(t *testing.T) {
	taskDir := t.TempDir()
	taskID := "APP-123"

	if err := removeGeneratedTaskFiles(taskDir, taskID); err != nil {
		t.Fatalf("removeGeneratedTaskFiles error: %v", err)
	}
}
