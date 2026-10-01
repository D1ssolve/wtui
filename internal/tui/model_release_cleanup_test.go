package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	"github.com/D1ssolve/wtui/internal/tui/panels"
)

func testCleanupPreview(selection task.ReleaseCleanupSelection, blockers ...string) task.ReleaseCleanupPreview {
	return task.ReleaseCleanupPreview{
		ReleaseID: "rel-1",
		Selection: selection,
		Tasks:     []string{"TASK-1"},
		Services:  []task.ReleaseCleanupServicePreview{{Name: "api", TaskBranches: []string{"feature/TASK-1"}}},
		Blockers:  blockers,
	}
}

func TestUpdate_ReleaseCleanupPlanReadyIgnoresStaleGeneration(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}})
	selection := task.DefaultReleaseCleanupSelection()
	m.releaseCleanupGeneration = 2
	m.releaseCleanupRequest = &releaseCleanupRequest{generation: 2, releaseID: "rel-1", selection: selection}

	updated, _ := m.Update(ReleaseCleanupPlanReadyMsg{Generation: 1})
	m = updated.(Model)
	if m.modal != nil || m.releaseCleanupRequest == nil {
		t.Fatalf("stale result mutated state: modal=%T request=%v", m.modal, m.releaseCleanupRequest != nil)
	}
}

func TestUpdate_ReleaseCleanupSubmitStartsReplanAndRejectsZeroPlan(t *testing.T) {
	selection := task.ReleaseCleanupSelection{RemoveTasks: true, RemoveRelease: true}
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}})
	m.modal = modal.NewReleaseCleanupChecklistModal(testCleanupPreview(selection))
	updated, cmd := m.Update(modal.SubmitReleaseCleanupMsg{ReleaseID: "rel-1", Selection: selection})
	m = updated.(Model)
	if cmd == nil || m.releaseCleanupRequest == nil || !m.releaseCleanupRequest.confirm {
		t.Fatal("submitted selection did not start replan")
	}

	updated, _ = m.Update(ReleaseCleanupPlanReadyMsg{Generation: m.releaseCleanupGeneration})
	m = updated.(Model)
	if m.pendingReleaseCleanupPlan != nil {
		t.Fatal("zero plan stored as approval")
	}
	if _, ok := m.modal.(*modal.ReleaseCleanupConfirmModal); ok {
		t.Fatal("zero plan opened confirmation")
	}
}

func TestUpdate_ReleaseCleanupLocalConfirmExecutesStoredPlan(t *testing.T) {
	mgr := &mockManager{cleanupExecuteResult: task.ReleaseCleanupResult{ReleaseID: "rel-1"}}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}})
	selection := task.DefaultReleaseCleanupSelection()
	m.releaseCleanupGeneration = 5
	m.pendingReleaseCleanupPlan = &task.ReleaseCleanupPlan{}
	m.pendingReleaseCleanupPreview = testCleanupPreview(selection)
	m.modal = modal.NewReleaseCleanupConfirmModal(m.pendingReleaseCleanupPreview, 5)
	m.cleanupQueueCurrent = &cleanupQueueItem{kind: modal.CleanupKindRelease, id: "rel-1"}

	updated, cmd := m.Update(modal.ConfirmReleaseCleanupMsg{ReleaseID: "rel-1", Generation: 5})
	m = updated.(Model)
	if cmd == nil || !m.opRunning || m.pendingReleaseCleanupPlan != nil {
		t.Fatalf("local cleanup not started: cmd nil=%v running=%v pending=%v", cmd == nil, m.opRunning, m.pendingReleaseCleanupPlan != nil)
	}
	runBatchCommands(cmd())
	if mgr.cleanupExecuteCalls != 1 {
		t.Fatalf("execute calls = %d", mgr.cleanupExecuteCalls)
	}
}

