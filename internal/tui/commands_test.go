package tui

import (
	"errors"
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
