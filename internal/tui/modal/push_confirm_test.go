package modal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPushConfirmDialog_ImplementsModal(t *testing.T) {
	var _ Modal = NewPushConfirmDialog("IN-101", "", nil)
}

func TestPushConfirmDialog_ViewShowsTaskAndWarnings(t *testing.T) {
	d := NewPushConfirmDialog("IN-101", "", []PushTargetInfo{
		{
			ServiceName: "api",
			Branch:      "main",
			RemoteName:  "origin",
			RemoteURL:   "git@github.com:org/api.git",
			Protected:   true,
		},
		{
			ServiceName: "worker",
			Branch:      "",
		},
	})

	view := stripAnsi(d.View())
	for _, want := range []string{
		"Push Confirmation",
		"Operation: task-wide push",
		"Task: IN-101",
		"Services: api, worker",
		"- main",
		"origin (git@github.com:org/api.git)",
		"Warnings:",
		"blank branch name",
		"protected branch",
		"Push? [Enter/y] confirm [Esc/n] cancel",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q: %s", want, view)
		}
	}
}

func TestPushConfirmDialog_EnterEmitsSubmitPushMsg(t *testing.T) {
	d := NewPushConfirmDialog("IN-102", "api", []PushTargetInfo{{ServiceName: "api", Branch: "feature/IN-102"}})
	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter must emit submit cmd")
	}

	msg := execCmd(cmd)
	sub, ok := msg.(SubmitPushMsg)
	if !ok {
		t.Fatalf("expected SubmitPushMsg, got %T", msg)
	}
	if sub.TaskID != "IN-102" || sub.ServiceName != "api" {
		t.Fatalf("unexpected submit payload: %+v", sub)
	}
}

func TestPushConfirmDialog_YEmitsSubmitPushMsg(t *testing.T) {
	d := NewPushConfirmDialog("IN-103", "", []PushTargetInfo{{ServiceName: "api", Branch: "feature/IN-103"}})
	_, cmd := d.Update(sendKey("y"))
	if cmd == nil {
		t.Fatal("y must emit submit cmd")
	}
	if _, ok := execCmd(cmd).(SubmitPushMsg); !ok {
		t.Fatalf("expected SubmitPushMsg, got %T", execCmd(cmd))
	}
}

func TestPushConfirm_ProtectedFlagTrue_WarnsProtected(t *testing.T) {
	d := NewPushConfirmDialog("IN-106", "", []PushTargetInfo{
		{ServiceName: "api", Branch: "main", Protected: true},
	})
	view := stripAnsi(d.View())
	if !strings.Contains(view, "protected branch") {
		t.Fatalf("expected protected branch warning, got:\n%s", view)
	}
}

func TestPushConfirm_ProtectedFlagFalse_NoProtectedWarning(t *testing.T) {
	d := NewPushConfirmDialog("IN-107", "", []PushTargetInfo{
		{ServiceName: "api", Branch: "main", Protected: false},
	})
	view := stripAnsi(d.View())
	if strings.Contains(view, "protected branch") {
		t.Fatalf("unexpected protected branch warning, got:\n%s", view)
	}
}

func TestPushConfirm_BlankBranch_WarnsEvenIfNotProtected(t *testing.T) {
	d := NewPushConfirmDialog("IN-108", "", []PushTargetInfo{
		{ServiceName: "api", Branch: "", Protected: false},
	})
	view := stripAnsi(d.View())
	if !strings.Contains(view, "blank branch name") {
		t.Fatalf("expected blank branch warning, got:\n%s", view)
	}
	if strings.Contains(view, "protected branch") {
		t.Fatalf("unexpected protected branch warning for blank branch, got:\n%s", view)
	}
}

func TestPushConfirm_DetachedHead_WarnsEvenIfNotProtected(t *testing.T) {
	d := NewPushConfirmDialog("IN-109", "", []PushTargetInfo{
		{ServiceName: "api", Branch: "HEAD", Protected: false},
	})
	view := stripAnsi(d.View())
	if !strings.Contains(view, "detached HEAD") {
		t.Fatalf("expected detached HEAD warning, got:\n%s", view)
	}
	if strings.Contains(view, "protected branch") {
		t.Fatalf("unexpected protected branch warning for detached HEAD, got:\n%s", view)
	}
}

func TestPushConfirmDialog_EscAndNClose(t *testing.T) {
	t.Run("esc", func(t *testing.T) {
		d := NewPushConfirmDialog("IN-104", "api", nil)
		_, cmd := d.Update(sendSpecialKey(tea.KeyEsc))
		if cmd == nil {
			t.Fatal("esc must emit close cmd")
		}
		if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
			t.Fatalf("expected CloseModalMsg, got %T", execCmd(cmd))
		}
	})

	t.Run("n", func(t *testing.T) {
		d := NewPushConfirmDialog("IN-105", "api", nil)
		_, cmd := d.Update(sendKey("n"))
		if cmd == nil {
			t.Fatal("n must emit close cmd")
		}
		if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
			t.Fatalf("expected CloseModalMsg, got %T", execCmd(cmd))
		}
	})
}
