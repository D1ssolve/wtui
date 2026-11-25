package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	"github.com/D1ssolve/wtui/internal/tui/panels"
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
	if cmd == nil || !m.opRunning || !strings.Contains(m.outputPanel.View(), "Release rel-1: advance") {
		t.Fatal("F on prepared release must fire the advance event")
	}

	m.opRunning = false
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-2", Status: domain.ReleaseStatusReleased}})
	updated, cmd = m.Update(sendKey("F"))
	m = updated.(Model)
	if cmd != nil || m.opRunning || !strings.Contains(m.outputPanel.View(), "unavailable for status released") {
		t.Fatal("F on released release must emit hint only")
	}
}

func TestE2E_ReleaseRetryRequiresRecoverableFailure(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 140, 40)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{
		ID:     "rel-1",
		Status: domain.ReleaseStatusFailed,
		Error:  &domain.ReleaseError{Recoverable: true},
	}})

	updated, cmd := m.Update(keyMsgFor("ctrl+r"))
	m = updated.(Model)
	if cmd == nil || !m.opRunning || !strings.Contains(m.outputPanel.View(), "Retrying release rel-1") {
		t.Fatal("ctrl+r on recoverable failed release must start retry")
	}

	m.opRunning = false
	m.releasesPanel.SetReleases([]domain.Release{{
		ID:     "rel-2",
		Status: domain.ReleaseStatusFailed,
		Error:  &domain.ReleaseError{Recoverable: false},
	}})
	updated, cmd = m.Update(keyMsgFor("ctrl+r"))
	m = updated.(Model)
	if cmd != nil || m.opRunning || !strings.Contains(m.outputPanel.View(), "release must be failed and recoverable") {
		t.Fatal("ctrl+r on non-recoverable failed release must emit hint only")
	}

	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-3", Status: domain.ReleaseStatusPrepared}})
	updated, cmd = m.Update(keyMsgFor("ctrl+r"))
	m = updated.(Model)
	if cmd != nil || m.opRunning {
		t.Fatal("ctrl+r on non-failed release must not start retry")
	}
}

func TestE2E_CustomReleaseEventKeyFiresEvent(t *testing.T) {
	mgr := &mockManager{releaseEvents: []task.ReleaseEvent{{Name: "approve", Key: "A", Label: "approve"}, {Name: "reject", Key: "X", Label: "send back to fixing"}}}
	m := sendWindowSize(newTestModel(t, mgr), 140, 40)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: "qualification"}})

	if !hasHint(footerHints(m), "X", "send back to fixing") {
		t.Fatalf("custom workflow event is missing from the footer: %+v", footerHints(m))
	}
	updated, cmd := m.Update(sendKey("X"))
	m = updated.(Model)
	if cmd == nil || !m.opRunning || !strings.Contains(m.outputPanel.View(), "Release rel-1: reject") {
		t.Fatal("X must fire the reject event")
	}
}

func TestE2E_WOpensWorkflowDiagramAtSelectedRelease(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 140, 60)
	flow, err := gitflow.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	m.flow = flow
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusPrepared}})

	updated, _ := m.Update(sendKey("W"))
	m = updated.(Model)
	overlay, ok := m.modal.(*modal.FlowOverlay)
	if !ok || !strings.Contains(overlay.View(), "▶ prepared") {
		t.Fatalf("modal = %T, want the workflow diagram with the prepared state marked", m.modal)
	}
}

type fakeAssistant struct {
	ai.Assistant
	got ai.FailureRequest
}

func (f *fakeAssistant) Explain(_ context.Context, r ai.FailureRequest) (ai.Explanation, error) {
	f.got = r
	return ai.Explanation{Cause: "Tag v1 exists at another commit.", Steps: []string{"Inspect the tag", "Press ctrl+r"}}, nil
}

func TestE2E_EExplainsSelectedFailedRelease(t *testing.T) {
	assistant := &fakeAssistant{}
	m := sendWindowSize(newTestModelWithOptions(t, &mockManager{}, Options{Assistant: assistant}), 140, 40)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusFailed, Error: &domain.ReleaseError{Message: "tag push failed", Recoverable: true}}})

	updated, cmd := m.Update(sendKey("E"))
	m = updated.(Model)
	if cmd == nil || !m.explaining {
		t.Fatal("E must ask the assistant")
	}
	msg := explainFailureCmd(assistant, "release rel-1", func() ai.FailureRequest { _, r := m.failureRequest(); return r }())()
	updated, _ = m.Update(msg)
	m = updated.(Model)
	if assistant.got.Error != "tag push failed" || !strings.Contains(assistant.got.Operation, "rel-1") {
		t.Fatalf("request = %+v", assistant.got)
	}
	explanation, ok := m.modal.(*modal.ExplanationModal)
	if !ok || !strings.Contains(explanation.View(), "Press ctrl+r") || m.explaining {
		t.Fatalf("modal = %T, want the explanation", m.modal)
	}
}

func TestE2E_GatedTaskKeysFollowItsWorkflowMachine(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 140, 40)
	m.tasksPanel.SetTasks([]domain.Task{{ID: "T-1"}})
	m.taskWorkflow = &domain.WorkflowSummary{Gated: true, Current: "qa", NextAction: "press A to accept", Events: []domain.WorkflowEvent{{Name: "accept", Key: "A", Label: "accept"}}}

	updated, cmd := m.Update(panels.PlanCloseTaskMsg{TaskID: "T-1"})
	m = updated.(Model)
	if cmd != nil || m.opRunning || !strings.Contains(m.outputPanel.View(), "workflow state qa; press A to accept") {
		t.Fatal("C outside a close state must explain the workflow instead of planning a close")
	}
	if !hasHint(footerHints(m), "A", "accept") {
		t.Fatalf("footer = %+v, want the task event", footerHints(m))
	}
	updated, cmd = m.Update(sendKey("A"))
	m = updated.(Model)
	if cmd == nil || !m.opRunning || !strings.Contains(m.outputPanel.View(), "Task T-1: accept") {
		t.Fatal("A must fire the accept event")
	}
}
