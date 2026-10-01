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
)

type releaseCleanupE2EManager struct {
	mockManager
	executeErr error
}

func (m *releaseCleanupE2EManager) ExecuteReleaseCleanup(_ context.Context, _ task.ReleaseCleanupPlan, statusCh chan<- string) (task.ReleaseCleanupResult, error) {
	m.cleanupExecuteCalls++
	for _, line := range []string{"remove release worktree", "remove task worktree", "retain local task branch"} {
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
	mgr.listReleasesResult = []domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}}
	m := sendWindowSize(newTestModel(t, mgr), 140, 40)
	m.outputPanel.SetSize(140, 30)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}})

	m = cleanupE2EOpenDialogAndSelectRelease(t, m)
	m = cleanupE2EStageChecklist(t, m, task.ReleaseCleanupSelection{RemoveRelease: true})
	checklist := m.modal.(*modal.ReleaseCleanupChecklistModal)
	_, submitCmd := checklist.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, _ := m.Update(submitCmd())
	m = updated.(Model)
	m = cleanupE2EStageConfirm(t, m, task.ReleaseCleanupSelection{RemoveRelease: true})
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
	m, executeCmd := cleanupE2EStart(t, m)
	m, statuses := cleanupE2EDrainExecution(t, m, executeCmd)

	if m.tasksPanel.SelectedTask() != nil || m.releasesPanel.SelectedRelease() != nil {
		t.Fatalf("rows retained after successful refresh: task=%+v release=%+v", m.tasksPanel.SelectedTask(), m.releasesPanel.SelectedRelease())
	}
	for _, want := range []string{"remove release worktree", "remove task worktree", "retain local task branch"} {
		if !slices.Contains(statuses, want) {
			t.Fatalf("statuses missing %q: %q", want, statuses)
		}
	}
	if !strings.Contains(m.outputPanel.View(), "Release cleanup done: rel-1") {
		t.Fatalf("completion missing: %s", m.outputPanel.View())
	}
}

func TestE2E_ReleaseCleanupChecklistCannotRequestBranchDeletion(t *testing.T) {
	mgr := &releaseCleanupE2EManager{}
	mgr.listTasksResult = []domain.Task{{ID: "TASK-1"}}
	mgr.listReleasesResult = []domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}}
	m := cleanupE2EModel(t, mgr)

	m = cleanupE2EOpenDialogAndSelectRelease(t, m)
	selection := task.ReleaseCleanupSelection{RemoveRelease: true}
	m = cleanupE2EStageChecklist(t, m, selection)
	if _, ok := m.modal.(*modal.ReleaseCleanupChecklistModal); !ok {
		t.Fatalf("checklist not open: %T", m.modal)
	}
	for range 2 {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
		m = updated.(Model)
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(Model)
	}
	checklist := m.modal.(*modal.ReleaseCleanupChecklistModal)
	scope := checklist.Selection()
	if scope.DeleteLocalTaskBranches || scope.DeleteRemoteTaskBranches || scope.DeleteLocalReleaseBranches || scope.DeleteRemoteReleaseBranches {
		t.Fatalf("checklist produced branch deletion flags: %+v", scope)
	}

	updated, enterCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if enterCmd == nil {
		t.Fatal("checklist enter did not submit")
	}
	submit, ok := enterCmd().(modal.SubmitReleaseCleanupMsg)
	if !ok {
		t.Fatalf("submit = %T", enterCmd())
	}
	if submit.Selection.DeleteRemoteTaskBranches || submit.Selection.DeleteRemoteReleaseBranches || submit.Selection.DeleteLocalTaskBranches || submit.Selection.DeleteLocalReleaseBranches {
		t.Fatalf("submit requested branch deletion: %+v", submit.Selection)
	}
	updated, _ = m.Update(submit)
	m = updated.(Model)

	m = cleanupE2EStageConfirm(t, m, submit.Selection)
	confirm, ok := m.modal.(*modal.ReleaseCleanupConfirmModal)
	if !ok {
		t.Fatalf("confirm modal = %T", m.modal)
	}
	_, confirmCmd := confirm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, executeCmd := m.Update(confirmCmd())
	m = updated.(Model)
	if _, ok := m.modal.(*modal.ReleaseCleanupRemoteConfirmModal); ok {
		t.Fatal("remote branch confirmation opened from manual flow")
	}
	if executeCmd == nil || !m.opRunning {
		t.Fatal("cleanup execution did not start")
	}
}

