package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	"github.com/D1ssolve/wtui/internal/tui/panels"
)

func helpView(t *testing.T, m Model) string {
	t.Helper()
	help, ok := m.modal.(*modal.HelpOverlay)
	if !ok {
		t.Fatalf("expected *modal.HelpOverlay, got %T", m.modal)
	}
	return help.View()
}

func openHelp(t *testing.T, m Model) Model {
	t.Helper()
	updated, _ := m.Update(sendKey("?"))
	m = updated.(Model)
	if _, ok := m.modal.(*modal.HelpOverlay); !ok {
		t.Fatalf("'?' must open help, got %T", m.modal)
	}
	return m
}

func TestHelpModal_QuitKeys_QuitApplication(t *testing.T) {
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'q'}},
		{Type: tea.KeyCtrlC},
	} {
		m := openHelp(t, sendWindowSize(newTestModel(t, &mockManager{}), 120, 40))
		updated, cmd := m.Update(key)
		m = updated.(Model)
		if cmd == nil {
			t.Fatalf("%q with help open must return a cmd", key.String())
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%q with help open must quit, got %T", key.String(), cmd())
		}
		if m.modal == nil {
			t.Fatalf("%q must quit without only closing help", key.String())
		}
	}
}

func TestHelpModal_TasksFocus_ShowsSelectedTaskWorkflow(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	m.tasksPanel.SetTasks([]domain.Task{{ID: "TASK-1"}})
	m.taskWorkflowGeneration = 1
	updated, _ := m.Update(TaskWorkflowLoadedMsg{TaskID: "TASK-1", Generation: 1, Workflow: domain.WorkflowSummary{
		Steps:      []domain.WorkflowStep{{Label: "code", State: "done"}, {Label: "merge", State: "now"}},
		NextAction: "merge now",
	}})
	m = updated.(Model)

	m = openHelp(t, m)
	view := helpView(t, m)
	if !strings.Contains(view, "Task TASK-1") || !strings.Contains(view, "merge now") {
		t.Fatalf("help must show selected task workflow, got %q", view)
	}
}

func TestHelpModal_ReleasesFocus_ShowsSelectedReleaseWorkflow(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	updated, _ := m.Update(ReleasesLoadedMsg{Releases: []domain.Release{{ID: "REL-1", Status: domain.ReleaseStatusAwaitingMasterMerge}}})
	m = updated.(Model)
	m.setFocus(FocusReleases)

	m = openHelp(t, m)
	view := helpView(t, m)
	if !strings.Contains(view, "Release REL-1") || !strings.Contains(view, "merge ready MRs in forge, then press M to reconcile") {
		t.Fatalf("help must show selected release workflow, got %q", view)
	}
}

func TestHelpModal_OutputFocus_ShowsNoWorkflowContext(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	m.tasksPanel.SetTasks([]domain.Task{{ID: "TASK-1"}})
	m.taskWorkflowGeneration = 1
	updated, _ := m.Update(TaskWorkflowLoadedMsg{TaskID: "TASK-1", Generation: 1, Workflow: domain.WorkflowSummary{
		Steps:      []domain.WorkflowStep{{Label: "merge", State: "now"}},
		NextAction: "merge now",
	}})
	m = updated.(Model)
	m.setFocus(FocusOutput)

	m = openHelp(t, m)
	view := helpView(t, m)
	if strings.Contains(view, "merge now") || strings.Contains(view, "Task TASK-1") {
		t.Fatalf("output focus help must stay generic, got %q", view)
	}
}

func TestHelpModal_OpenHelp_UpdatesOnValidWorkflowMessage(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	m.tasksPanel.SetTasks([]domain.Task{{ID: "TASK-1"}})
	m.taskWorkflowGeneration = 1
	m = openHelp(t, m)
	if strings.Contains(helpView(t, m), "merge now") {
		t.Fatal("help must not show workflow before it loads")
	}

	updated, _ := m.Update(TaskWorkflowLoadedMsg{TaskID: "TASK-1", Generation: 1, Workflow: domain.WorkflowSummary{
		Steps:      []domain.WorkflowStep{{Label: "merge", State: "now"}},
		NextAction: "merge now",
	}})
	m = updated.(Model)
	if !strings.Contains(helpView(t, m), "merge now") {
		t.Fatalf("open help must update on valid workflow message, got %q", helpView(t, m))
	}
}

func TestHelpModal_OpenHelp_IgnoresStaleWorkflowMessage(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	m.tasksPanel.SetTasks([]domain.Task{{ID: "TASK-1"}})
	m.taskWorkflowGeneration = 2
	m = openHelp(t, m)

	updated, _ := m.Update(TaskWorkflowLoadedMsg{TaskID: "TASK-1", Generation: 1, Workflow: domain.WorkflowSummary{
		Steps:      []domain.WorkflowStep{{Label: "merge", State: "now"}},
		NextAction: "stale action",
	}})
	m = updated.(Model)
	if strings.Contains(helpView(t, m), "stale action") {
		t.Fatalf("stale workflow must not reach open help, got %q", helpView(t, m))
	}
}

func TestHelpModal_TaskSelectionChange_ClearsWorkflowContext(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	m.tasksPanel.SetTasks([]domain.Task{{ID: "TASK-1"}, {ID: "TASK-2"}})
	m.taskWorkflowGeneration = 1
	updated, _ := m.Update(TaskWorkflowLoadedMsg{TaskID: "TASK-1", Generation: 1, Workflow: domain.WorkflowSummary{
		Steps:      []domain.WorkflowStep{{Label: "merge", State: "now"}},
		NextAction: "merge now",
	}})
	m = updated.(Model)

	updated, _ = m.Update(sendKey("j"))
	m = updated.(Model)
	m = openHelp(t, m)
	if !strings.Contains(helpView(t, m), "merge now") {
		t.Fatal("precondition: help shows workflow")
	}

	updated, _ = m.Update(panels.TaskSelectionChangedMsg{TaskID: "TASK-2"})
	m = updated.(Model)
	if strings.Contains(helpView(t, m), "merge now") {
		t.Fatalf("selection change must clear help workflow context, got %q", helpView(t, m))
	}
}

func TestHelpModal_TasksLoadedWithNoSelection_ClearsWorkflowContext(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	m.tasksPanel.SetTasks([]domain.Task{{ID: "TASK-1"}})
	m.taskWorkflowGeneration = 1
	updated, _ := m.Update(TaskWorkflowLoadedMsg{TaskID: "TASK-1", Generation: 1, Workflow: domain.WorkflowSummary{
		Steps:      []domain.WorkflowStep{{Label: "merge", State: "now"}},
		NextAction: "merge now",
	}})
	m = updated.(Model)
	m = openHelp(t, m)
	if !strings.Contains(helpView(t, m), "merge now") {
		t.Fatal("precondition: help shows workflow")
	}

	updated, _ = m.Update(TasksLoadedMsg{})
	m = updated.(Model)
	if strings.Contains(helpView(t, m), "merge now") {
		t.Fatalf("refresh with no selected task must clear help workflow context, got %q", helpView(t, m))
	}
}