func TestUpdate_ReleaseCleanupRemoteRequiresSecondConfirmAndRejectsStale(t *testing.T) {
	mgr := &mockManager{}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}})
	selection := task.DefaultReleaseCleanupSelection()
	selection.DeleteRemoteTaskBranches = true
	m.releaseCleanupGeneration = 8
	m.pendingReleaseCleanupPlan = &task.ReleaseCleanupPlan{}
	m.pendingReleaseCleanupPreview = testCleanupPreview(selection)
	m.modal = modal.NewReleaseCleanupConfirmModal(m.pendingReleaseCleanupPreview, 8)
	m.cleanupQueueCurrent = &cleanupQueueItem{kind: modal.CleanupKindRelease, id: "rel-1"}

	updated, cmd := m.Update(modal.ConfirmReleaseCleanupMsg{ReleaseID: "rel-1", Generation: 7})
	m = updated.(Model)
	if cmd != nil || mgr.cleanupExecuteCalls != 0 {
		t.Fatal("stale normal confirmation executed")
	}
	updated, cmd = m.Update(modal.ConfirmReleaseCleanupMsg{ReleaseID: "rel-1", Generation: 8})
	m = updated.(Model)
	if cmd != nil || mgr.cleanupExecuteCalls != 0 {
		t.Fatal("first remote confirmation executed")
	}
	if _, ok := m.modal.(*modal.ReleaseCleanupRemoteConfirmModal); !ok {
		t.Fatalf("modal = %T", m.modal)
	}
	updated, cmd = m.Update(modal.ConfirmRemoteReleaseCleanupMsg{ReleaseID: "other", Generation: 8})
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("unmatched remote confirmation executed")
	}
	updated, cmd = m.Update(modal.ConfirmRemoteReleaseCleanupMsg{ReleaseID: "rel-1", Generation: 8})
	m = updated.(Model)
	if cmd == nil || !m.opRunning {
		t.Fatal("matching remote confirmation did not execute")
	}
}

func TestUpdate_ReleaseCleanupCancellationInvalidatesPendingPlan(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.releaseCleanupGeneration = 3
	m.pendingReleaseCleanupPlan = &task.ReleaseCleanupPlan{}
	m.pendingReleaseCleanupPreview = testCleanupPreview(task.DefaultReleaseCleanupSelection())
	m.modal = modal.NewReleaseCleanupConfirmModal(m.pendingReleaseCleanupPreview, 3)
	updated, _ := m.Update(modal.CloseModalMsg{})
	m = updated.(Model)
	if m.pendingReleaseCleanupPlan != nil || m.releaseCleanupGeneration == 3 {
		t.Fatal("cancel did not invalidate pending cleanup")
	}
}

func TestUpdate_ReleaseCleanupDoneRefreshesTasksReleasesAndRepoOnSuccessOrFailure(t *testing.T) {
	for _, executeErr := range []error{nil, errors.New("partial failure")} {
		mgr := &mockManager{}
		m := sendWindowSize(newTestModel(t, mgr), 120, 40)
		m.opRunning = true
		m.releaseCleanupExecuting = 4
		m.releaseCleanupExecutingID = "rel-1"
		updated, cmd := m.Update(ReleaseCleanupDoneMsg{Generation: 4, Result: task.ReleaseCleanupResult{ReleaseID: "rel-1"}, Err: executeErr})
		m = updated.(Model)
		if cmd == nil || m.opRunning {
			t.Fatalf("done state: cmd nil=%v running=%v", cmd == nil, m.opRunning)
		}
		runBatchCommands(cmd())
		if mgr.listTasksCalls != 1 || mgr.listReleasesCalls != 1 || len(mgr.repoRefreshArgs) != 1 || !mgr.repoRefreshArgs[0] {
			t.Fatalf("refreshes: tasks=%d releases=%d repos=%v", mgr.listTasksCalls, mgr.listReleasesCalls, mgr.repoRefreshArgs)
		}
		want := "Release cleanup done: rel-1"
		if executeErr != nil {
			want = "Release cleanup failed: partial failure"
		}
		if !strings.Contains(m.outputPanel.View(), want) {
			t.Fatalf("output missing %q: %s", want, m.outputPanel.View())
		}
	}
}