func TestE2E_ReleaseCleanupPartialFailureRefreshRetainsReleaseRow(t *testing.T) {
	mgr := &releaseCleanupE2EManager{executeErr: errors.New("branch deletion failed")}
	mgr.listTasksResult = []domain.Task{{ID: "TASK-1"}}
	mgr.listReleasesResult = []domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}}
	m := cleanupE2EModel(t, mgr)
	m, executeCmd := cleanupE2EStart(t, m)
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

// cleanupE2EOpenDialogAndSelectRelease drives D, feeds the scan back, selects
// the release candidate, and submits, leaving the release replan in flight.
func cleanupE2EOpenDialogAndSelectRelease(t *testing.T, m Model) Model {
	t.Helper()
	updated, keyCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("D")})
	m = updated.(Model)
	if keyCmd == nil {
		t.Fatal("D did not emit OpenCleanupDialogMsg")
	}
	updated, scanCmd := m.Update(keyCmd())
	m = updated.(Model)
	if scanCmd == nil {
		t.Fatal("cleanup scan did not start")
	}
	ready, ok := extractCleanupScanReady(scanCmd())
	if !ok {
		t.Fatal("D did not produce a cleanup scan")
	}
	var release *modal.CleanupCandidate
	for i := range ready.Candidates {
		if ready.Candidates[i].Kind == modal.CleanupKindRelease && ready.Candidates[i].ID == "rel-1" {
			release = &ready.Candidates[i]
		}
	}
	if release == nil || !release.Ready {
		t.Fatalf("release candidate = %+v, want ready rel-1", release)
	}
	updated, _ = m.Update(ready)
	m = updated.(Model)
	if _, ok := m.modal.(*modal.CleanupCandidatesModal); !ok {
		t.Fatalf("candidates dialog not open: %T", m.modal)
	}
	updated, _ = m.Update(modal.SubmitCleanupMsg{Generation: m.cleanupScanModalGen, Releases: []string{"rel-1"}})
	m = updated.(Model)
	if m.releaseCleanupRequest == nil {
		t.Fatal("release replan not in flight after submit")
	}
	return m
}

// cleanupE2EStageChecklist installs the checklist a real identity-bearing
// plan-ready would have opened; synthetic zero plans are rejected at
// plan-ready.
func cleanupE2EStageChecklist(t *testing.T, m Model, selection task.ReleaseCleanupSelection) Model {
	t.Helper()
	if m.releaseCleanupRequest == nil {
		t.Fatal("release replan not in flight")
	}
	m.releaseCleanupRequest = nil
	m.opRunning = false
	m.modal = modal.NewReleaseCleanupChecklistModal(testCleanupPreview(selection))
	m.modal.SetTerminalSize(m.width, m.height)
	return m
}

// cleanupE2EStageConfirm installs the pending approval and confirm modal a
// real unblocked plan-ready would have produced.
func cleanupE2EStageConfirm(t *testing.T, m Model, selection task.ReleaseCleanupSelection) Model {
	t.Helper()
	if m.releaseCleanupRequest == nil {
		t.Fatal("release replan not in flight")
	}
	plan := task.ReleaseCleanupPlan{}
	m.releaseCleanupRequest = nil
	m.opRunning = false
	m.pendingReleaseCleanupPlan = &plan
	m.pendingReleaseCleanupPreview = testCleanupPreview(selection)
	m.modal = modal.NewReleaseCleanupConfirmModal(m.pendingReleaseCleanupPreview, m.releaseCleanupGeneration)
	m.modal.SetTerminalSize(m.width, m.height)
	return m
}

func cleanupE2EStart(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	m = cleanupE2EOpenDialogAndSelectRelease(t, m)
	selection := task.ReleaseCleanupSelection{RemoveRelease: true}
	m = cleanupE2EStageChecklist(t, m, selection)
	if _, ok := m.modal.(*modal.ReleaseCleanupChecklistModal); !ok {
		t.Fatalf("checklist not open: %T", m.modal)
	}
	updated, enterCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if enterCmd == nil {
		t.Fatal("checklist enter did not submit")
	}
	updated, _ = m.Update(enterCmd())
	m = updated.(Model)
	m = cleanupE2EStageConfirm(t, m, selection)
	confirm := m.modal.(*modal.ReleaseCleanupConfirmModal)
	_, confirmCmd := confirm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, executeCmd := m.Update(confirmCmd())
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
