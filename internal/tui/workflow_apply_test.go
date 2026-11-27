package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	tea "github.com/charmbracelet/bubbletea"
)

func workflowApplyBuilder(t *testing.T) *modal.WorkflowBuilder {
	t.Helper()
	flow, err := gitflow.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	browser := modal.NewFlowOverlay(flow, modal.FlowPosition{})
	browser.SetTerminalSize(100, 24)
	browser.Update(tea.KeyMsg{Type: tea.KeyRight})
	next, _ := browser.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	return next.(*modal.WorkflowBuilder)
}

func workflowApplyScreen(m Model) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(stripANSIForModel(m.View()), "┃", " ")), " ")
}

func TestWorkflowApplyUnavailableKeepsDraft(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 100, 24)
	builder := workflowApplyBuilder(t)
	m.modal = builder
	next, cmd := m.update(modal.ReviewWorkflowApplyMsg{Builder: builder})
	if screen := workflowApplyScreen(next); cmd != nil || !strings.Contains(screen, "--config") {
		t.Fatalf("review without a write capability must explain how to configure it: %s", screen)
	}
	next, _ = next.update(tea.KeyMsg{Type: tea.KeyEsc})
	if next.modal != builder {
		t.Fatal("dismissing the error did not return to the original draft")
	}
}

func TestWorkflowApplyRequiresCurrentExplicitConfirmation(t *testing.T) {
	writes := 0
	m := newTestModelWithOptions(t, &mockManager{listReleasesResult: []domain.Release{
		{Status: domain.ReleaseStatus("custom_wait")},
		{Status: domain.ReleaseStatusDraft},
		{Status: domain.ReleaseStatusReleased},
		{Status: domain.ReleaseStatusFailed},
	}}, Options{WorkflowApply: WorkflowApplyTarget{
		Path: "/tmp/wtui/config.yaml",
		Apply: func(draft string) (bool, error) {
			if _, err := gitflow.ParseWorkflowYAML(draft); err != nil {
				t.Fatal(err)
			}
			writes++
			return true, nil
		},
	}})
	m = sendWindowSize(m, 100, 24)
	builder := workflowApplyBuilder(t)
	m.modal = builder
	if _, cmd := m.update(modal.ReviewWorkflowApplyMsg{Builder: workflowApplyBuilder(t)}); cmd != nil {
		t.Fatal("a stale builder started review")
	}
	m = reviewWorkflowForTest(t, m, builder)
	if !strings.Contains(m.View(), "2 in-flight") {
		t.Fatal("review did not present the active count from the manager")
	}
	stale := modal.NewWorkflowApplyConfirm(builder, "/tmp/wtui/config.yaml", 2)
	if _, cmd := m.update(modal.ConfirmWorkflowApplyMsg{Dialog: stale}); cmd != nil {
		t.Fatal("a different dialog could write")
	}
	if _, cmd := m.update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil || writes != 0 {
		t.Fatal("review or Enter could write without explicit confirmation")
	}
	m, cmd := m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd == nil {
		t.Fatal("explicit confirmation did not submit")
	}
	confirm := cmd()
	m, cmd = m.update(confirm)
	if cmd == nil {
		t.Fatal("the current confirmation did not schedule a write")
	}
	if _, duplicate := m.update(confirm); duplicate != nil {
		t.Fatal("duplicate confirmation scheduled another write")
	}
	m, quit := m.update(cmd())
	if writes != 1 || !m.WorkflowReloadRequested() || quit == nil {
		t.Fatal("one successful write must request a fresh session")
	}
	if _, ok := quit().(tea.QuitMsg); !ok {
		t.Fatal("successful apply did not stop the old program")
	}
}

func reviewWorkflowForTest(t *testing.T, m Model, builder *modal.WorkflowBuilder) Model {
	t.Helper()
	m, cmd := m.update(modal.ReviewWorkflowApplyMsg{Builder: builder})
	if cmd == nil {
		t.Fatal("review did not inspect releases")
	}
	m, _ = m.update(cmd())
	return m
}