func TestUpdate_ReleaseCleanupDoneIgnoresStaleGeneration(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.opRunning = true
	m.releaseCleanupExecuting = 5
	updated, cmd := m.Update(ReleaseCleanupDoneMsg{Generation: 4, Result: task.ReleaseCleanupResult{ReleaseID: "rel-1"}})
	m = updated.(Model)
	if cmd != nil || !m.opRunning {
		t.Fatal("stale completion changed active operation")
	}
}

func TestUpdate_ReleaseCleanupPlanningInvalidatedByFocusOrSelectionChange(t *testing.T) {
	for _, change := range []struct {
		name string
		key  string
	}{
		{name: "focus", key: "1"},
		{name: "selection", key: "j"},
	} {
		t.Run(change.name, func(t *testing.T) {
			m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
			m.setFocus(FocusReleases)
			m.releasesPanel.SetReleases([]domain.Release{
				{ID: "rel-1", Status: domain.ReleaseStatusReleased},
				{ID: "rel-2", Status: domain.ReleaseStatusReleased},
			})
			planned, _ := m.startReleaseCleanupPlan("rel-1", task.DefaultReleaseCleanupSelection(), false)
			m = planned
			generation := m.releaseCleanupGeneration

			updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(change.key)})
			m = updated.(Model)
			if m.releaseCleanupRequest != nil || m.opRunning || m.releaseCleanupGeneration == generation {
				t.Fatalf("planning not invalidated: request=%v running=%v generation=%d", m.releaseCleanupRequest != nil, m.opRunning, m.releaseCleanupGeneration)
			}
		})
	}
}

func TestUpdate_ReleaseCleanupStateBlocksRepeatedAndMutatingReleaseActions(t *testing.T) {
	selection := task.DefaultReleaseCleanupSelection()
	for _, phase := range []string{"op-running", "planning", "checklist", "confirming", "executing"} {
		t.Run(phase, func(t *testing.T) {
			m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
			m.setFocus(FocusReleases)
			m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}})
			switch phase {
			case "op-running":
				m.opRunning = true
			case "planning":
				m.opRunning = true
				m.releaseCleanupRequest = &releaseCleanupRequest{generation: 1, releaseID: "rel-1", selection: selection}
			case "checklist":
				m.modal = modal.NewReleaseCleanupChecklistModal(testCleanupPreview(selection))
			case "confirming":
				m.pendingReleaseCleanupPlan = &task.ReleaseCleanupPlan{}
				m.pendingReleaseCleanupPreview = testCleanupPreview(selection)
			case "executing":
				m.opRunning = true
				m.releaseCleanupExecuting = 1
			}
			generation := m.releaseCleanupGeneration
			originalModal := m.modal
			for _, action := range []struct {
				status domain.ReleaseStatus
				msg    tea.Msg
			}{
				{status: domain.ReleaseStatusReleased, msg: panels.OpenCleanupDialogMsg{}},
				{status: domain.ReleaseStatusDraft, msg: panels.OpenCreateReleaseDialogMsg{}},
				{status: domain.ReleaseStatusPrepared, msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("F")}},
				{status: domain.ReleaseStatusAwaitingMasterMerge, msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("M")}},
			} {
				m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: action.status}})
				updated, cmd := m.Update(action.msg)
				m = updated.(Model)
				if cmd != nil {
					t.Fatalf("%T returned mutating command during %s", action.msg, phase)
				}
			}
			if m.releaseCleanupGeneration != generation || m.modal != originalModal {
				t.Fatalf("blocked action changed state: generation=%d modal=%T", m.releaseCleanupGeneration, m.modal)
			}
		})
	}
}

