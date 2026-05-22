package tui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestRiderTaskArgsUsesTaskIDSolution(t *testing.T) {
	name, args := riderTaskArgs("IN-001")

	if name != "rider" {
		t.Fatalf("name = %q, want rider", name)
	}
	if len(args) != 1 || args[0] != "IN-001.sln" {
		t.Fatalf("args = %v, want [IN-001.sln]", args)
	}
}

func TestCodeWorkspaceTaskArgsUsesTaskIDWorkspace(t *testing.T) {
	name, args := codeWorkspaceTaskArgs("code", "IN-001")

	if name != "code" {
		t.Fatalf("name = %q, want code", name)
	}
	if len(args) != 1 || args[0] != "IN-001.code-workspace" {
		t.Fatalf("args = %v, want [IN-001.code-workspace]", args)
	}
}

func TestCodeWorkspaceTaskArgsUsesConfiguredEditor(t *testing.T) {
	name, _ := codeWorkspaceTaskArgs("cursor", "MY-TASK")
	if name != "cursor" {
		t.Fatalf("name = %q, want cursor", name)
	}
}

func TestExecTeaProcessReturnsOriginalErrorAndOp(t *testing.T) {
	original := errors.New("rider failed")
	msg := execProcessDoneMsg("Open Rider for IN-001", original)
	done, ok := msg.(CommandDoneMsg)
	if !ok {
		t.Fatalf("msg = %T, want CommandDoneMsg", msg)
	}
	if !errors.Is(done.Err, original) {
		t.Fatalf("err = %v, want original error", done.Err)
	}
	if strings.Contains(done.Err.Error(), "shell:") {
		t.Fatalf("err = %q, must not add shell-specific context", done.Err.Error())
	}
	if done.Op != "Open Rider for IN-001" {
		t.Fatalf("op = %q, want Open Rider for IN-001", done.Op)
	}

	msg = execProcessDoneMsg("Open Rider for IN-001", nil)
	done, ok = msg.(CommandDoneMsg)
	if !ok {
		t.Fatalf("msg = %T, want CommandDoneMsg", msg)
	}
	if done.Err != nil {
		t.Fatalf("err = %v, want nil", done.Err)
	}
}

func TestLazygitServiceArgsUsesWorktreePath(t *testing.T) {
	name, args := lazygitServiceArgs("/tmp/service")

	if name != "lazygit" {
		t.Fatalf("name = %q, want lazygit", name)
	}
	if len(args) != 2 || args[0] != "-p" || args[1] != "/tmp/service" {
		t.Fatalf("args = %v, want [-p /tmp/service]", args)
	}
}

func TestLazygitServiceExecCmdUsesWorktreeDir(t *testing.T) {
	cmd := lazygitServiceExecCmd("/tmp/service")

	if filepath.Base(cmd.Path) != "lazygit" {
		t.Fatalf("Path = %q, want lazygit executable", cmd.Path)
	}
	if cmd.Dir != "/tmp/service" {
		t.Fatalf("Dir = %q, want /tmp/service", cmd.Dir)
	}
	if len(cmd.Args) != 3 || cmd.Args[0] != "lazygit" || cmd.Args[1] != "-p" || cmd.Args[2] != "/tmp/service" {
		t.Fatalf("Args = %v, want [lazygit -p /tmp/service]", cmd.Args)
	}
}

func TestLazygitDoneMessagePreservesMetadataAndError(t *testing.T) {
	original := errors.New("lazygit failed")
	msg := lazygitServiceDoneMsg("IN-001", "collection", "/tmp/service", original)

	got, ok := msg.(LazygitDoneMsg)
	if !ok {
		t.Fatalf("msg = %T, want LazygitDoneMsg", msg)
	}
	if got.TaskID != "IN-001" {
		t.Errorf("TaskID = %q, want IN-001", got.TaskID)
	}
	if got.ServiceName != "collection" {
		t.Errorf("ServiceName = %q, want collection", got.ServiceName)
	}
	if got.WorktreePath != "/tmp/service" {
		t.Errorf("WorktreePath = %q, want /tmp/service", got.WorktreePath)
	}
	if !errors.Is(got.Err, original) {
		t.Fatalf("Err = %v, want original", got.Err)
	}
}
