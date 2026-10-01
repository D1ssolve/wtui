package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/domain"
)

func loadTaskWorkflow(t *testing.T, m Model, wf domain.WorkflowSummary) Model {
	t.Helper()
	m.tasksPanel.SetTasks([]domain.Task{{ID: "TASK-1"}})
	m.taskWorkflowGeneration = 1
	updated, _ := m.Update(TaskWorkflowLoadedMsg{TaskID: "TASK-1", Generation: 1, Workflow: wf})
	return updated.(Model)
}

func TestModelView_WorkflowStrip_RendersOnceBelowHeader(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	m = loadTaskWorkflow(t, m, domain.WorkflowSummary{
		Steps:      []domain.WorkflowStep{{Label: "code", State: "done"}, {Label: "merge", State: "now"}},
		NextAction: "merge now",
	})

	view := stripANSIForModel(m.View())
	if n := strings.Count(view, "ⓘ merge now"); n != 1 {
		t.Fatalf("workflow action must render exactly once, got %d: %q", n, view)
	}
	headerAt := strings.Index(view, "git worktree manager")
	stripAt := strings.Index(view, "ⓘ merge now")
	if headerAt < 0 || stripAt < headerAt {
		t.Fatalf("workflow strip must render below the header: %q", view)
	}
	if !strings.Contains(view, "✓ code") {
		t.Fatalf("workflow strip missing steps: %q", view)
	}
}

func TestModelView_WorkflowStrip_NoSelection_RendersNothing(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	view := stripANSIForModel(m.View())
	if strings.Contains(view, "ⓘ") {
		t.Fatalf("no selection must render no workflow strip: %q", view)
	}
}

func TestModelView_WorkflowStrip_ReleaseContext(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	updated, _ := m.Update(ReleasesLoadedMsg{Releases: []domain.Release{{
		ID: "REL-1", Version: "1.2.3", Status: domain.ReleaseStatusAwaitingMasterMerge,
	}}})
	m = updated.(Model)
	m.setFocus(FocusReleases)

	view := stripANSIForModel(m.View())
	if !strings.Contains(view, "REL-1") || !strings.Contains(view, "v1.2.3") {
		t.Fatalf("strip missing release identity/version: %q", view)
	}
	if n := strings.Count(view, "merge ready MRs in forge, then press M to reconcile"); n != 1 {
		t.Fatalf("release action must render exactly once, got %d: %q", n, view)
	}
}

func TestModelView_WorkflowStrip_ShowsSelectedServiceGuidance(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	m.servicesPanel.SetServices("TASK-1", []domain.Service{{Name: "api"}})
	m = loadTaskWorkflow(t, m, domain.WorkflowSummary{
		Steps:      []domain.WorkflowStep{{Label: "code", State: "done"}, {Label: "merge", State: "now"}},
		NextAction: "merge now",
		Services: []domain.ServiceWorkflow{
			{ServiceName: "api", Status: "blocked", Detail: "merge blocked: need rebase"},
			{ServiceName: "api", Status: "next", NextAction: "push hotfix branch"},
		},
	})

	view := stripANSIForModel(m.View())
	if !strings.Contains(view, "api: merge blocked: need rebase") {
		t.Fatalf("strip missing blocked guidance row: %q", view)
	}
	if !strings.Contains(view, "api: push hotfix branch") {
		t.Fatalf("strip missing duplicate guidance row: %q", view)
	}
}

func TestModelView_WorkflowStrip_SmallTerminalBounded(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 50, 10)
	m = loadTaskWorkflow(t, m, domain.WorkflowSummary{
		Steps:      []domain.WorkflowStep{{Label: "code", State: "done"}, {Label: "merge", State: "now"}},
		NextAction: "merge now",
	})

	view := m.View()
	for i, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > 50 {
			t.Fatalf("line %d width = %d, want <= 50: %q", i, w, stripANSIForModel(line))
		}
	}
}
