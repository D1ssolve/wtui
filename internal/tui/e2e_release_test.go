package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
)

func TestE2E_RightPaneModeIsRememberedByTab(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 140, 40)

	updated, _ := m.Update(sendKey("3"))
	m = updated.(Model)
	updated, _ = m.Update(sendKey("1"))
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.focus != FocusReleases {
		t.Fatalf("Tab from tasks = %v, want remembered releases pane", m.focus)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.focus != FocusOutput {
		t.Fatalf("Tab from right pane = %v, want output", m.focus)
	}
}

func TestE2E_TaskMergeInspectionOpensConfirmAndConfirmStartsMerge(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 140, 40)
	m.tasksPanel.SetTasks([]domain.Task{{ID: "TASK-1"}})

	updated, cmd := m.Update(sendKey("M"))
	m = updated.(Model)
	if cmd == nil || !m.opRunning {
		t.Fatal("M must start task MR inspection")
	}

	inspection := task.TaskMergeInspection{TaskID: "TASK-1", Services: []task.ServiceMergeInspection{
		{ServiceName: "api", Status: "ready"},
		{ServiceName: "worker", Status: "blocked", Blockers: []string{"CI failed"}},
	}}
	updated, _ = m.Update(TaskMergeInspectionMsg{TaskID: "TASK-1", Generation: m.mergeInspectionGeneration, Inspection: inspection})
	m = updated.(Model)
	dialog, ok := m.modal.(*modal.MergeConfirmDialog)
	if !ok || !strings.Contains(dialog.View(), "worker") {
		t.Fatalf("inspection modal = %T, want merge details", m.modal)
	}

	updated, cmd = m.Update(modal.ConfirmMergeMsg{TaskID: "TASK-1"})
	m = updated.(Model)
	if cmd == nil || !m.opRunning || m.modal != nil {
		t.Fatal("merge confirmation must start merge and close modal")
	}
}

func TestE2E_ReleaseActionsAreStatusAware(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 140, 40)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusPrepared}})

	updated, cmd := m.Update(sendKey("F"))
	m = updated.(Model)
	if cmd == nil || !m.opRunning || !strings.Contains(m.outputPanel.View(), "Promoting release rel-1") {
		t.Fatal("F on prepared release must start promote")
	}

	m.opRunning = false
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-2", Status: domain.ReleaseStatusReleased}})
	updated, cmd = m.Update(sendKey("F"))
	m = updated.(Model)
	if cmd != nil || m.opRunning || !strings.Contains(m.outputPanel.View(), "unavailable for status released") {
		t.Fatal("F on released release must emit hint only")
	}
}