func TestWorkflowApplyFailsClosedWhenReleasesCannotBeCounted(t *testing.T) {
	for _, manager := range []*mockManager{
		{listReleasesErr: errors.New("permission denied")},
		{listReleasesResult: []domain.Release{{
			ID: "broken", Status: domain.ReleaseStatusFailed,
			Error: &domain.ReleaseError{Code: "ERR_RELEASE_MANIFEST_INVALID"},
		}}},
	} {
		m := newTestModelWithOptions(t, manager, Options{WorkflowApply: WorkflowApplyTarget{
			Path: "/tmp/wtui/config.yaml",
			Apply: func(string) (bool, error) {
				t.Fatal("unreadable release data must prevent a write")
				return false, nil
			},
		}})
		m = sendWindowSize(m, 100, 24)
		builder := workflowApplyBuilder(t)
		m.modal = builder
		m = reviewWorkflowForTest(t, m, builder)
		if screen := workflowApplyScreen(m); !strings.Contains(screen, "Check release storage") {
			t.Fatalf("missing actionable release count error: %s", screen)
		}
		m, _ = m.update(tea.KeyMsg{Type: tea.KeyEsc})
		if m.modal != builder {
			t.Fatal("release inspection failure lost the draft")
		}
	}
}

func TestWorkflowApplyRechecksBusyStateAtConfirmation(t *testing.T) {
	for name, setBusy := range map[string]func(*Model){
		"operation":  func(m *Model) { m.opRunning = true },
		"cleanup":    func(m *Model) { m.releaseCleanupRequest = &releaseCleanupRequest{} },
		"conversion": func(m *Model) { m.tasks = []domain.Task{{PendingConversionTargetID: "next"}} },
	} {
		t.Run(name, func(t *testing.T) {
			m := newTestModelWithOptions(t, &mockManager{}, Options{WorkflowApply: WorkflowApplyTarget{
				Path:  "/tmp/wtui/config.yaml",
				Apply: func(string) (bool, error) { return true, nil },
			}})
			builder := workflowApplyBuilder(t)
			m.modal = builder
			m = reviewWorkflowForTest(t, m, builder)
			dialog := m.modal.(*modal.WorkflowApplyConfirm)
			setBusy(&m)
			m, cmd := m.update(modal.ConfirmWorkflowApplyMsg{Dialog: dialog})
			if cmd != nil {
				t.Fatal("started a write while busy")
			}
			m, _ = m.update(tea.KeyMsg{Type: tea.KeyEsc})
			if m.modal != builder {
				t.Fatal("busy confirmation must return to the draft, not leave a reusable approval")
			}
		})
	}
}

func TestWorkflowApplyKeepsDraftOnWriteOrReloadFailure(t *testing.T) {
	for _, tt := range []struct {
		name, visible string
		written       bool
	}{
		{name: "write failed", visible: "not written"},
		{name: "reload failed", visible: "could not reload", written: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := sendWindowSize(newTestModelWithOptions(t, &mockManager{}, Options{WorkflowApply: WorkflowApplyTarget{
				Path:  "/tmp/wtui/config.yaml",
				Apply: func(string) (bool, error) { return tt.written, errors.New("check fixture") },
			}}), 100, 24)
			builder := workflowApplyBuilder(t)
			m.modal = builder
			m = reviewWorkflowForTest(t, m, builder)
			dialog := m.modal.(*modal.WorkflowApplyConfirm)
			m, cmd := m.update(modal.ConfirmWorkflowApplyMsg{Dialog: dialog})
			m, quit := m.update(cmd())
			if screen := workflowApplyScreen(m); quit != nil || m.WorkflowReloadRequested() || !strings.Contains(screen, tt.visible) {
				t.Fatalf("apply failure did not remain visibly distinct from a successful reload: %s", screen)
			}
			m, _ = m.update(tea.KeyMsg{Type: tea.KeyEsc})
			if m.modal != builder {
				t.Fatal("apply failure lost the original draft")
			}
			if _, cmd := m.update(modal.ConfirmWorkflowApplyMsg{Dialog: dialog}); cmd != nil {
				t.Fatal("failed apply left a reusable confirmation")
			}
		})
	}
}