func TestUpdate_ReleaseCleanupSecondExecutionCannotOverwriteActiveGeneration(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}})
	m.releaseCleanupGeneration = 10
	m.releaseCleanupExecuting = 9
	m.pendingReleaseCleanupPlan = &task.ReleaseCleanupPlan{}
	m.pendingReleaseCleanupPreview = testCleanupPreview(task.DefaultReleaseCleanupSelection())
	m.modal = modal.NewReleaseCleanupConfirmModal(m.pendingReleaseCleanupPreview, 10)
	m.cleanupQueueCurrent = &cleanupQueueItem{kind: modal.CleanupKindRelease, id: "rel-1"}

	updated, cmd := m.Update(modal.ConfirmReleaseCleanupMsg{ReleaseID: "rel-1", Generation: 10})
	m = updated.(Model)
	if cmd != nil || m.releaseCleanupExecuting != 9 {
		t.Fatalf("second execution cmd=%v generation=%d", cmd, m.releaseCleanupExecuting)
	}
}

func TestUpdate_ReleaseCleanupConfirmRequiresCurrentReleasedSelection(t *testing.T) {
	for _, remote := range []bool{false, true} {
		for _, tc := range []struct {
			name     string
			focus    FocusPanel
			release  domain.Release
			selected domain.Release
		}{
			{name: "focus drift", focus: FocusTasks, release: domain.Release{ID: "rel-1", Status: domain.ReleaseStatusReleased}},
			{name: "selection drift", focus: FocusReleases, release: domain.Release{ID: "rel-2", Status: domain.ReleaseStatusReleased}},
			{name: "status drift", focus: FocusReleases, release: domain.Release{ID: "rel-1", Status: domain.ReleaseStatusFailed}},
		} {
			t.Run(fmt.Sprintf("remote=%v/%s", remote, tc.name), func(t *testing.T) {
				m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
				m.setFocus(tc.focus)
				m.releasesPanel.SetReleases([]domain.Release{tc.release})
				selection := task.DefaultReleaseCleanupSelection()
				if remote {
					selection.DeleteRemoteTaskBranches = true
				}
				m.releaseCleanupGeneration = 4
				m.pendingReleaseCleanupPlan = &task.ReleaseCleanupPlan{}
				m.pendingReleaseCleanupPreview = testCleanupPreview(selection)
				var msg tea.Msg
				if remote {
					m.modal = modal.NewReleaseCleanupRemoteConfirmModal(m.pendingReleaseCleanupPreview, 4)
					msg = modal.ConfirmRemoteReleaseCleanupMsg{ReleaseID: "rel-1", Generation: 4}
				} else {
					m.modal = modal.NewReleaseCleanupConfirmModal(m.pendingReleaseCleanupPreview, 4)
					msg = modal.ConfirmReleaseCleanupMsg{ReleaseID: "rel-1", Generation: 4}
				}
				m.cleanupQueueCurrent = &cleanupQueueItem{kind: modal.CleanupKindRelease, id: "rel-1"}
				updated, cmd := m.Update(msg)
				m = updated.(Model)
				if cmd != nil || m.releaseCleanupExecuting != 0 {
					t.Fatalf("stale confirmation executed: cmd=%v generation=%d", cmd, m.releaseCleanupExecuting)
				}
			})
		}
	}
}

func TestUpdate_ReleasesLoadedDriftInvalidatesCleanupConfirmation(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}})
	m.releaseCleanupGeneration = 3
	m.pendingReleaseCleanupPlan = &task.ReleaseCleanupPlan{}
	m.pendingReleaseCleanupPreview = testCleanupPreview(task.DefaultReleaseCleanupSelection())
	m.modal = modal.NewReleaseCleanupConfirmModal(m.pendingReleaseCleanupPreview, 3)

	updated, _ := m.Update(ReleasesLoadedMsg{Releases: []domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusFailed}}})
	m = updated.(Model)
	if m.pendingReleaseCleanupPlan != nil || m.modal != nil || m.releaseCleanupGeneration == 3 {
		t.Fatalf("drift retained approval: plan=%v modal=%T generation=%d", m.pendingReleaseCleanupPlan != nil, m.modal, m.releaseCleanupGeneration)
	}
}

