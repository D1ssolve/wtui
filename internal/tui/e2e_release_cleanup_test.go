package tui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	"github.com/D1ssolve/wtui/internal/tui/panels"
)

type releaseCleanupE2EManager struct {
	mockManager
	executeErr error
}

func (m *releaseCleanupE2EManager) ExecuteReleaseCleanup(_ context.Context, _ task.ReleaseCleanupPlan, statusCh chan<- string) (task.ReleaseCleanupResult, error) {
	m.cleanupExecuteCalls++
	for _, line := range []string{"remove release worktree", "remove task worktree", "delete local task branch"} {
		statusCh <- line
	}
	m.listTasksResult = nil
	if m.executeErr == nil {
		m.listReleasesResult = nil
	}
	return task.ReleaseCleanupResult{ReleaseID: "rel-1"}, m.executeErr
}

func TestE2E_ReleasedCleanupPlansChecksConfirmsAndStartsStreamingExecution(t *testing.T) {
	mgr := &mockManager{cleanupExecuteStatuses: []string{"remove task worktree"}, cleanupExecuteResult: task.ReleaseCleanupResult{ReleaseID: "rel-1"}}
	m := sendWindowSize(newTestModel(t, mgr), 140, 40)
	m.outputPanel.SetSize(140, 30)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}})

	updated, panelCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("D")})
	m = updated.(Model)
	request := panelCmd().(panels.PlanReleaseCleanupMsg)
	updated, _ = m.Update(request)
	m = updated.(Model)
	generation := m.releaseCleanupGeneration
	updated, _ = m.Update(ReleaseCleanupPlanReadyMsg{Generation: generation, Preview: testCleanupPreview(task.DefaultReleaseCleanupSelection())})
	m = updated.(Model)
	checklist := m.modal.(*modal.ReleaseCleanupChecklistModal)
	_, submitCmd := checklist.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, _ = m.Update(submitCmd())
	m = updated.(Model)
	generation = m.releaseCleanupGeneration
	updated, _ = m.Update(ReleaseCleanupPlanReadyMsg{Generation: generation, Preview: testCleanupPreview(task.DefaultReleaseCleanupSelection())})
	m = updated.(Model)
	confirm := m.modal.(*modal.ReleaseCleanupConfirmModal)
	_, confirmCmd := confirm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, executeCmd := m.Update(confirmCmd())
	m = updated.(Model)
	if executeCmd == nil || !m.opRunning {
		t.Fatal("cleanup execution did not start")
	}
}

func TestE2E_ReleaseCleanupSuccessStreamsCompletesAndRefreshesRows(t *testing.T) {
	mgr := &releaseCleanupE2EManager{}
	mgr.listTasksResult = []domain.Task{{ID: "TASK-1"}}
	mgr.listReleasesResult = []domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}}
	m := cleanupE2EModel(t, mgr)
	m, executeCmd := cleanupE2EStart(t, m, false)
	m, statuses := cleanupE2EDrainExecution(t, m, executeCmd)

	if m.tasksPanel.SelectedTask() != nil || m.releasesPanel.SelectedRelease() != nil {
		t.Fatalf("rows retained after successful refresh: task=%+v release=%+v", m.tasksPanel.SelectedTask(), m.releasesPanel.SelectedRelease())
	}
	for _, want := range []string{"remove release worktree", "remove task worktree", "delete local task branch"} {
		if !slices.Contains(statuses, want) {
			t.Fatalf("statuses missing %q: %q", want, statuses)
		}
	}
	if !strings.Contains(m.outputPanel.View(), "Release cleanup done: rel-1") {
		t.Fatalf("completion missing: %s", m.outputPanel.View())
	}
}

func TestE2E_ReleaseCleanupRemotePathRequiresWarningThenCompletes(t *testing.T) {
	mgr := &releaseCleanupE2EManager{}
	mgr.listTasksResult = []domain.Task{{ID: "TASK-1"}}
	mgr.listReleasesResult = []domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}}
	m := cleanupE2EModel(t, mgr)
	m, executeCmd := cleanupE2EStart(t, m, true)
	if executeCmd == nil {
		t.Fatal("remote confirmation did not start execution")
	}
	m, statuses := cleanupE2EDrainExecution(t, m, executeCmd)
	if len(statuses) != 3 {
		t.Fatalf("remote statuses = %q", statuses)
	}
	if mgr.cleanupExecuteCalls != 1 || m.releasesPanel.SelectedRelease() != nil {
		t.Fatalf("execute calls=%d release=%+v", mgr.cleanupExecuteCalls, m.releasesPanel.SelectedRelease())
	}
}

