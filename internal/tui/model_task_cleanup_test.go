package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/task"
)

func qualifyingTaskCleanupPreview(taskID string) task.TaskCleanupPreview {
	return task.TaskCleanupPreview{
		TaskID: taskID,
		Services: []task.TaskCleanupServicePreview{{
			Name:     "api",
			RepoPath: "/repos/api",
			Branch:   "feature/" + taskID,
			Complete: true,
		}},
	}
}

func startTaskCleanupInspectionForTest(m Model, taskID string) Model {
	m.taskCleanupGeneration++
	m.taskCleanupRequest = &taskCleanupRequest{generation: m.taskCleanupGeneration, taskID: taskID}
	m.opRunning = true
	return m
}

func TestTaskCleanup_ErrorsSurfaceInOutput(t *testing.T) {
	t.Run("planning error without queue only reports", func(t *testing.T) {
		mgr := &mockManager{}
		m := sendWindowSize(newTestModel(t, mgr), 120, 40)
		m = startTaskCleanupInspectionForTest(m, "T-1")
		m.operationGeneration++
		m.taskCleanupRequest.operationGeneration = m.operationGeneration
		updated, cmd := m.Update(TaskCleanupPlanReadyMsg{TaskID: "T-1", Generation: m.taskCleanupGeneration, OperationGeneration: m.operationGeneration, Err: errors.New("planner boom")})
		m = updated.(Model)
		if cmd != nil {
			runBatchCommands(cmd())
		}
		if m.opRunning || mgr.taskCleanupExecCalls != 0 {
			t.Fatal("planning error left cleanup running")
		}
		if !strings.Contains(m.outputPanel.View(), "Plan task cleanup failed for T-1: planner boom") {
			t.Fatalf("planning error missing: %s", m.outputPanel.View())
		}
	})

	t.Run("stale plan ready is ignored", func(t *testing.T) {
		mgr := &mockManager{}
		m := sendWindowSize(newTestModel(t, mgr), 120, 40)
		m = startTaskCleanupInspectionForTest(m, "T-1")
		stale := TaskCleanupPlanReadyMsg{TaskID: "T-1", Generation: m.taskCleanupGeneration + 9}
		updated, _ := m.Update(stale)
		m = updated.(Model)
		if mgr.taskCleanupExecCalls != 0 || m.taskCleanupRequest == nil {
			t.Fatal("stale plan-ready mutated cleanup state")
		}
	})
}