func TestUpdate_ExecutingReleaseCleanupBlocksTaskMutations(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	m.setFocus(FocusTasks)
	m.tasksPanel.SetTasks([]domain.Task{{ID: "TASK-1"}})
	m.releaseCleanupExecuting = 7
	m.opRunning = true

	for _, msg := range []tea.Msg{
		panels.OpenRemoveDialogMsg{TaskID: "TASK-1"},
		panels.PlanCloseTaskMsg{TaskID: "TASK-1"},
		panels.OpenSyncStrategyDialogMsg{TaskID: "TASK-1"},
		panels.PushTaskMsg{TaskID: "TASK-1"},
		panels.ShellExecMsg{TaskDir: "/tasks/TASK-1"},
		modal.SubmitCleanupMsg{Generation: 1, Tasks: []string{"TASK-1"}},
		modal.ConfirmMergeMsg{TaskID: "TASK-1"},
	} {
		updated, cmd := m.Update(msg)
		m = updated.(Model)
		if cmd != nil || m.modal != nil || m.releaseCleanupExecuting != 7 {
			t.Fatalf("%T escaped cleanup lock: cmd=%v modal=%T generation=%d", msg, cmd, m.modal, m.releaseCleanupExecuting)
		}
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("M")})
	m = updated.(Model)
	if cmd != nil || m.mergeInspection != nil {
		t.Fatal("task merge key escaped cleanup lock")
	}
}

func TestUpdate_ExecutingReleaseCleanupBlocksServiceMutations(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	m.setFocus(FocusServices)
	m.servicesPanel.SetServices("TASK-1", []domain.Service{{Name: "api", WorktreePath: "/tasks/TASK-1/api"}})
	m.releaseCleanupExecuting = 7
	m.opRunning = true

	for _, msg := range []tea.Msg{
		panels.OpenAddServiceMsg{TaskID: "TASK-1"},
		panels.OpenRemoveServiceDialogMsg{TaskID: "TASK-1", ServiceName: "api"},
		panels.OpenSyncServiceStrategyDialogMsg{TaskID: "TASK-1", ServiceName: "api"},
		panels.PushServiceMsg{TaskID: "TASK-1", ServiceName: "api"},
		panels.StashServiceMsg{TaskID: "TASK-1", ServiceName: "api"},
		panels.OpenLazygitServiceMsg{TaskID: "TASK-1", ServiceName: "api", WorktreePath: "/tasks/TASK-1/api"},
		modal.ForgeCreateMRMsg{TaskID: "TASK-1", Title: "MR"},
		modal.ForgeMergeMRMsg{TaskID: "TASK-1", ServiceName: "api"},
	} {
		updated, cmd := m.Update(msg)
		m = updated.(Model)
		if cmd != nil || m.modal != nil || m.releaseCleanupExecuting != 7 {
			t.Fatalf("%T escaped cleanup lock: cmd=%v modal=%T generation=%d", msg, cmd, m.modal, m.releaseCleanupExecuting)
		}
	}
}

func TestUpdate_PendingReleaseCleanupPlanBlocksTaskMutation(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	m.setFocus(FocusTasks)
	m.pendingReleaseCleanupPlan = &task.ReleaseCleanupPlan{}
	m.pendingReleaseCleanupPreview = testCleanupPreview(task.DefaultReleaseCleanupSelection())

	updated, cmd := m.Update(panels.OpenRemoveDialogMsg{TaskID: "TASK-1"})
	m = updated.(Model)
	if cmd != nil || m.modal != nil || m.pendingReleaseCleanupPlan == nil {
		t.Fatal("pending cleanup approval allowed task mutation")
	}
}

