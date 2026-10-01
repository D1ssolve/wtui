package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/task"
)

// staleOpFixture builds a model where operation B (generation 2) is running,
// so a completion carrying generation 1 belongs to the superseded operation A.
func staleOpFixture(t *testing.T) (Model, *mockManager) {
	t.Helper()
	mgr := &mockManager{}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m.operationGeneration = 2
	m.opRunning = true
	return m, mgr
}

func TestUpdate_StaleCreateReleaseDoneMsg_Ignored(t *testing.T) {
	m, _ := staleOpFixture(t)
	before := m.outputPanel.View()

	updated, cmd := m.Update(CreateReleaseDoneMsg{Generation: 1, Release: domain.Release{ID: "rel-stale"}})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("stale CreateReleaseDoneMsg must not return a command")
	}
	if !m.opRunning {
		t.Fatal("stale CreateReleaseDoneMsg must not clear opRunning")
	}
	if m.outputPanel.View() != before {
		t.Fatalf("stale CreateReleaseDoneMsg mutated output: %q", m.outputPanel.View())
	}
}

func TestUpdate_StaleReleaseMergeDoneMsg_Ignored(t *testing.T) {
	m, _ := staleOpFixture(t)
	before := m.outputPanel.View()

	updated, cmd := m.Update(ReleaseMergeDoneMsg{Generation: 1, Release: domain.Release{ID: "rel-stale"}, Result: task.ReleaseMergeResult{ReleaseID: "rel-stale", Merged: []string{"api"}}})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("stale ReleaseMergeDoneMsg must not return a command")
	}
	if !m.opRunning {
		t.Fatal("stale ReleaseMergeDoneMsg must not clear opRunning")
	}
	if m.outputPanel.View() != before {
		t.Fatalf("stale ReleaseMergeDoneMsg mutated output: %q", m.outputPanel.View())
	}
}

func TestUpdate_StaleReleaseActionDoneMsg_Ignored(t *testing.T) {
	m, _ := staleOpFixture(t)
	before := m.outputPanel.View()

	updated, cmd := m.Update(ReleaseActionDoneMsg{Generation: 1, Action: "finalize", Release: domain.Release{ID: "rel-stale", Status: domain.ReleaseStatusReleased, TaskIDs: []string{"T-1"}}})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("stale ReleaseActionDoneMsg must not return a command")
	}
	if !m.opRunning {
		t.Fatal("stale ReleaseActionDoneMsg must not clear opRunning")
	}
	if m.outputPanel.View() != before {
		t.Fatalf("stale ReleaseActionDoneMsg mutated output: %q", m.outputPanel.View())
	}
}

func TestUpdate_StaleTaskMergeDoneMsg_Ignored(t *testing.T) {
	m, mgr := staleOpFixture(t)
	before := m.outputPanel.View()

	updated, cmd := m.Update(TaskMergeDoneMsg{Generation: 1, Result: task.TaskMergeResult{TaskID: "T-1", Merged: []string{"api"}}})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("stale TaskMergeDoneMsg must not return a command")
	}
	if !m.opRunning {
		t.Fatal("stale TaskMergeDoneMsg must not clear opRunning")
	}
	if m.outputPanel.View() != before {
		t.Fatalf("stale TaskMergeDoneMsg mutated output: %q", m.outputPanel.View())
	}
	if mgr.taskCleanupPlanCalls != 0 {
		t.Fatalf("stale TaskMergeDoneMsg triggered %d cleanup inspections", mgr.taskCleanupPlanCalls)
	}
	if len(m.cleanupQueue) != 0 {
		t.Fatalf("stale TaskMergeDoneMsg queued cleanup tasks: %v", m.cleanupQueue)
	}
}

func TestUpdate_CurrentGenerationDoneMsg_StillClearsOpRunning(t *testing.T) {
	t.Run("create release error path", func(t *testing.T) {
		m, _ := staleOpFixture(t)

		updated, _ := m.Update(CreateReleaseDoneMsg{Generation: 2, Err: errors.New("create failed")})
		m = updated.(Model)

		if m.opRunning {
			t.Fatal("current-generation CreateReleaseDoneMsg must clear opRunning")
		}
		if !strings.Contains(m.outputPanel.View(), "Create release failed: create failed") {
			t.Fatalf("output missing failure line: %q", m.outputPanel.View())
		}
	})

	t.Run("release action success path", func(t *testing.T) {
		m, _ := staleOpFixture(t)

		updated, cmd := m.Update(ReleaseActionDoneMsg{Generation: 2, Action: "finalize", Release: domain.Release{ID: "rel-1"}})
		m = updated.(Model)

		if m.opRunning {
			t.Fatal("current-generation ReleaseActionDoneMsg must clear opRunning")
		}
		if cmd == nil {
			t.Fatal("current-generation ReleaseActionDoneMsg must refresh releases")
		}
	})

	t.Run("task merge success path does not trigger cleanup", func(t *testing.T) {
		m, mgr := staleOpFixture(t)

		updated, cmd := m.Update(TaskMergeDoneMsg{Generation: 2, Result: task.TaskMergeResult{TaskID: "T-1", Merged: []string{"api"}}})
		m = updated.(Model)

		if cmd == nil {
			t.Fatal("current-generation TaskMergeDoneMsg must trigger reload commands")
		}
		runBatchCommands(cmd())
		if mgr.taskCleanupPlanCalls != 0 {
			t.Fatalf("cleanup inspections = %d, want 0", mgr.taskCleanupPlanCalls)
		}
		if m.taskCleanupRequest != nil {
			t.Fatalf("cleanup request = %+v, want none", m.taskCleanupRequest)
		}
	})
}
