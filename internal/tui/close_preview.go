package tui

import (
	"context"
	"time"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/logutil"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) startClosePreview(taskID string) (Model, tea.Cmd) {
	if blocked := m.taskWorkflowBlocks(taskID, gitflow.EventActionClose); blocked != "" {
		m.outputPanel.AppendLine(blocked)
		return m, nil
	}
	if selected := m.tasksPanel.SelectedTask(); selected != nil && selected.ID == taskID {
		taskCopy := *selected
		m.pendingCloseTask = &taskCopy
	} else {
		m.pendingCloseTask = &domain.Task{ID: taskID}
	}
	ctx := m.startOperation("close preview for task "+taskID, true)
	m.outputPanel.AppendLine("Planning close for task " + taskID + "...")
	return m, tea.Batch(planCloseTaskCmd(ctx, m.mgr, taskID, m.operationGeneration), m.spinner.Tick)
}

func planCloseTaskCmd(parent context.Context, mgr task.Manager, taskID string, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(parent, taskID), 30*time.Second)
		defer cancel()

		plan, err := mgr.PlanCloseTask(ctx, taskID)
		return ClosePlanReadyMsg{Generation: generation, Plan: plan, Err: err}
	}
}

func (m Model) finishClosePreview(msg ClosePlanReadyMsg) Model {
	if msg.Generation != m.operationGeneration {
		return m
	}
	m.opRunning = false
	// The read may already have returned when the user confirms cancellation.
	if requested, _ := m.finishOperationCancellation(msg.Err); requested {
		m.pendingCloseTask = nil
		m.outputPanel.AppendLine("Close preview cancelled; press C to retry.")
		return m
	}
	if msg.Err != nil {
		m.pendingCloseTask = nil
		m.reportFailure("Plan close task", msg.Err)
		return m
	}
	if msg.Plan.HotfixReview {
		m.modal = modal.NewHotfixCloseModal(msg.Plan)
		m.modal.SetTerminalSize(m.width, m.height)
		return m
	}
	closeTask := m.resolveCloseTask(msg.Plan.TaskID)
	m.modal = modal.NewCloseTaskConfirmModal(closeTask, msg.Plan, m.width, m.height)
	return m
}