func TestUpdate_StaleCleanupChecklistAfterReleaseRefreshCannotReplan(t *testing.T) {
	selection := task.DefaultReleaseCleanupSelection()
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}})
	m.modal = modal.NewReleaseCleanupChecklistModal(testCleanupPreview(selection))

	updated, _ := m.Update(ReleasesLoadedMsg{Releases: []domain.Release{{ID: "rel-2", Status: domain.ReleaseStatusReleased}}})
	m = updated.(Model)
	if m.modal != nil || m.releaseCleanupRequest != nil || m.opRunning {
		t.Fatalf("refresh retained stale checklist: modal=%T request=%v running=%v", m.modal, m.releaseCleanupRequest != nil, m.opRunning)
	}
	updated, cmd := m.Update(modal.SubmitReleaseCleanupMsg{ReleaseID: "rel-1", Selection: selection})
	m = updated.(Model)
	if cmd != nil || m.releaseCleanupRequest != nil || m.opRunning {
		t.Fatal("stale checklist submission started replan")
	}
}

func TestUpdate_ReleaseRefreshDriftClearsActiveCleanupPlanningRequest(t *testing.T) {
	selection := task.DefaultReleaseCleanupSelection()
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}})
	m.releaseCleanupGeneration = 2
	m.releaseCleanupRequest = &releaseCleanupRequest{generation: 2, releaseID: "rel-1", selection: selection}
	m.opRunning = true

	updated, _ := m.Update(ReleasesLoadedMsg{Releases: []domain.Release{{ID: "rel-2", Status: domain.ReleaseStatusReleased}}})
	m = updated.(Model)
	if m.releaseCleanupRequest != nil || m.opRunning || m.releaseCleanupGeneration == 2 {
		t.Fatalf("refresh retained planning request: request=%v running=%v generation=%d", m.releaseCleanupRequest != nil, m.opRunning, m.releaseCleanupGeneration)
	}
}

func TestUpdate_ReleaseCleanupPlanReadyRejectsZeroPlan(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 120, 40)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}})
	selection := task.DefaultReleaseCleanupSelection()
	m.releaseCleanupGeneration = 2
	m.releaseCleanupRequest = &releaseCleanupRequest{generation: 2, releaseID: "rel-1", selection: selection}

	updated, _ := m.Update(ReleaseCleanupPlanReadyMsg{Generation: 2})
	m = updated.(Model)
	if m.pendingReleaseCleanupPlan != nil {
		t.Fatal("zero plan stored as approval")
	}
	if m.modal != nil {
		t.Fatalf("zero plan opened %T", m.modal)
	}
}

func TestUpdate_ReleaseCleanupRemoteConfirmRejectsMismatchedQueuedRelease(t *testing.T) {
	for _, tc := range []struct {
		name    string
		current *cleanupQueueItem
	}{
		{name: "nil current item", current: nil},
		{name: "task kind current item", current: &cleanupQueueItem{kind: modal.CleanupKindTask, id: "rel-1"}},
		{name: "different release current item", current: &cleanupQueueItem{kind: modal.CleanupKindRelease, id: "rel-2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr := &mockManager{}
			m := sendWindowSize(newTestModel(t, mgr), 120, 40)
			m.setFocus(FocusReleases)
			m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}})
			selection := task.DefaultReleaseCleanupSelection()
			selection.DeleteRemoteTaskBranches = true
			m.releaseCleanupGeneration = 6
			m.pendingReleaseCleanupPlan = &task.ReleaseCleanupPlan{}
			m.pendingReleaseCleanupPreview = task.ReleaseCleanupPreview{ReleaseID: "rel-1", Selection: selection}
			m.modal = modal.NewReleaseCleanupRemoteConfirmModal(m.pendingReleaseCleanupPreview, 6)
			m.cleanupQueueCurrent = tc.current

			updated, cmd := m.Update(modal.ConfirmRemoteReleaseCleanupMsg{ReleaseID: "rel-1", Generation: 6})
			m = updated.(Model)
			if cmd != nil || m.releaseCleanupExecuting != 0 || mgr.cleanupExecuteCalls != 0 {
				t.Fatal("remote confirmation without matching queued release executed")
			}
		})
	}
}

