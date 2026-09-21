package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
)

func taskMergeTestPreview() task.ReleasePreview {
	return task.ReleasePreview{
		TaskMergeDuringPrepare: true,
		Rows: []task.ReleasePreviewRow{{
			ServiceName: "api", Version: "1.2.3", ReleaseBranch: "release/1.2.3", Tag: "v1.2.3",
		}},
	}
}

func taskMergeReadyPlan() task.ReleaseTaskMergePlan {
	return task.ReleaseTaskMergePlan{Rows: []task.ReleaseTaskMergeRow{{
		ServiceName: "api", TaskID: "ZA-1", Branch: "feature/ZA-1",
		MRNumber: 12, HeadSHA: "abc12345", TargetBranch: "develop", TargetSHA: "def67890",
		Status: "pending", Ready: true,
	}}}
}

func submitTaskMergeRelease(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(modal.SubmitCreateReleaseMsg{
		Title:    "August release",
		TaskIDs:  []string{"ZA-1"},
		Versions: map[string]string{"api": "1.2.3"},
	})
	return updated.(Model), cmd
}

func findMsgInBatch(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("command is nil")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return msg
	}
	for _, c := range batch {
		if c == nil {
			continue
		}
		if result := c(); result != nil {
			switch result.(type) {
			case ReleaseTaskMergePlanReadyMsg, ReleaseTaskMergeRetryPlanReadyMsg:
				return result
			}
		}
	}
	t.Fatalf("no plan-ready message in batch %T", msg)
	return nil
}

func TestUpdate_SubmitCreateRelease_TaskMergeTiming_LaunchesPlanningBeforeModal(t *testing.T) {
	mgr := &mockManager{releasePreview: taskMergeTestPreview()}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)

	m, cmd := submitTaskMergeRelease(t, m)

	if m.modal != nil {
		t.Fatalf("modal opened before planning finished: %T", m.modal)
	}
	if !m.opRunning {
		t.Fatal("planning must set opRunning")
	}
	if m.pendingReleaseSubmit == nil {
		t.Fatal("pending submit must be stored during planning")
	}
	if !strings.Contains(m.outputPanel.View(), "Planning task MR merges") {
		t.Fatalf("output missing planning line: %q", m.outputPanel.View())
	}

	ready := findMsgInBatch(t, cmd).(ReleaseTaskMergePlanReadyMsg)
	if mgr.planTaskMergeCalls != 1 {
		t.Fatalf("PlanReleaseTaskMerges calls = %d, want 1", mgr.planTaskMergeCalls)
	}
	if mgr.planTaskMergeParams.Title != "August release" || mgr.planTaskMergeParams.ServiceVersions["api"] != "1.2.3" {
		t.Fatalf("plan params = %+v", mgr.planTaskMergeParams)
	}
	if ready.Generation == 0 {
		t.Fatal("generation must be non-zero")
	}
}

func TestUpdate_SubmitCreateRelease_LegacyTiming_OpensModalWithoutPlanning(t *testing.T) {
	mgr := &mockManager{releasePreview: task.ReleasePreview{Rows: []task.ReleasePreviewRow{{ServiceName: "api", Version: "1.2.3"}}}}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)

	m, cmd := submitTaskMergeRelease(t, m)

	if cmd != nil {
		t.Fatal("legacy path must not return a command")
	}
	if _, ok := m.modal.(*modal.ReleaseExecuteConfirmDialog); !ok {
		t.Fatalf("modal = %T, want ReleaseExecuteConfirmDialog", m.modal)
	}
	if mgr.planTaskMergeCalls != 0 {
		t.Fatalf("legacy path planned task merges: %d", mgr.planTaskMergeCalls)
	}
}

func TestUpdate_ReleaseTaskMergePlanReady_OpensConfirmWithRows(t *testing.T) {
	mgr := &mockManager{releasePreview: taskMergeTestPreview()}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m, _ = submitTaskMergeRelease(t, m)
	generation := m.releaseTaskMerge.generation

	updated, _ := m.Update(ReleaseTaskMergePlanReadyMsg{Generation: generation, Plan: taskMergeReadyPlan()})
	m = updated.(Model)

	dlg, ok := m.modal.(*modal.ReleaseExecuteConfirmDialog)
	if !ok {
		t.Fatalf("modal = %T, want ReleaseExecuteConfirmDialog", m.modal)
	}
	if dlg.TaskMergeGeneration() != generation {
		t.Fatalf("dialog generation = %d, want %d", dlg.TaskMergeGeneration(), generation)
	}
	if m.opRunning {
		t.Fatal("planning done must clear opRunning")
	}
	if view := m.modal.View(); !strings.Contains(view, "abc12345") || !strings.Contains(view, "[Enter/y] execute") {
		t.Fatalf("confirm view missing task MR rows or execute hint: %s", view)
	}
}

