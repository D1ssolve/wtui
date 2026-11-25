package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/logutil"
	"github.com/D1ssolve/wtui/internal/tui/panels"
	tea "github.com/charmbracelet/bubbletea"
)

type stalledValidationManager struct {
	mockManager
	started chan context.Context
	stop    context.Context
}

func (m *stalledValidationManager) ValidateTask(ctx context.Context, _ string) (domain.TaskValidation, error) {
	m.started <- ctx
	select {
	case <-ctx.Done():
		return domain.TaskValidation{}, fmt.Errorf("status: %w", ctx.Err())
	case <-m.stop.Done():
		return domain.TaskValidation{}, m.stop.Err()
	}
}

func TestStandaloneValidationEscCancelsSelectedRead(t *testing.T) {
	stop, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	mgr := &stalledValidationManager{started: make(chan context.Context, 1), stop: stop}
	m := sendWindowSize(newTestModel(t, mgr), 140, 40)
	m.tasksPanel.SetTasks([]domain.Task{{ID: "T-14"}})
	model, keyCmd := m.Update(sendKey("V"))
	start, ok := keyCmd().(panels.ValidateTaskMsg)
	if !ok || start.TaskID != "T-14" {
		t.Fatalf("V did not select T-14: %+v", start)
	}
	model, cmd := model.Update(start)
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd().(tea.BatchMsg)[0]() }()
	var managerCtx context.Context
	select {
	case managerCtx = <-mgr.started:
	case <-stop.Done():
		t.Fatal("validation did not start")
	}
	deadline, ok := managerCtx.Deadline()
	if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 30*time.Second {
		t.Fatalf("validation deadline = %v, want the existing 30s cap", deadline)
	}
	if id := logutil.TaskIDFromContext(managerCtx); id != "T-14" {
		t.Fatalf("log task ID = %q, want T-14", id)
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !strings.Contains(model.View(), "[y/N]") {
		t.Fatal("standalone validation did not offer Esc confirmation")
	}
	model, _ = model.Update(ValidationResultMsg{Generation: 0, Validation: domain.TaskValidation{TaskID: "old"}})
	if !strings.Contains(model.View(), "[y/N]") {
		t.Fatal("stale validation result dismissed the current confirmation")
	}
	model, _ = model.Update(sendKey("n"))
	if managerCtx.Err() != nil || strings.Contains(model.View(), "[y/N]") {
		t.Fatal("declining must dismiss confirmation without interrupting the read")
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model, _ = model.Update(sendKey("y"))
	select {
	case msg := <-done:
		model, _ = model.Update(msg)
	case <-stop.Done():
		t.Fatal("Esc confirmation did not reach the real validation context")
	}
	got := model.(Model)
	output := got.outputPanel.View()
	if managerCtx.Err() != context.Canceled || !strings.Contains(output, "cancelled for task T-14") ||
		strings.Contains(output, "All services clean.") || got.opRunning || got.modal != nil {
		t.Fatalf("cancelled validation left a result or blocked the UI: %s", output)
	}
}
