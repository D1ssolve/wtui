package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	"github.com/D1ssolve/wtui/internal/tui/panels"
)

func TestE2E_ReleaseFlow_FocusOpenCreateAndSubmit(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 140, 40)
	m.tasks = []domain.Task{{
		ID:       "ZA-553",
		Phase:    "feature",
		ParentID: "",
		Services: []domain.Service{{Name: "svc-api"}, {Name: "svc-worker"}},
	}}

	t.Log("Tab cycle reaches releases from tasks")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.focus != FocusServices {
		t.Fatalf("focus after tab 1 = %v, want %v", m.focus, FocusServices)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.focus != FocusOutput {
		t.Fatalf("focus after tab 2 = %v, want %v", m.focus, FocusOutput)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.focus != FocusReleases {
		t.Fatalf("focus after tab 3 = %v, want %v", m.focus, FocusReleases)
	}

	t.Log("Key 3 focuses releases")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m = updated.(Model)
	if m.focus != FocusTasks {
		t.Fatalf("focus after key 1 = %v, want %v", m.focus, FocusTasks)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	m = updated.(Model)
	if m.focus != FocusReleases {
		t.Fatalf("focus after key 3 = %v, want %v", m.focus, FocusReleases)
	}

	t.Log("Key N opens create release dialog")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("N")})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("N should emit open create dialog command")
	}
	openMsg, ok := cmd().(panels.OpenCreateReleaseDialogMsg)
	if !ok {
		t.Fatalf("expected OpenCreateReleaseDialogMsg, got %T", cmd())
	}

	updated, _ = m.Update(openMsg)
	m = updated.(Model)
	dialog, ok := m.modal.(*modal.CreateReleaseDialog)
	if !ok {
		t.Fatalf("expected CreateReleaseDialog, got %T", m.modal)
	}
	if dialog.Title() != "Create Release — Select Tasks" {
		t.Fatalf("phase 1 title = %q", dialog.Title())
	}

	t.Log("Phase 1 -> Phase 2 -> submit emits SubmitCreateReleaseMsg")
	updated, _ = m.Update(sendKey(" "))
	m = updated.(Model)

	updated, _ = m.Update(sendSpecialKey(KeyEnter))
	m = updated.(Model)
	dialog, ok = m.modal.(*modal.CreateReleaseDialog)
	if !ok {
		t.Fatalf("expected CreateReleaseDialog after enter, got %T", m.modal)
	}
	if dialog.Title() != "Create Release — Enter Versions" {
		t.Fatalf("phase 2 title = %q", dialog.Title())
	}

	updated, _ = m.Update(panels.ReleaseVersionsLoadedMsg{Versions: map[string]string{
		"svc-api":    "1.2.3",
		"svc-worker": "2.0.0",
	}})
	m = updated.(Model)

	updated, submitCmd := m.Update(sendSpecialKey(KeyEnter))
	m = updated.(Model)
	if submitCmd == nil {
		t.Fatal("phase 2 enter with valid versions should emit submit cmd")
	}
	submitMsg, ok := submitCmd().(modal.SubmitCreateReleaseMsg)
	if !ok {
		t.Fatalf("expected SubmitCreateReleaseMsg, got %T", submitCmd())
	}
	if len(submitMsg.TaskIDs) != 1 || submitMsg.TaskIDs[0] != "ZA-553" {
		t.Fatalf("submit task IDs = %+v, want [ZA-553]", submitMsg.TaskIDs)
	}
	if submitMsg.Versions["svc-api"] != "1.2.3" || submitMsg.Versions["svc-worker"] != "2.0.0" {
		t.Fatalf("submit versions = %+v", submitMsg.Versions)
	}
}

func TestE2E_ReleaseFlow_QInTasksFocus_IsNoOp(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)
	m.tasksPanel.SetTasks([]domain.Task{{ID: "ZA-553", Phase: "feature", ParentID: ""}})

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Q")})
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("Q should be no-op")
	}
	if m.modal != nil {
		t.Fatalf("Q should not open modal, got %T", m.modal)
	}
}