func TestUpdate_ReleaseTaskMergePlanReady_StaleGenerationIgnored(t *testing.T) {
	mgr := &mockManager{releasePreview: taskMergeTestPreview()}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m, _ = submitTaskMergeRelease(t, m)
	generation := m.releaseTaskMerge.generation

	updated, _ := m.Update(ReleaseTaskMergePlanReadyMsg{Generation: generation + 1, Plan: taskMergeReadyPlan()})
	m = updated.(Model)
	if m.modal != nil {
		t.Fatalf("stale plan result opened modal %T", m.modal)
	}
	if !m.opRunning {
		t.Fatal("stale result must not clear the in-flight operation")
	}

	updated, _ = m.Update(ReleaseTaskMergePlanReadyMsg{Generation: generation, Plan: taskMergeReadyPlan()})
	m = updated.(Model)
	if _, ok := m.modal.(*modal.ReleaseExecuteConfirmDialog); !ok {
		t.Fatalf("current plan result must open modal, got %T", m.modal)
	}
}

func TestUpdate_ReleaseTaskMergePlanReady_ErrorLeavesNoModal(t *testing.T) {
	mgr := &mockManager{releasePreview: taskMergeTestPreview()}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m, _ = submitTaskMergeRelease(t, m)
	generation := m.releaseTaskMerge.generation

	updated, _ := m.Update(ReleaseTaskMergePlanReadyMsg{Generation: generation, Err: errors.New("forge down")})
	m = updated.(Model)

	if m.modal != nil {
		t.Fatalf("error result opened modal %T", m.modal)
	}
	if m.opRunning || m.pendingReleaseSubmit != nil || m.releaseTaskMerge != nil {
		t.Fatalf("error result must reset state: op=%v pending=%v req=%v", m.opRunning, m.pendingReleaseSubmit != nil, m.releaseTaskMerge != nil)
	}
	if !strings.Contains(m.outputPanel.View(), "forge down") {
		t.Fatalf("output missing error: %q", m.outputPanel.View())
	}
}

func TestUpdate_ConfirmReleaseExecute_TaskMergePlanPassesExactPlan(t *testing.T) {
	mgr := &mockManager{releasePreview: taskMergeTestPreview(), createReleaseResult: domain.Release{ID: "rel-1"}}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m, _ = submitTaskMergeRelease(t, m)
	generation := m.releaseTaskMerge.generation

	updated, _ := m.Update(ReleaseTaskMergePlanReadyMsg{Generation: generation, Plan: taskMergeReadyPlan()})
	m = updated.(Model)
	wantPlan := m.releaseTaskMerge.plan

	updated, cmd := m.Update(modal.ConfirmReleaseExecuteMsg{
		Title: "August release", TaskIDs: []string{"ZA-1"}, Versions: map[string]string{"api": "1.2.3"},
		Generation: generation,
	})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("confirm must return create command")
	}
	if m.modal != nil || m.pendingReleaseSubmit != nil || m.releaseTaskMerge != nil {
		t.Fatal("confirm must clear modal and pending state")
	}
	runBatchCommands(cmd())

	mgr.mu.Lock()
	calls := mgr.createReleaseCalls
	params := mgr.createReleaseParams
	mgr.mu.Unlock()
	if calls != 1 {
		t.Fatalf("CreateRelease calls = %d, want 1", calls)
	}
	if params.ConfirmedTaskMergePlan != wantPlan {
		t.Fatalf("ConfirmedTaskMergePlan = %p, want exact plan %p", params.ConfirmedTaskMergePlan, wantPlan)
	}

	updated, cmd = m.Update(modal.ConfirmReleaseExecuteMsg{
		Title: "August release", TaskIDs: []string{"ZA-1"}, Versions: map[string]string{"api": "1.2.3"},
		Generation: generation,
	})
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("repeated confirm must not return a command")
	}
	mgr.mu.Lock()
	calls = mgr.createReleaseCalls
	mgr.mu.Unlock()
	if calls != 1 {
		t.Fatalf("repeated confirm executed: calls = %d", calls)
	}
}

