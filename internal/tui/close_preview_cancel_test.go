package tui

import (
	"context"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	"github.com/D1ssolve/wtui/internal/tui/panels"
	tea "github.com/charmbracelet/bubbletea"
)

type blockedClosePreviewManager struct {
	closeTaskE2EManager
	started chan context.Context
	stop    context.Context
}

func (m *blockedClosePreviewManager) PlanCloseTask(ctx context.Context, _ string) (task.ClosePlan, error) {
	m.started <- ctx
	select {
	case <-ctx.Done():
		// Like git status in production, the planner may return a domain
		// failure instead of preserving context.Canceled.
		return task.ClosePlan{}, task.ErrValidationFailed
	case <-m.stop.Done():
		return task.ClosePlan{}, m.stop.Err()
	}
}

func TestClosePreviewEscapeCancelsPendingRead(t *testing.T) {
	stop, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	mgr := &blockedClosePreviewManager{started: make(chan context.Context, 1), stop: stop}
	m := sendWindowSize(newTestModel(t, mgr), 140, 40)
	model, _ := m.Update(TasksLoadedMsg{Tasks: []domain.Task{{ID: "T-1"}}})
	selection := model.(Model).taskWorkflowGeneration
	model, cmd := model.Update(panels.PlanCloseTaskMsg{TaskID: "T-1"})

	done := make(chan tea.Msg, 1)
	go func() { done <- cmd().(tea.BatchMsg)[0]() }()
	var readCtx context.Context
	select {
	case readCtx = <-mgr.started:
	case <-stop.Done():
		t.Fatal("close preview did not start")
	}

	esc := tea.KeyMsg{Type: tea.KeyEsc}
	model, _ = model.Update(esc)
	if model.(Model).operationCancelHint() == "" {
		t.Fatal("Esc did not offer to cancel the pending close preview")
	}
	model, _ = model.Update(sendKey("n"))
	if readCtx.Err() != nil || model.(Model).operationCancelHint() != "" {
		t.Fatal("declining did not dismiss the prompt while keeping the same read pending")
	}
	model, _ = model.Update(esc)
	model, _ = model.Update(sendKey("y"))
	select {
	case msg := <-done:
		model, _ = model.Update(msg)
	case <-stop.Done():
		t.Fatal("Esc/y did not release the close preview read")
	}

	got := model.(Model)
	if readCtx.Err() != context.Canceled || got.opRunning || got.modal != nil ||
		got.operationCancelHint() != "" || got.pendingCloseTask != nil || mgr.closeCalls != 0 {
		t.Fatalf("cancelled preview left a result, active status, or writes: read=%v busy=%v modal=%T hint=%q pending=%v writes=%d",
			readCtx.Err(), got.opRunning, got.modal, got.operationCancelHint(), got.pendingCloseTask, mgr.closeCalls)
	}
	model, _ = model.Update(TaskWorkflowLoadedMsg{TaskID: "T-1", Generation: selection})
	if model.(Model).taskWorkflowPending {
		t.Fatal("cancelling the preview invalidated the current task selection's pending workflow load")
	}
}

func TestClosePreviewDiscardsQueuedSuccessAfterCancellation(t *testing.T) {
	mgr := &closeTaskE2EManager{plan: task.ClosePlan{TaskID: "T-1"}}
	m := sendWindowSize(newTestModel(t, mgr), 140, 40)
	start := panels.PlanCloseTaskMsg{TaskID: "T-1"}
	model, cmd := m.Update(start)
	queued, ok := extractClosePlanReadyFromBatch(cmd)
	if !ok || queued.Err != nil {
		t.Fatalf("expected a successful plan already queued, got %+v", queued)
	}

	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model, _ = model.Update(sendKey("y"))
	model, _ = model.Update(queued)
	if got := model.(Model).modal; got != nil {
		t.Fatalf("confirmed cancellation let a queued success open %T", got)
	}

	model, retry := model.Update(start)
	model, _ = model.Update(queued)
	if got := model.(Model).modal; got != nil {
		t.Fatalf("old preview opened %T during retry", got)
	}
	ready, ok := extractClosePlanReadyFromBatch(retry)
	if !ok {
		t.Fatal("cancellation did not leave close preview retryable")
	}
	model, _ = model.Update(ready)
	if got := model.(Model).modal; got == nil {
		t.Fatal("retry did not open a confirmation")
	} else if _, ok := got.(*modal.CloseTaskConfirmModal); !ok {
		t.Fatalf("retry opened %T instead of the close confirmation", got)
	}
}
