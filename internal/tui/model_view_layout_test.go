package tui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/domain"
)

func assertModelViewBounds(t *testing.T, m Model, width, height int) {
	t.Helper()
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != height {
		t.Fatalf("view height = %d lines, want %d:\n%s", len(lines), height, stripANSIForModel(view))
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("line %d width = %d, want <= %d: %q", i, w, width, stripANSIForModel(line))
		}
	}
}

func testWorkflowSummary() domain.WorkflowSummary {
	return domain.WorkflowSummary{
		Steps:      []domain.WorkflowStep{{Label: "code", State: "done"}, {Label: "merge", State: "now"}},
		NextAction: "merge now",
	}
}

func loadTasksForView(t *testing.T, m Model) Model {
	t.Helper()
	updated, cmd := m.Update(TasksLoadedMsg{Tasks: []domain.Task{{ID: "TASK-1"}}})
	m = updated.(Model)
	if cmd != nil {
		runBatchCommands(cmd())
	}
	return m
}

func TestModelView_BoundsAfterTaskWorkflowArrives(t *testing.T) {
	for _, size := range []struct{ w, h int }{{120, 40}, {90, 30}, {50, 24}} {
		t.Run(sizeLabel(size.w, size.h), func(t *testing.T) {
			m := sendWindowSize(newTestModel(t, &mockManager{}), size.w, size.h)
			assertModelViewBounds(t, m, size.w, size.h)

			m = loadTasksForView(t, m)
			assertModelViewBounds(t, m, size.w, size.h)

			updated, _ := m.Update(TaskWorkflowLoadedMsg{TaskID: "TASK-1", Generation: m.taskWorkflowGeneration, Workflow: testWorkflowSummary()})
			m = updated.(Model)
			assertModelViewBounds(t, m, size.w, size.h)
			if !strings.Contains(stripANSIForModel(m.View()), "ⓘ merge now") {
				t.Fatal("workflow strip missing after load")
			}
		})
	}
}

func TestModelView_BoundsShortTerminalWithWorkflow(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 80, 10)
	m = loadTasksForView(t, m)
	assertModelViewBounds(t, m, 80, 10)

	updated, _ := m.Update(TaskWorkflowLoadedMsg{TaskID: "TASK-1", Generation: m.taskWorkflowGeneration, Workflow: testWorkflowSummary()})
	m = updated.(Model)
	assertModelViewBounds(t, m, 80, 10)
}

func TestModelView_SelectedServiceGuidanceWithinBounds(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 100, 30)
	m = loadTasksForView(t, m)

	updated, _ := m.Update(ServicesLoadedMsg{TaskID: "TASK-1", Generation: m.taskWorkflowGeneration, Services: []domain.Service{{Name: "api"}}})
	m = updated.(Model)
	assertModelViewBounds(t, m, 100, 30)

	workflow := testWorkflowSummary()
	workflow.Services = []domain.ServiceWorkflow{{ServiceName: "api", Status: "blocked", Detail: "merge blocked: need rebase"}}
	updated, _ = m.Update(TaskWorkflowLoadedMsg{TaskID: "TASK-1", Generation: m.taskWorkflowGeneration, Workflow: workflow})
	m = updated.(Model)
	assertModelViewBounds(t, m, 100, 30)

	view := stripANSIForModel(m.View())
	if n := strings.Count(view, "api: merge blocked: need rebase"); n != 1 {
		t.Fatalf("selected-service guidance must render once, got %d: %q", n, view)
	}
}

func TestModelView_ReleaseWorkflowWithinBounds(t *testing.T) {
	for _, size := range []struct{ w, h int }{{120, 40}, {50, 24}} {
		t.Run(sizeLabel(size.w, size.h), func(t *testing.T) {
			m := sendWindowSize(newTestModel(t, &mockManager{}), size.w, size.h)
			updated, cmd := m.Update(ReleasesLoadedMsg{Releases: []domain.Release{{
				ID: "REL-1", Version: "1.2.3", Status: domain.ReleaseStatusAwaitingMasterMerge,
			}}})
			m = updated.(Model)
			if cmd != nil {
				runBatchCommands(cmd())
			}
			assertModelViewBounds(t, m, size.w, size.h)

			m.setFocus(FocusReleases)
			assertModelViewBounds(t, m, size.w, size.h)
			if !strings.Contains(stripANSIForModel(m.View()), "REL-1") {
				t.Fatal("release workflow strip missing")
			}
		})
	}
}

func sizeLabel(w, h int) string {
	return "view-" + strconv.Itoa(w) + "x" + strconv.Itoa(h)
}

func assertOrderedInView(t *testing.T, view string, markers ...string) {
	t.Helper()
	prev := -1
	for _, marker := range markers {
		idx := strings.Index(view, marker)
		if idx < 0 {
			t.Fatalf("marker %q missing:\n%s", marker, view)
		}
		if idx <= prev {
			t.Fatalf("marker %q out of order:\n%s", marker, view)
		}
		prev = idx
	}
}

func TestModelView_Narrow60x24RetainsOutputMarkerAndFooter(t *testing.T) {
	t.Run("task", func(t *testing.T) {
		m := sendWindowSize(newTestModel(t, &mockManager{}), 60, 24)
		m = loadTasksForView(t, m)
		m.outputPanel.AppendLine("OUTPUTMARKER cleanup failed")
		assertModelViewBounds(t, m, 60, 24)
		view := stripANSIForModel(m.View())
		assertOrderedInView(t, view, "TASKS", "OUTPUT", "[q] quit")
		if !strings.Contains(view, "OUTPUTMARKER") {
			t.Fatalf("output marker cropped:\n%s", view)
		}
	})

	t.Run("release-stacked", func(t *testing.T) {
		m := sendWindowSize(newTestModel(t, &mockManager{}), 60, 24)
		updated, cmd := m.Update(ReleasesLoadedMsg{Releases: []domain.Release{{
			ID: "REL-1", Version: "1.0.0", Status: domain.ReleaseStatusAwaitingMasterMerge,
		}}})
		m = updated.(Model)
		if cmd != nil {
			runBatchCommands(cmd())
		}
		m.setFocus(FocusReleases)
		m.outputPanel.AppendLine("OUTPUTMARKER cleanup failed")
		assertModelViewBounds(t, m, 60, 24)
		view := stripANSIForModel(m.View())
		assertOrderedInView(t, view, "TASKS", "RELEASES", "OUTPUT", "[q] quit")
		if !strings.Contains(view, "OUTPUTMARKER") {
			t.Fatalf("output marker cropped:\n%s", view)
		}
	})
}

func TestModelView_Compact50x10Bounded(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 50, 10)
	m = loadTasksForView(t, m)
	assertModelViewBounds(t, m, 50, 10)

	updated, _ := m.Update(TaskWorkflowLoadedMsg{TaskID: "TASK-1", Generation: m.taskWorkflowGeneration, Workflow: testWorkflowSummary()})
	m = updated.(Model)
	assertModelViewBounds(t, m, 50, 10)
}