func TestUpdate_ConfirmReleaseExecute_StaleTaskMergeGeneration_DoesNotExecute(t *testing.T) {
	mgr := &mockManager{releasePreview: taskMergeTestPreview()}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m, _ = submitTaskMergeRelease(t, m)
	generation := m.releaseTaskMerge.generation

	updated, _ := m.Update(ReleaseTaskMergePlanReadyMsg{Generation: generation, Plan: taskMergeReadyPlan()})
	m = updated.(Model)

	updated, cmd := m.Update(modal.ConfirmReleaseExecuteMsg{
		Title: "August release", TaskIDs: []string{"ZA-1"}, Versions: map[string]string{"api": "1.2.3"},
		Generation: generation + 1,
	})
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("stale generation confirm must not return a command")
	}
	if mgr.createReleaseCalls != 0 {
		t.Fatalf("stale confirm executed: calls = %d", mgr.createReleaseCalls)
	}
}

func TestUpdate_CloseModalMsg_TaskMergeConfirm_CancelMutatesNothing(t *testing.T) {
	mgr := &mockManager{releasePreview: taskMergeTestPreview()}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m, _ = submitTaskMergeRelease(t, m)
	generation := m.releaseTaskMerge.generation

	updated, _ := m.Update(ReleaseTaskMergePlanReadyMsg{Generation: generation, Plan: taskMergeReadyPlan()})
	m = updated.(Model)

	updated, _ = m.Update(modal.CloseModalMsg{})
	m = updated.(Model)
	if m.pendingReleaseSubmit != nil || m.releaseTaskMerge != nil {
		t.Fatal("cancel must clear pending submit and plan")
	}

	updated, cmd := m.Update(modal.ConfirmReleaseExecuteMsg{
		Title: "August release", TaskIDs: []string{"ZA-1"}, Versions: map[string]string{"api": "1.2.3"},
		Generation: generation,
	})
	m = updated.(Model)
	if cmd != nil || mgr.createReleaseCalls != 0 {
		t.Fatalf("confirm after cancel executed: cmd=%v calls=%d", cmd != nil, mgr.createReleaseCalls)
	}
}

func TestUpdate_FocusChangeDuringPlanning_InvalidatesPlanResult(t *testing.T) {
	mgr := &mockManager{releasePreview: taskMergeTestPreview()}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m, _ = submitTaskMergeRelease(t, m)
	generation := m.releaseTaskMerge.generation

	updated, _ := m.Update(sendKey("tab"))
	m = updated.(Model)

	updated, _ = m.Update(ReleaseTaskMergePlanReadyMsg{Generation: generation, Plan: taskMergeReadyPlan()})
	m = updated.(Model)
	if m.modal != nil {
		t.Fatalf("plan result after focus change opened modal %T", m.modal)
	}
	if m.opRunning {
		t.Fatal("invalidated planning must clear opRunning")
	}
}

func TestUpdate_StartReleaseRetry_TaskMergeBlocked_LaunchesRetryPlanning(t *testing.T) {
	mgr := &mockManager{}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusTaskMergeBlocked}})

	updated, cmd := m.Update(sendKey("R"))
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("R on task_merge_blocked must start retry planning")
	}
	if !m.opRunning || m.releaseTaskMerge == nil || m.releaseTaskMerge.releaseID != "rel-1" {
		t.Fatalf("retry planning state wrong: op=%v req=%+v", m.opRunning, m.releaseTaskMerge)
	}
	ready := findMsgInBatch(t, cmd).(ReleaseTaskMergeRetryPlanReadyMsg)
	if mgr.planTaskMergeRetryCalls != 1 || mgr.planTaskMergeRetryReleaseID != "rel-1" {
		t.Fatalf("PlanReleaseTaskMergeRetry calls=%d id=%q", mgr.planTaskMergeRetryCalls, mgr.planTaskMergeRetryReleaseID)
	}
	if ready.ReleaseID != "rel-1" {
		t.Fatalf("ready release = %q", ready.ReleaseID)
	}
}

