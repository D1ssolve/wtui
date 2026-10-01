package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/panels"
)

func loadWorkflowForSelection(t *testing.T, m Model, taskID, nextAction string) Model {
	t.Helper()
	updated, _ := m.Update(TaskWorkflowLoadedMsg{
		TaskID:     taskID,
		Generation: m.taskWorkflowGeneration,
		Workflow: domain.WorkflowSummary{
			Steps:      []domain.WorkflowStep{{Label: "merge", State: "now"}},
			NextAction: nextAction,
		},
	})
	return updated.(Model)
}

func TestUpdate_TaskSelectionChangedMsg_DelayedSelectionDoesNotResetWorkflow(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	m.tasksPanel.SetTasks([]domain.Task{{ID: "TASK-A"}, {ID: "TASK-B"}})

	m.taskWorkflowGeneration = 1
	m = loadWorkflowForSelection(t, m, "TASK-A", "action A")

	updated, _ := m.Update(sendKey("j"))
	m = updated.(Model)
	updated, _ = m.Update(panels.TaskSelectionChangedMsg{TaskID: "TASK-B"})
	m = updated.(Model)
	m = loadWorkflowForSelection(t, m, "TASK-B", "action B")

	if !strings.Contains(stripANSIForModel(m.View()), "action B") {
		t.Fatalf("precondition: task B workflow shown, got %q", stripANSIForModel(m.View()))
	}

	updated, cmd := m.Update(panels.TaskSelectionChangedMsg{TaskID: "TASK-A"})
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("delayed selection message must not trigger a reload command")
	}
	if m.taskWorkflowPending {
		t.Fatal("delayed selection message must not mark the workflow pending")
	}
	if !strings.Contains(stripANSIForModel(m.View()), "action B") {
		t.Fatalf("delayed TaskSelectionChangedMsg reset the current workflow: %q", stripANSIForModel(m.View()))
	}
}

func TestUpdate_TaskSelectionChangedMsg_DelayedMidSelectionDoesNotResetWorkflow(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	m.tasksPanel.SetTasks([]domain.Task{{ID: "TASK-A"}, {ID: "TASK-B"}})

	m.taskWorkflowGeneration = 1
	m = loadWorkflowForSelection(t, m, "TASK-A", "action A")

	updated, _ := m.Update(sendKey("j"))
	m = updated.(Model)
	updated, _ = m.Update(panels.TaskSelectionChangedMsg{TaskID: "TASK-B"})
	m = updated.(Model)
	updated, _ = m.Update(sendKey("k"))
	m = updated.(Model)
	updated, _ = m.Update(panels.TaskSelectionChangedMsg{TaskID: "TASK-A"})
	m = updated.(Model)
	m = loadWorkflowForSelection(t, m, "TASK-A", "action A again")

	if !strings.Contains(stripANSIForModel(m.View()), "action A again") {
		t.Fatalf("precondition: task A workflow shown, got %q", stripANSIForModel(m.View()))
	}

	updated, cmd := m.Update(panels.TaskSelectionChangedMsg{TaskID: "TASK-B"})
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("delayed mid-selection message must not trigger a reload command")
	}
	if !strings.Contains(stripANSIForModel(m.View()), "action A again") {
		t.Fatalf("delayed mid-selection message reset the current workflow: %q", stripANSIForModel(m.View()))
	}
}

func TestUpdate_ReleaseMergeDoneMsg_ReportsResultWithoutDoneLabel(t *testing.T) {
	release := domain.Release{ID: "REL-1", Status: domain.ReleaseStatusAwaitingMasterMerge}
	tests := []struct {
		name     string
		result   task.ReleaseMergeResult
		err      error
		contains []string
		notDone  bool
	}{
		{
			name:     "complete success is done",
			result:   task.ReleaseMergeResult{ReleaseID: "REL-1", Merged: []string{"api", "worker"}},
			contains: []string{"Merge release MRs done: REL-1"},
		},
		{
			name:     "failed service is not done",
			result:   task.ReleaseMergeResult{ReleaseID: "REL-1", Merged: []string{"api"}, Failed: []string{"worker"}},
			contains: []string{"worker"},
			notDone:  true,
		},
		{
			name:     "skipped service is not done",
			result:   task.ReleaseMergeResult{ReleaseID: "REL-1", Merged: []string{"api"}, Skipped: []string{"worker"}},
			contains: []string{"worker"},
			notDone:  true,
		},
		{
			name:     "mixed failed and skipped names both reported",
			result:   task.ReleaseMergeResult{ReleaseID: "REL-1", Failed: []string{"api"}, Skipped: []string{"worker"}},
			contains: []string{"api", "worker"},
			notDone:  true,
		},
		{
			name:     "operation error is failure",
			result:   task.ReleaseMergeResult{ReleaseID: "REL-1"},
			err:      errors.New("boom"),
			contains: []string{"Merge release MRs failed: boom"},
			notDone:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
			m.opRunning = true

			updated, _ := m.Update(ReleaseMergeDoneMsg{Release: release, Result: tt.result, Err: tt.err})
			m = updated.(Model)

			out := m.outputPanel.View()
			for _, want := range tt.contains {
				if !strings.Contains(out, want) {
					t.Fatalf("output = %q, want to contain %q", out, want)
				}
			}
			if tt.notDone && strings.Contains(out, "Merge release MRs done:") {
				t.Fatalf("output = %q, must not be labeled done", out)
			}
			if m.opRunning {
				t.Fatal("opRunning must be cleared when the merge result arrives")
			}
		})
	}
}

func TestModelView_WorkflowStrip_ReleaseIgnoresHiddenTaskService(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	m.tasksPanel.SetTasks([]domain.Task{{ID: "TASK-1"}})
	m.servicesPanel.SetServices("TASK-1", []domain.Service{{Name: "api"}})

	updated, _ := m.Update(ReleasesLoadedMsg{Releases: []domain.Release{{
		ID:      "REL-1",
		Version: "1.2.3",
		Status:  domain.ReleaseStatusAwaitingMasterMerge,
		Services: []domain.ReleaseService{
			{Name: "api", Status: domain.ReleaseStatusAwaitingMasterMerge},
		},
	}}})
	m = updated.(Model)
	m.setFocus(FocusReleases)

	view := stripANSIForModel(m.View())
	if !strings.Contains(view, "REL-1") {
		t.Fatalf("precondition: release strip shown, got %q", view)
	}
	if strings.Contains(view, "api:") {
		t.Fatalf("release strip must not reuse hidden task service selection: %q", view)
	}
}
