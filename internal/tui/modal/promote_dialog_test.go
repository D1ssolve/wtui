package modal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestPromoteToReleaseDialog_OpensInLoadingState(t *testing.T) {
	d := NewPromoteToReleaseDialog(
		"IN-1234",
		[]domain.Service{{Name: "api"}, {Name: "worker"}},
		"develop",
		100,
		30,
	)

	if !d.loading {
		t.Fatal("dialog should start in loading state")
	}

	view := stripAnsi(d.View())
	if !strings.Contains(view, "This will create release branches, push them to origin, add new worktrees, and generate a new task directory.") {
		t.Fatalf("view should show promote operation warning, got: %q", view)
	}
	if !strings.Contains(view, "Loading proposed versions") {
		t.Fatalf("view should show loading hint, got: %q", view)
	}
	if !strings.Contains(view, "…") {
		t.Fatalf("view should show placeholder ellipsis, got: %q", view)
	}
}

func TestPromoteToReleaseDialog_SetProposedVersions_PopulatesFields(t *testing.T) {
	d := NewPromoteToReleaseDialog(
		"IN-1234",
		[]domain.Service{{Name: "api"}, {Name: "worker"}},
		"develop",
		100,
		30,
	)

	d.SetProposedVersions(map[string]string{
		"api":    "v1.2.3",
		"worker": "v2.0.0",
	})

	if d.loading {
		t.Fatal("dialog should leave loading state after proposals")
	}

	got := d.Versions()
	if got["api"] != "v1.2.3" {
		t.Fatalf("api version mismatch: got %q", got["api"])
	}
	if got["worker"] != "v2.0.0" {
		t.Fatalf("worker version mismatch: got %q", got["worker"])
	}
}

func TestPromoteToReleaseDialog_InvalidVersion_EnterBlockedAndErrorShown(t *testing.T) {
	d := NewPromoteToReleaseDialog(
		"IN-1234",
		[]domain.Service{{Name: "api"}},
		"develop",
		100,
		30,
	)

	d.SetProposedVersions(map[string]string{"api": "v1.2.3"})
	d.versions[0].value = "not-semver"

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd != nil {
		t.Fatal("enter with invalid version must not emit submit cmd")
	}

	view := stripAnsi(d.View())
	if !strings.Contains(view, "Invalid semver") {
		t.Fatalf("expected inline validation error, got: %q", view)
	}
}

func TestPromoteToReleaseDialog_ValidEnter_EmitsPromoteToReleaseMsg(t *testing.T) {
	d := NewPromoteToReleaseDialog(
		"IN-1234",
		[]domain.Service{{Name: "api"}, {Name: "worker"}},
		"develop",
		100,
		30,
	)

	d.SetProposedVersions(map[string]string{
		"api":    "v1.2.3",
		"worker": "v2.0.0",
	})

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter with valid versions must emit cmd")
	}

	msg := execCmd(cmd)
	sub, ok := msg.(PromoteToReleaseMsg)
	if !ok {
		t.Fatalf("expected PromoteToReleaseMsg, got %T", msg)
	}

	if sub.TaskID != "IN-1234" {
		t.Fatalf("TaskID mismatch: got %q", sub.TaskID)
	}
	if sub.Versions["api"] != "v1.2.3" || sub.Versions["worker"] != "v2.0.0" {
		t.Fatalf("unexpected versions payload: %+v", sub.Versions)
	}
}

func TestPromoteToReleaseDialog_Esc_EmitsCloseModalMsg(t *testing.T) {
	d := NewPromoteToReleaseDialog(
		"IN-1234",
		[]domain.Service{{Name: "api"}},
		"develop",
		100,
		30,
	)

	_, cmd := d.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("esc must return close cmd")
	}

	msg := execCmd(cmd)
	if _, ok := msg.(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", msg)
	}
}