func TestUpdate_StartReleaseRetry_FailedRecoverable_UsesLegacyRetry(t *testing.T) {
	mgr := &mockManager{}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{
		ID: "rel-1", Status: domain.ReleaseStatusFailed,
		Error: &domain.ReleaseError{Message: "tag push failed", Recoverable: true},
	}})

	updated, cmd := m.Update(sendKey("R"))
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("R on failed recoverable must start legacy retry")
	}
	runBatchCommands(cmd())
	if mgr.retryReleaseCalls != 1 {
		t.Fatalf("legacy RetryRelease calls = %d, want 1", mgr.retryReleaseCalls)
	}
	if mgr.planTaskMergeRetryCalls != 0 {
		t.Fatalf("legacy retry must not plan task merges: %d", mgr.planTaskMergeRetryCalls)
	}
}

func TestUpdate_StartReleaseRetry_AwaitingTaskMerge_LaunchesRetryPlanning(t *testing.T) {
	mgr := &mockManager{}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusAwaitingTaskMerge}})

	updated, cmd := m.Update(sendKey("R"))
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("R on awaiting_task_merge must start retry planning")
	}
	if !m.opRunning || m.releaseTaskMerge == nil || m.releaseTaskMerge.releaseID != "rel-1" {
		t.Fatalf("retry planning state wrong: op=%v req=%+v", m.opRunning, m.releaseTaskMerge)
	}
	ready := findMsgInBatch(t, cmd).(ReleaseTaskMergeRetryPlanReadyMsg)
	if mgr.planTaskMergeRetryCalls != 1 || mgr.planTaskMergeRetryReleaseID != "rel-1" {
		t.Fatalf("PlanReleaseTaskMergeRetry calls=%d id=%q", mgr.planTaskMergeRetryCalls, mgr.planTaskMergeRetryReleaseID)
	}
	if ready.ReleaseID != "rel-1" {
		t.Fatalf("ready release = %q", ready.ReleaseID)
	}
}

func TestUpdate_RetryTaskMerge_Awaiting_ConfirmExecutesAndRefreshes(t *testing.T) {
	mgr := &mockManager{retryTaskMergeResult: domain.Release{ID: "rel-1", Status: domain.ReleaseStatusPrepared}}
	mgr.planTaskMergeRetryResult = taskMergeReadyPlan()
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusAwaitingTaskMerge}})

	updated, cmd := m.Update(sendKey("R"))
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("R on awaiting_task_merge must start retry planning")
	}
	generation := m.releaseTaskMerge.generation

	updated, _ = m.Update(findMsgInBatch(t, cmd))
	m = updated.(Model)
	if _, ok := m.modal.(*modal.ReleaseTaskMergeRetryConfirmDialog); !ok {
		t.Fatalf("modal = %T, want ReleaseTaskMergeRetryConfirmDialog", m.modal)
	}

	updated, cmd = m.Update(modal.ConfirmReleaseTaskMergeRetryMsg{ReleaseID: "rel-1", Generation: generation})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("retry confirm must return a command")
	}
	if m.modal != nil || m.releaseTaskMerge != nil {
		t.Fatal("retry confirm must clear modal and pending plan")
	}
	runBatchCommands(cmd())
	mgr.mu.Lock()
	calls := mgr.retryTaskMergeCalls
	gotID := mgr.retryTaskMergeReleaseID
	mgr.mu.Unlock()
	if calls != 1 || gotID != "rel-1" {
		t.Fatalf("RetryReleaseTaskMerges calls=%d id=%q, want 1/rel-1", calls, gotID)
	}
}