func TestE2E_ReleaseCleanupPartialFailureRefreshRetainsReleaseRow(t *testing.T) {
	mgr := &releaseCleanupE2EManager{executeErr: errors.New("branch deletion failed")}
	mgr.listTasksResult = []domain.Task{{ID: "TASK-1"}}
	mgr.listReleasesResult = []domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}}
	m := cleanupE2EModel(t, mgr)
	m, executeCmd := cleanupE2EStart(t, m, false)
	m, statuses := cleanupE2EDrainExecution(t, m, executeCmd)
	if len(statuses) != 3 {
		t.Fatalf("partial failure statuses = %q", statuses)
	}

	if m.tasksPanel.SelectedTask() != nil {
		t.Fatalf("removed task row retained: %+v", m.tasksPanel.SelectedTask())
	}
	if release := m.releasesPanel.SelectedRelease(); release == nil || release.ID != "rel-1" {
		t.Fatalf("failed cleanup release row = %+v", release)
	}
	if !strings.Contains(m.outputPanel.View(), "Release cleanup failed: branch deletion failed") {
		t.Fatalf("failure missing: %s", m.outputPanel.View())
	}
}

func cleanupE2EModel(t *testing.T, mgr task.Manager) Model {
	t.Helper()
	m := sendWindowSize(newTestModel(t, mgr), 140, 40)
	m.outputPanel.SetSize(140, 30)
	m.tasks = []domain.Task{{ID: "TASK-1"}}
	m.tasksPanel.SetTasks(m.tasks)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}})
	return m
}

func cleanupE2EStart(t *testing.T, m Model, remote bool) (Model, tea.Cmd) {
	t.Helper()
	updated, panelCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("D")})
	m = updated.(Model)
	request := panelCmd().(panels.PlanReleaseCleanupMsg)
	updated, _ = m.Update(request)
	m = updated.(Model)
	selection := task.DefaultReleaseCleanupSelection()
	updated, _ = m.Update(ReleaseCleanupPlanReadyMsg{Generation: m.releaseCleanupGeneration, Preview: testCleanupPreview(selection)})
	m = updated.(Model)
	checklist := m.modal.(*modal.ReleaseCleanupChecklistModal)
	if remote {
		for range 2 {
			updatedModal, _ := checklist.Update(tea.KeyMsg{Type: tea.KeyDown})
			checklist = updatedModal.(*modal.ReleaseCleanupChecklistModal)
		}
		updatedModal, _ := checklist.Update(tea.KeyMsg{Type: tea.KeySpace})
		checklist = updatedModal.(*modal.ReleaseCleanupChecklistModal)
		selection.DeleteRemoteTaskBranches = true
	}
	_, submitCmd := checklist.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, _ = m.Update(submitCmd())
	m = updated.(Model)
	updated, _ = m.Update(ReleaseCleanupPlanReadyMsg{Generation: m.releaseCleanupGeneration, Preview: testCleanupPreview(selection)})
	m = updated.(Model)
	confirm := m.modal.(*modal.ReleaseCleanupConfirmModal)
	_, confirmCmd := confirm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, executeCmd := m.Update(confirmCmd())
	m = updated.(Model)
	if !remote {
		return m, executeCmd
	}
	if executeCmd != nil {
		t.Fatal("first remote confirmation executed cleanup")
	}
	remoteConfirm, ok := m.modal.(*modal.ReleaseCleanupRemoteConfirmModal)
	if !ok {
		t.Fatalf("remote modal = %T", m.modal)
	}
	_, remoteCmd := remoteConfirm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, executeCmd = m.Update(remoteCmd())
	return updated.(Model), executeCmd
}

func cleanupE2EDrainExecution(t *testing.T, m Model, cmd tea.Cmd) (Model, []string) {
	t.Helper()
	if cmd == nil {
		t.Fatal("cleanup execution command is nil")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("execution command = %T, want tea.BatchMsg", msg)
	}
	statuses := make([]string, 0, 3)
	for _, batchCmd := range batch {
		if batchCmd == nil {
			continue
		}
		msg = batchCmd()
		for {
			if line, ok := msg.(OutputLineMsg); ok {
				statuses = append(statuses, line.Line)
			}
			switch msg.(type) {
			case OutputLineMsg, ReleaseCleanupDoneMsg:
				updated, next := m.Update(msg)
				m = updated.(Model)
				if _, done := msg.(ReleaseCleanupDoneMsg); done {
					m = cleanupE2EApplyRefresh(t, m, next)
					return m, statuses
				}
				if next == nil {
					t.Fatal("status line has no continuation")
				}
				msg = next()
			default:
				break
			}
			if _, streamMsg := msg.(OutputLineMsg); !streamMsg {
				if _, doneMsg := msg.(ReleaseCleanupDoneMsg); !doneMsg {
					break
				}
			}
		}
	}
	t.Fatal("cleanup command produced no completion")
	return m, statuses
}

func cleanupE2EApplyRefresh(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("cleanup completion returned no refresh")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatal("cleanup refresh is not batch")
	}
	for _, refreshCmd := range batch {
		if refreshCmd == nil {
			continue
		}
		updated, _ := m.Update(refreshCmd())
		m = updated.(Model)
	}
	return m
}