func TestUpdate_ReleaseCleanupDoneRejectsMismatchedResultID(t *testing.T) {
	for _, tc := range []struct {
		name     string
		resultID string
	}{
		{name: "different release", resultID: "rel-2"},
		{name: "empty release", resultID: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr := &mockManager{cleanupExecuteResult: task.ReleaseCleanupResult{ReleaseID: "rel-1"}}
			m := sendWindowSize(newTestModel(t, mgr), 120, 40)
			m.setFocus(FocusReleases)
			m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}})
			selection := task.DefaultReleaseCleanupSelection()
			m.releaseCleanupGeneration = 5
			m.pendingReleaseCleanupPlan = &task.ReleaseCleanupPlan{}
			m.pendingReleaseCleanupPreview = testCleanupPreview(selection)
			m.modal = modal.NewReleaseCleanupConfirmModal(m.pendingReleaseCleanupPreview, 5)
			m.cleanupQueueCurrent = &cleanupQueueItem{kind: modal.CleanupKindRelease, id: "rel-1"}

			updated, cmd := m.Update(modal.ConfirmReleaseCleanupMsg{ReleaseID: "rel-1", Generation: 5})
			m = updated.(Model)
			if cmd == nil || m.releaseCleanupExecuting == 0 {
				t.Fatal("confirmation did not start execution")
			}

			updated, cmd = m.Update(ReleaseCleanupDoneMsg{Generation: 5, Result: task.ReleaseCleanupResult{ReleaseID: tc.resultID}})
			m = updated.(Model)
			if cmd != nil {
				t.Fatal("mismatched completion produced a command")
			}
			if m.releaseCleanupExecuting == 0 {
				t.Fatal("mismatched completion accepted")
			}
			if strings.Contains(m.outputPanel.View(), "Release cleanup done") {
				t.Fatalf("mismatched completion reported done: %s", m.outputPanel.View())
			}
		})
	}
}

func TestUpdate_ReleaseCleanupConfirmRejectsMismatchedQueuedRelease(t *testing.T) {
	for _, tc := range []struct {
		name    string
		current *cleanupQueueItem
	}{
		{name: "nil current item", current: nil},
		{name: "task kind current item", current: &cleanupQueueItem{kind: modal.CleanupKindTask, id: "rel-1"}},
		{name: "different release current item", current: &cleanupQueueItem{kind: modal.CleanupKindRelease, id: "rel-2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr := &mockManager{}
			m := sendWindowSize(newTestModel(t, mgr), 120, 40)
			m.setFocus(FocusReleases)
			m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}})
			selection := task.DefaultReleaseCleanupSelection()
			m.releaseCleanupGeneration = 6
			m.pendingReleaseCleanupPlan = &task.ReleaseCleanupPlan{}
			m.pendingReleaseCleanupPreview = task.ReleaseCleanupPreview{ReleaseID: "rel-1", Selection: selection}
			m.modal = modal.NewReleaseCleanupConfirmModal(m.pendingReleaseCleanupPreview, 6)
			m.cleanupQueueCurrent = tc.current

			updated, cmd := m.Update(modal.ConfirmReleaseCleanupMsg{ReleaseID: "rel-1", Generation: 6})
			m = updated.(Model)
			if cmd != nil || m.releaseCleanupExecuting != 0 || mgr.cleanupExecuteCalls != 0 {
				t.Fatal("confirmation without matching queued release executed")
			}
		})
	}
}