func TestUpdate_RetryTaskMerge_ConfirmExecutesAndRefreshes(t *testing.T) {
	mgr := &mockManager{retryTaskMergeResult: domain.Release{ID: "rel-1", Status: domain.ReleaseStatusPrepared}}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusTaskMergePartial}})

	updated, cmd := m.Update(sendKey("R"))
	m = updated.(Model)
	generation := m.releaseTaskMerge.generation

	updated, _ = m.Update(findMsgInBatch(t, cmd))
	m = updated.(Model)
	dlg, ok := m.modal.(*modal.ReleaseTaskMergeRetryConfirmDialog)
	if !ok {
		t.Fatalf("modal = %T, want ReleaseTaskMergeRetryConfirmDialog", m.modal)
	}
	if m.opRunning {
		t.Fatal("retry plan ready must clear opRunning")
	}
	wantPlan := m.releaseTaskMerge.plan

	updated, cmd = m.Update(modal.ConfirmReleaseTaskMergeRetryMsg{ReleaseID: "rel-1", Generation: generation})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("retry confirm must return a command")
	}
	if m.modal != nil || m.releaseTaskMerge != nil {
		t.Fatal("retry confirm must clear modal and pending plan")
	}
	var done ReleaseActionDoneMsg
	found := false
	var walk func(c tea.Cmd)
	walk = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, sub := range msg {
				walk(sub)
			}
		case ReleaseActionDoneMsg:
			done, found = msg, true
		}
	}
	walk(cmd)
	if !found {
		t.Fatal("retry command did not produce ReleaseActionDoneMsg")
	}
	if done.Err != nil || done.Release.ID != "rel-1" {
		t.Fatalf("done = %#v", done)
	}
	mgr.mu.Lock()
	calls := mgr.retryTaskMergeCalls
	gotPlan := mgr.retryTaskMergePlan
	gotID := mgr.retryTaskMergeReleaseID
	mgr.mu.Unlock()
	if calls != 1 || gotID != "rel-1" || gotPlan != wantPlan {
		t.Fatalf("RetryReleaseTaskMerges calls=%d id=%q plan=%p, want 1/rel-1/%p", calls, gotID, gotPlan, wantPlan)
	}

	updated, cmd = m.Update(done)
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("retry done must refresh releases")
	}
	_ = cmd()
	if mgr.listReleasesCalls != 1 {
		t.Fatalf("retry done must refresh releases once, got %d", mgr.listReleasesCalls)
	}
	_ = dlg
}

func TestUpdate_RetryTaskMerge_PartialErrorRefreshesReleases(t *testing.T) {
	mgr := &mockManager{}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m.opRunning = true

	updated, cmd := m.Update(ReleaseActionDoneMsg{Action: "retry", Release: domain.Release{ID: "rel-1", Status: domain.ReleaseStatusTaskMergePartial}, Err: errors.New("worker: merge failed")})
	m = updated.(Model)
	if m.opRunning {
		t.Fatal("retry done must clear opRunning")
	}
	if !strings.Contains(m.outputPanel.View(), "Retry release failed: worker: merge failed") {
		t.Fatalf("output missing failure: %q", m.outputPanel.View())
	}
	if cmd == nil {
		t.Fatal("partial failure must refresh releases")
	}
	_ = cmd()
	if mgr.listReleasesCalls != 1 {
		t.Fatalf("partial failure releases refresh = %d, want 1", mgr.listReleasesCalls)
	}
}

func TestUpdate_ConfirmReleaseTaskMergeRetry_StaleGeneration_DoesNotExecute(t *testing.T) {
	mgr := &mockManager{}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusTaskMergeBlocked}})

	updated, cmd := m.Update(sendKey("R"))
	m = updated.(Model)
	generation := m.releaseTaskMerge.generation

	updated, _ = m.Update(findMsgInBatch(t, cmd))
	m = updated.(Model)

	updated, cmd = m.Update(modal.ConfirmReleaseTaskMergeRetryMsg{ReleaseID: "rel-1", Generation: generation + 1})
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("stale retry confirm must not return a command")
	}
	if mgr.retryTaskMergeCalls != 0 {
		t.Fatalf("stale retry confirm executed: calls = %d", mgr.retryTaskMergeCalls)
	}
}

func TestUpdate_ReleaseTaskMergeRetryPlanReady_DriftedSelectionIgnored(t *testing.T) {
	mgr := &mockManager{}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusTaskMergeBlocked}, {ID: "rel-2", Status: domain.ReleaseStatusTaskMergeBlocked}})

	updated, cmd := m.Update(sendKey("R"))
	m = updated.(Model)
	generation := m.releaseTaskMerge.generation
	_ = cmd()

	m.releasesPanel, _ = m.releasesPanel.Update(sendKey("j"))

	updated, _ = m.Update(ReleaseTaskMergeRetryPlanReadyMsg{ReleaseID: "rel-1", Generation: generation, Plan: taskMergeReadyPlan()})
	m = updated.(Model)
	if m.modal != nil {
		t.Fatalf("drifted retry plan opened modal %T", m.modal)
	}
}
