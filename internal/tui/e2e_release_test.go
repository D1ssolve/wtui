package tui

import (
	"strings"
	"testing"
	"time"

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
	if m.focus != FocusReleases {
		t.Fatalf("focus after tab 2 = %v, want %v", m.focus, FocusReleases)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.focus != FocusOutput {
		t.Fatalf("focus after tab 3 = %v, want %v", m.focus, FocusOutput)
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

func TestE2E_FinishReleaseFlow_PreparedSelection_Finishes(t *testing.T) {
	preparedAt := time.Now().UTC()
	mgr := &mockManager{
		listReleasesResult: []domain.Release{{
			ID:         "rel-1",
			Status:     domain.ReleaseStatusPrepared,
			PreparedAt: &preparedAt,
			CreatedAt:  time.Now().UTC(),
			Services:   []domain.ReleaseService{{Name: "svc-api", Version: "1.2.3", Tag: "v1.2.3"}},
		}},
		finishReleaseResult: domain.Release{ID: "rel-1", Status: domain.ReleaseStatusReleased},
		finishReleaseDone:   make(chan struct{}),
	}

	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 140, 40)

	updated, _ := m.Update(ReleasesLoadedMsg{Releases: mgr.listReleasesResult})
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	m = updated.(Model)
	if m.focus != FocusReleases {
		t.Fatalf("focus = %v, want FocusReleases", m.focus)
	}

	updated, cmd := m.Update(sendKey("f"))
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("f should emit FinishReleaseMsg")
	}
	finishMsg, ok := cmd().(panels.FinishReleaseMsg)
	if !ok {
		t.Fatalf("expected FinishReleaseMsg, got %T", cmd())
	}

	updated, _ = m.Update(finishMsg)
	m = updated.(Model)
	dialog, ok := m.modal.(*modal.ReleaseFinishConfirmDialog)
	if !ok {
		t.Fatalf("expected ReleaseFinishConfirmDialog, got %T", m.modal)
	}

	updatedModal, confirmCmd := dialog.Update(sendSpecialKey(KeyEnter))
	dialog = updatedModal.(*modal.ReleaseFinishConfirmDialog)
	if confirmCmd == nil {
		t.Fatal("Enter should emit confirm command")
	}
	confirmMsg, ok := confirmCmd().(modal.ConfirmFinishReleaseMsg)
	if !ok {
		t.Fatalf("expected ConfirmFinishReleaseMsg, got %T", confirmCmd())
	}

	updated, opCmd := m.Update(confirmMsg)
	m = updated.(Model)
	if !m.opRunning {
		t.Fatal("opRunning should be true after confirm")
	}
	if m.modal != nil {
		t.Fatal("modal should be cleared after confirm")
	}
	if !strings.Contains(m.outputPanel.View(), "Finishing release rel-1...") {
		t.Fatalf("output should contain start line, got %q", m.outputPanel.View())
	}

	batch, ok := opCmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected tea.BatchMsg, got %T", opCmd())
	}

	var firstLine OutputLineMsg
	var foundLine bool
	for _, c := range batch {
		if c == nil {
			continue
		}
		if msg, ok := c().(OutputLineMsg); ok {
			firstLine = msg
			foundLine = true
		}
	}
	if !foundLine {
		t.Fatal("expected OutputLineMsg from finish release command")
	}

	select {
	case <-mgr.finishReleaseDone:
	case <-time.After(2 * time.Second):
		t.Fatal("FinishRelease was not called")
	}

	updated, nextCmd := m.Update(firstLine)
	m = updated.(Model)
	if nextCmd == nil {
		t.Fatal("OutputLineMsg should carry next command")
	}
	doneMsg, ok := nextCmd().(FinishReleaseDoneMsg)
	if !ok {
		t.Fatalf("expected FinishReleaseDoneMsg, got %T", nextCmd())
	}

	updated, reloadCmd := m.Update(doneMsg)
	m = updated.(Model)
	if m.opRunning {
		t.Fatal("opRunning should be false after FinishReleaseDoneMsg")
	}
	if !strings.Contains(m.outputPanel.View(), "Finish release done: rel-1") {
		t.Fatalf("output should contain success line, got %q", m.outputPanel.View())
	}

	reloadMsg := reloadCmd()
	loaded, ok := reloadMsg.(ReleasesLoadedMsg)
	if !ok {
		t.Fatalf("expected ReleasesLoadedMsg, got %T", reloadMsg)
	}
	updated, _ = m.Update(loaded)
	m = updated.(Model)

	if mgr.listReleasesCalls != 1 {
		t.Fatalf("ListReleases calls = %d, want 1", mgr.listReleasesCalls)
	}
	mgr.mu.Lock()
	finishCalls := mgr.finishReleaseCalls
	finishID := mgr.finishReleaseID
	mgr.mu.Unlock()
	if finishCalls != 1 {
		t.Fatalf("FinishRelease calls = %d, want 1", finishCalls)
	}
	if finishID != "rel-1" {
		t.Fatalf("FinishRelease releaseID = %q, want rel-1", finishID)
	}
}

func TestE2E_FinishReleaseFlow_NonPreparedSelection_FKeyIsNoOp(t *testing.T) {
	mgr := &mockManager{
		listReleasesResult: []domain.Release{{
			ID:        "rel-1",
			Status:    domain.ReleaseStatusReleased,
			CreatedAt: time.Now().UTC(),
		}},
	}

	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 140, 40)

	updated, _ := m.Update(ReleasesLoadedMsg{Releases: mgr.listReleasesResult})
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	m = updated.(Model)
	if m.focus != FocusReleases {
		t.Fatalf("focus = %v, want FocusReleases", m.focus)
	}

	updated, cmd := m.Update(sendKey("f"))
	m = updated.(Model)
	if cmd != nil {
		t.Fatalf("f should be no-op for released release, got %T", cmd())
	}
	if m.modal != nil {
		t.Fatalf("modal should be nil, got %T", m.modal)
	}
	if mgr.finishReleaseCalls != 0 {
		t.Fatalf("FinishRelease calls = %d, want 0", mgr.finishReleaseCalls)
	}
}
