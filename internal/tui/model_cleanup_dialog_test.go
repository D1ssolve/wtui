package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	"github.com/D1ssolve/wtui/internal/tui/panels"
)

func readyCleanupTaskCandidate(id string, services int) modal.CleanupCandidate {
	return modal.CleanupCandidate{Kind: modal.CleanupKindTask, ID: id, Ready: true, Services: services, Resources: services}
}

func readyCleanupReleaseCandidate(id string, services int) modal.CleanupCandidate {
	return modal.CleanupCandidate{Kind: modal.CleanupKindRelease, ID: id, Ready: true, Services: services, Resources: services}
}

func extractCleanupScanReady(msg tea.Msg) (CleanupScanReadyMsg, bool) {
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		ready, ok := msg.(CleanupScanReadyMsg)
		return ready, ok
	}
	for _, cmd := range batch {
		if cmd == nil {
			continue
		}
		if ready, ok := cmd().(CleanupScanReadyMsg); ok {
			return ready, true
		}
	}
	return CleanupScanReadyMsg{}, false
}

// openCleanupDialog drives D and feeds the resulting scan back, returning the
// model with the candidates dialog open.
func openCleanupDialog(t *testing.T, m Model, candidates []modal.CleanupCandidate) Model {
	t.Helper()
	updated, keyCmd := m.Update(sendKey("D"))
	m = updated.(Model)
	if keyCmd == nil {
		t.Fatal("expected key D to emit OpenCleanupDialogMsg command")
	}
	openMsg, ok := keyCmd().(panels.OpenCleanupDialogMsg)
	if !ok {
		t.Fatalf("expected OpenCleanupDialogMsg, got %T", keyCmd())
	}
	updated, scanCmd := m.Update(openMsg)
	m = updated.(Model)
	if scanCmd == nil || !m.opRunning {
		t.Fatalf("cleanup scan did not start: cmd nil=%v running=%v", scanCmd == nil, m.opRunning)
	}
	ready := CleanupScanReadyMsg{Generation: m.cleanupScanGeneration, Candidates: candidates}
	updated, _ = m.Update(ready)
	m = updated.(Model)
	if _, ok := m.modal.(*modal.CleanupCandidatesModal); !ok {
		t.Fatalf("candidates dialog not open: %T", m.modal)
	}
	return m
}

func TestCleanupDialog_RefreshDoesNotScanCleanup(t *testing.T) {
	mgr := &mockManager{listTasksResult: []domain.Task{{ID: "T-1"}, {ID: "T-2"}}}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)

	updated, cmd := m.Update(sendKey("r"))
	m = updated.(Model)
	runBatchCommands(cmd())
	updated, cmd = m.Update(TasksLoadedMsg{Tasks: mgr.listTasksResult})
	m = updated.(Model)
	if cmd != nil {
		runBatchCommands(cmd())
	}
	if mgr.taskCleanupPlanCalls != 0 || mgr.taskCleanupExecCalls != 0 || mgr.cleanupExecuteCalls != 0 {
		t.Fatalf("refresh triggered cleanup: plan=%d exec=%d releaseExec=%d",
			mgr.taskCleanupPlanCalls, mgr.taskCleanupExecCalls, mgr.cleanupExecuteCalls)
	}
}

func TestCleanupDialog_LifecycleCompletionsDoNotTriggerCleanup(t *testing.T) {
	t.Run("close", func(t *testing.T) {
		mgr := &mockManager{}
		m := sendWindowSize(newTestModel(t, mgr), 120, 40)
		updated, cmd := m.Update(CloseTaskFinishedMsg{Result: task.CloseTaskResult{TaskID: "T-1", Success: true}})
		m = updated.(Model)
		runBatchCommands(cmd())
		if mgr.taskCleanupPlanCalls != 0 || mgr.taskCleanupExecCalls != 0 {
			t.Fatalf("close triggered cleanup: plan=%d exec=%d", mgr.taskCleanupPlanCalls, mgr.taskCleanupExecCalls)
		}
		if _, ok := m.modal.(*modal.CloseTaskSummaryModal); !ok {
			t.Fatalf("summary modal = %T", m.modal)
		}
	})

	t.Run("task merge", func(t *testing.T) {
		mgr := &mockManager{}
		m := sendWindowSize(newTestModel(t, mgr), 120, 40)
		_, cmd := m.Update(TaskMergeDoneMsg{Result: task.TaskMergeResult{TaskID: "T-1", Merged: []string{"api"}}})
		runBatchCommands(cmd())
		if mgr.taskCleanupPlanCalls != 0 || mgr.taskCleanupExecCalls != 0 {
			t.Fatalf("merge triggered cleanup: plan=%d exec=%d", mgr.taskCleanupPlanCalls, mgr.taskCleanupExecCalls)
		}
	})

	t.Run("release finalize and retry", func(t *testing.T) {
		for _, action := range []string{"finalize", "retry"} {
			mgr := &mockManager{}
			m := sendWindowSize(newTestModel(t, mgr), 120, 40)
			release := domain.Release{ID: "rel-1", Status: domain.ReleaseStatusReleased, TaskIDs: []string{"T-1", "T-2"}}
			_, cmd := m.Update(ReleaseActionDoneMsg{Action: action, Release: release})
			runBatchCommands(cmd())
			if mgr.taskCleanupPlanCalls != 0 || mgr.taskCleanupExecCalls != 0 || mgr.cleanupExecuteCalls != 0 {
				t.Fatalf("%s triggered cleanup: plan=%d exec=%d releaseExec=%d",
					action, mgr.taskCleanupPlanCalls, mgr.taskCleanupExecCalls, mgr.cleanupExecuteCalls)
			}
		}
	})
}

func TestCleanupDialog_KeyDStartsReadOnlyScan(t *testing.T) {
	mgr := &mockManager{
		listTasksResult:    []domain.Task{{ID: "T-1", Phase: "release"}},
		listReleasesResult: []domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}},
	}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m.tasksPanel.SetTasks(mgr.listTasksResult)

	updated, keyCmd := m.Update(sendKey("D"))
	m = updated.(Model)
	if keyCmd == nil {
		t.Fatal("expected key D to emit OpenCleanupDialogMsg command")
	}
	updated, cmd := m.Update(keyCmd())
	m = updated.(Model)
	if cmd == nil || !m.opRunning {
		t.Fatal("D did not start the cleanup scan")
	}
	ready, ok := extractCleanupScanReady(cmd())
	if !ok {
		t.Fatal("scan command did not produce CleanupScanReadyMsg")
	}
	if mgr.taskCleanupExecCalls != 0 || mgr.cleanupExecuteCalls != 0 {
		t.Fatalf("scan mutated resources: exec=%d releaseExec=%d",
			mgr.taskCleanupExecCalls, mgr.cleanupExecuteCalls)
	}
	if len(ready.Candidates) != 2 {
		t.Fatalf("candidates = %+v, want task and release", ready.Candidates)
	}
	if ready.Candidates[0].Kind != modal.CleanupKindTask || ready.Candidates[0].ID != "T-1" {
		t.Fatalf("release-phase task lost task identity: %+v", ready.Candidates[0])
	}
	if ready.Candidates[1].Kind != modal.CleanupKindRelease || ready.Candidates[1].ID != "rel-1" {
		t.Fatalf("release candidate = %+v", ready.Candidates[1])
	}
	if mgr.cleanupPlanSelection != (task.ReleaseCleanupSelection{RemoveRelease: true}) {
		t.Fatalf("release scan selection = %+v, want release-only", mgr.cleanupPlanSelection)
	}

	updated, _ = m.Update(ready)
	m = updated.(Model)
	if _, ok := m.modal.(*modal.CleanupCandidatesModal); !ok {
		t.Fatalf("scan ready did not open dialog: %T", m.modal)
	}
}

func TestCleanupDialog_ScanErrorIsVisible(t *testing.T) {
	mgr := &mockManager{listTasksErr: errors.New("disk gone")}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)

	updated, keyCmd := m.Update(sendKey("D"))
	m = updated.(Model)
	if keyCmd == nil {
		t.Fatal("expected key D to emit OpenCleanupDialogMsg command")
	}
	updated, cmd := m.Update(keyCmd())
	m = updated.(Model)
	ready, ok := extractCleanupScanReady(cmd())
	if !ok {
		t.Fatal("scan command did not produce CleanupScanReadyMsg")
	}
	if ready.Err == nil {
		t.Fatal("list failure not reported as scan error")
	}
	updated, _ = m.Update(ready)
	m = updated.(Model)
	if m.modal != nil {
		t.Fatalf("scan error opened modal %T", m.modal)
	}
	if !strings.Contains(m.outputPanel.View(), "Cleanup scan failed") {
		t.Fatalf("scan error missing: %s", m.outputPanel.View())
	}
}

func TestCleanupDialog_StaleScanReadyIgnored(t *testing.T) {
	mgr := &mockManager{}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)

	updated, keyCmd := m.Update(sendKey("D"))
	m = updated.(Model)
	if keyCmd == nil {
		t.Fatal("expected key D to emit OpenCleanupDialogMsg command")
	}
	updated, cmd := m.Update(keyCmd())
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("scan did not start")
	}
	updated, _ = m.Update(CleanupScanReadyMsg{Generation: m.cleanupScanGeneration + 9, Candidates: []modal.CleanupCandidate{readyCleanupTaskCandidate("T-1", 1)}})
	m = updated.(Model)
	if m.modal != nil {
		t.Fatalf("stale scan opened modal %T", m.modal)
	}
}

func TestCleanupDialog_SubmitQueuesTasksBeforeReleases(t *testing.T) {
	mgr := &mockManager{}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}})
	m.setFocus(FocusTasks)
	m = openCleanupDialog(t, m, []modal.CleanupCandidate{
		readyCleanupTaskCandidate("T-1", 1),
		readyCleanupReleaseCandidate("rel-1", 1),
	})
	generation := m.cleanupScanModalGen

	updated, cmd := m.Update(modal.SubmitCleanupMsg{Generation: generation, Tasks: []string{"T-1"}, Releases: []string{"rel-1"}})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("submit did not start the cleanup queue")
	}
	if m.cleanupQueueCurrent == nil || m.cleanupQueueCurrent.kind != modal.CleanupKindTask || m.cleanupQueueCurrent.id != "T-1" {
		t.Fatalf("queue head = %+v, want task T-1", m.cleanupQueueCurrent)
	}
	if len(m.cleanupQueue) != 1 || m.cleanupQueue[0].kind != modal.CleanupKindRelease {
		t.Fatalf("queue remainder = %+v, want release rel-1", m.cleanupQueue)
	}
	runBatchCommands(cmd())
	if len(mgr.taskCleanupPlanTaskIDs) != 1 || mgr.taskCleanupPlanTaskIDs[0] != "T-1" {
		t.Fatalf("task replan = %v, want [T-1]", mgr.taskCleanupPlanTaskIDs)
	}
	if mgr.taskCleanupExecCalls != 0 || mgr.cleanupExecuteCalls != 0 {
		t.Fatal("submit executed cleanup without confirmation")
	}
}

func TestCleanupDialog_ForgedOrStaleSubmitIgnored(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  modal.SubmitCleanupMsg
	}{
		{name: "wrong generation", msg: modal.SubmitCleanupMsg{Generation: 999, Tasks: []string{"T-1"}}},
		{name: "unknown task", msg: modal.SubmitCleanupMsg{Tasks: []string{"T-9"}}},
		{name: "kind mismatch", msg: modal.SubmitCleanupMsg{Releases: []string{"T-1"}}},
		{name: "blocked candidate", msg: modal.SubmitCleanupMsg{Tasks: []string{"T-2"}}},
		{name: "empty scope", msg: modal.SubmitCleanupMsg{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr := &mockManager{}
			m := sendWindowSize(newTestModel(t, mgr), 120, 40)
			m = openCleanupDialog(t, m, []modal.CleanupCandidate{
				readyCleanupTaskCandidate("T-1", 1),
				{Kind: modal.CleanupKindTask, ID: "T-2", Reason: "dirty"},
			})
			if tc.msg.Generation == 0 && tc.name != "wrong generation" {
				tc.msg.Generation = m.cleanupScanModalGen
			}
			updated, cmd := m.Update(tc.msg)
			m = updated.(Model)
			if cmd != nil {
				t.Fatal("forged submit emitted command")
			}
			if m.cleanupQueueActive() {
				t.Fatal("forged submit started the queue")
			}
			if _, ok := m.modal.(*modal.CleanupCandidatesModal); !ok {
				t.Fatalf("dialog lost: %T", m.modal)
			}
		})
	}
}

// queueTaskCleanup drives the queue to the point where a task replan is in
// flight for T-1.
func queueTaskCleanup(t *testing.T, mgr *mockManager) Model {
	t.Helper()
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m = openCleanupDialog(t, m, []modal.CleanupCandidate{readyCleanupTaskCandidate("T-1", 1)})
	updated, _ := m.Update(modal.SubmitCleanupMsg{Generation: m.cleanupScanModalGen, Tasks: []string{"T-1"}})
	m = updated.(Model)
	if m.taskCleanupRequest == nil {
		t.Fatal("task replan not in flight")
	}
	return m
}

// stageTaskCleanupConfirm installs the pending approval and confirm modal a
// real identity-bearing plan would have produced; synthetic zero plans are
// rejected at plan-ready.
func stageTaskCleanupConfirm(m Model) Model {
	plan := task.TaskCleanupPlan{}
	m.taskCleanupRequest = nil
	m.opRunning = false
	m.pendingTaskCleanupPlan = &plan
	m.pendingTaskCleanupPreview = qualifyingTaskCleanupPreview("T-1")
	m.modal = modal.NewTaskCleanupConfirmModal(m.pendingTaskCleanupPreview, m.taskCleanupGeneration, plan.Fingerprint())
	return m
}

func TestCleanupDialog_TaskPlanReadyRejectsZeroPlan(t *testing.T) {
	mgr := &mockManager{}
	m := queueTaskCleanup(t, mgr)

	updated, _ := m.Update(TaskCleanupPlanReadyMsg{
		TaskID:              "T-1",
		Generation:          m.taskCleanupGeneration,
		OperationGeneration: m.operationGeneration,
	})
	m = updated.(Model)
	if m.pendingTaskCleanupPlan != nil {
		t.Fatal("zero plan stored as approval")
	}
	if _, ok := m.modal.(*modal.TaskCleanupConfirmModal); ok {
		t.Fatal("zero plan opened confirmation")
	}
	if mgr.taskCleanupExecCalls != 0 {
		t.Fatal("zero plan executed")
	}
}

func TestCleanupDialog_TaskPlanReadyRejectsZeroOperationGeneration(t *testing.T) {
	mgr := &mockManager{}
	m := queueTaskCleanup(t, mgr)

	updated, _ := m.Update(TaskCleanupPlanReadyMsg{
		TaskID:     "T-1",
		Generation: m.taskCleanupGeneration,
	})
	m = updated.(Model)
	if m.pendingTaskCleanupPlan != nil {
		t.Fatal("zero operation generation plan stored as approval")
	}
	if _, ok := m.modal.(*modal.TaskCleanupConfirmModal); ok {
		t.Fatal("zero operation generation opened confirmation")
	}
}

func TestCleanupDialog_TaskPlanReadyMismatchStopsQueue(t *testing.T) {
	mgr := &mockManager{}
	m := queueTaskCleanup(t, mgr)

	updated, cmd := m.Update(TaskCleanupPlanReadyMsg{
		TaskID:              "T-1",
		Generation:          m.taskCleanupGeneration,
		OperationGeneration: m.operationGeneration,
	})
	m = updated.(Model)
	if m.cleanupQueueActive() {
		t.Fatal("mismatched plan left queue active")
	}
	if m.pendingTaskCleanupPlan != nil {
		t.Fatal("mismatched plan stored as approval")
	}
	if !strings.Contains(m.outputPanel.View(), "Cleanup queue stopped") {
		t.Fatalf("stop reason missing: %s", m.outputPanel.View())
	}
	runBatchCommands(cmd())
	if mgr.listTasksCalls == 0 || mgr.listReleasesCalls == 0 {
		t.Fatal("mismatched plan did not refresh lists")
	}
}

func TestCleanupDialog_TaskPlanErrorStopsQueue(t *testing.T) {
	mgr := &mockManager{}
	m := queueTaskCleanup(t, mgr)

	updated, cmd := m.Update(TaskCleanupPlanReadyMsg{
		TaskID:              "T-1",
		Generation:          m.taskCleanupGeneration,
		OperationGeneration: m.operationGeneration,
		Err:                 errors.New("planner boom"),
	})
	m = updated.(Model)
	if mgr.taskCleanupExecCalls != 0 {
		t.Fatal("failed replan executed")
	}
	if m.cleanupQueueActive() {
		t.Fatal("failed replan left queue active")
	}
	if !strings.Contains(m.outputPanel.View(), "Plan task cleanup failed for T-1: planner boom") {
		t.Fatalf("failure reason missing: %s", m.outputPanel.View())
	}
	runBatchCommands(cmd())
	if mgr.listTasksCalls == 0 || mgr.listReleasesCalls == 0 {
		t.Fatal("failed replan did not refresh lists")
	}
}

func TestCleanupDialog_TaskConfirmExecutesOnlyExactApproval(t *testing.T) {
	t.Run("mismatched approvals do nothing", func(t *testing.T) {
		mgr := &mockManager{}
		m := queueTaskCleanup(t, mgr)
		m = stageTaskCleanupConfirm(m)
		generation := m.taskCleanupGeneration
		fingerprint := m.pendingTaskCleanupPlan.Fingerprint()
		for _, msg := range []modal.ConfirmTaskCleanupMsg{
			{TaskID: "T-1", Generation: generation + 1, Fingerprint: fingerprint},
			{TaskID: "T-1", Generation: generation, Fingerprint: [32]byte{9}},
			{TaskID: "T-2", Generation: generation, Fingerprint: fingerprint},
		} {
			updated, cmd := m.Update(msg)
			m = updated.(Model)
			if cmd != nil {
				t.Fatalf("mismatched confirmation %+v emitted command", msg)
			}
		}
		if mgr.taskCleanupExecCalls != 0 {
			t.Fatal("mismatched confirmation executed cleanup")
		}
		if m.pendingTaskCleanupPlan == nil {
			t.Fatal("approval consumed by mismatched confirmation")
		}
	})

	t.Run("exact approval executes and advances", func(t *testing.T) {
		mgr := &mockManager{}
		mgr.taskCleanupExecResult = task.TaskCleanupResult{TaskID: "T-1", Completed: []string{"remove task worktree /tasks/T-1/api"}}
		m := queueTaskCleanup(t, mgr)
		m = stageTaskCleanupConfirm(m)
		generation := m.taskCleanupGeneration
		fingerprint := m.pendingTaskCleanupPlan.Fingerprint()

		updated, cmd := m.Update(modal.ConfirmTaskCleanupMsg{TaskID: "T-1", Generation: generation, Fingerprint: fingerprint})
		m = updated.(Model)
		if cmd == nil || !m.opRunning {
			t.Fatal("confirmation did not start execution")
		}
		if m.pendingTaskCleanupPlan != nil {
			t.Fatal("approval not consumed before execution")
		}
		if m.modal != nil {
			t.Fatalf("confirmation left modal %T", m.modal)
		}
		runBatchCommands(cmd())
		if mgr.taskCleanupExecCalls != 1 {
			t.Fatalf("execute calls = %d, want 1", mgr.taskCleanupExecCalls)
		}

		updated, cmd = m.Update(TaskCleanupDoneMsg{
			TaskID:              "T-1",
			Generation:          generation,
			OperationGeneration: m.operationGeneration,
			Result:              mgr.taskCleanupExecResult,
		})
		m = updated.(Model)
		if !strings.Contains(m.outputPanel.View(), "Task cleanup done: T-1") {
			t.Fatalf("completion missing: %s", m.outputPanel.View())
		}
		if m.cleanupQueueActive() {
			t.Fatal("queue still active after last item")
		}
		runBatchCommands(cmd())
		if mgr.listTasksCalls == 0 || mgr.listReleasesCalls == 0 {
			t.Fatal("queue completion did not refresh lists")
		}
	})
}

// TestCleanupDialog_ConfirmCommandDoneGenerations drains the real command
// returned by an exact confirmation and feeds the command-produced
// TaskCleanupDoneMsg back into the model, proving the generation fields line
// up with what the model expects (no manually constructed completion).
func TestCleanupDialog_ConfirmCommandDoneGenerations(t *testing.T) {
	mgr := &mockManager{}
	mgr.taskCleanupExecResult = task.TaskCleanupResult{TaskID: "T-1", Completed: []string{"remove task worktree /tasks/T-1/api"}}
	m := queueTaskCleanup(t, mgr)
	m = stageTaskCleanupConfirm(m)
	fingerprint := m.pendingTaskCleanupPlan.Fingerprint()

	updated, cmd := m.Update(modal.ConfirmTaskCleanupMsg{TaskID: "T-1", Generation: m.taskCleanupGeneration, Fingerprint: fingerprint})
	m = updated.(Model)
	if cmd == nil || !m.opRunning {
		t.Fatal("confirmation did not start execution")
	}

	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("confirmation command = %T, want tea.BatchMsg", cmd())
	}
	var done TaskCleanupDoneMsg
	found := false
	for _, batchCmd := range batch {
		if batchCmd == nil {
			continue
		}
		msg := batchCmd()
		for {
			line, isLine := msg.(OutputLineMsg)
			if !isLine {
				break
			}
			if line.Next == nil {
				t.Fatal("status line has no continuation")
			}
			msg = line.Next()
		}
		if doneMsg, ok := msg.(TaskCleanupDoneMsg); ok {
			done = doneMsg
			found = true
		}
	}
	if !found {
		t.Fatal("execution command produced no TaskCleanupDoneMsg")
	}
	if done.Generation != m.taskCleanupGeneration {
		t.Fatalf("done Generation = %d, want taskCleanupGeneration %d", done.Generation, m.taskCleanupGeneration)
	}
	if done.OperationGeneration != m.operationGeneration {
		t.Fatalf("done OperationGeneration = %d, want operationGeneration %d", done.OperationGeneration, m.operationGeneration)
	}

	updated, doneCmd := m.Update(done)
	m = updated.(Model)
	if m.taskCleanupExecuting != 0 || m.opRunning {
		t.Fatal("real completion rejected: cleanup still running")
	}
	if !strings.Contains(m.outputPanel.View(), "Task cleanup done:") {
		t.Fatalf("completion output missing: %s", m.outputPanel.View())
	}
	if m.cleanupQueueActive() {
		t.Fatal("queue still active after real completion")
	}
	if doneCmd == nil {
		t.Fatal("queue completion did not refresh lists")
	}
}

func TestCleanupDialog_TaskFailureStopsQueueAndRefreshes(t *testing.T) {
	mgr := &mockManager{}
	m := queueTaskCleanup(t, mgr)
	m.taskCleanupExecuting = m.taskCleanupGeneration
	m.taskCleanupExecutingID = "T-1"
	m.opRunning = true

	updated, cmd := m.Update(TaskCleanupDoneMsg{
		TaskID:              "T-1",
		Generation:          m.taskCleanupGeneration,
		OperationGeneration: m.operationGeneration,
		Result:              task.TaskCleanupResult{TaskID: "T-1"},
		Err:                 errors.New("worktree busy"),
	})
	m = updated.(Model)
	if m.cleanupQueueActive() {
		t.Fatal("failure left queue active")
	}
	if !strings.Contains(m.outputPanel.View(), "Task cleanup failed for T-1: worktree busy") {
		t.Fatalf("failure missing: %s", m.outputPanel.View())
	}
	runBatchCommands(cmd())
	if mgr.listTasksCalls == 0 || mgr.listReleasesCalls == 0 {
		t.Fatal("failure did not refresh lists")
	}
}

func TestCleanupDialog_CancelStopsRemainingQueue(t *testing.T) {
	mgr := &mockManager{}
	m := queueTaskCleanup(t, mgr)
	m = stageTaskCleanupConfirm(m)

	updated, cmd := m.Update(modal.CloseModalMsg{})
	m = updated.(Model)
	if mgr.taskCleanupExecCalls != 0 {
		t.Fatal("cancel executed cleanup")
	}
	if m.cleanupQueueActive() {
		t.Fatal("cancel left queue active")
	}
	if m.pendingTaskCleanupPlan != nil {
		t.Fatal("cancel left approval pending")
	}
	if !strings.Contains(m.outputPanel.View(), "cancelled") {
		t.Fatalf("cancel note missing: %s", m.outputPanel.View())
	}
	_ = cmd
}

func TestCleanupDialog_RemoteCandidatesReportedNotPrompted(t *testing.T) {
	mgr := &mockManager{}
	m := queueTaskCleanup(t, mgr)
	m.taskCleanupExecuting = m.taskCleanupGeneration
	m.taskCleanupExecutingID = "T-1"
	m.opRunning = true

	updated, cmd := m.Update(TaskCleanupDoneMsg{
		TaskID:              "T-1",
		Generation:          m.taskCleanupGeneration,
		OperationGeneration: m.operationGeneration,
		Result: task.TaskCleanupResult{TaskID: "T-1", Remote: []task.TaskCleanupRemoteCandidate{
			{RepoPath: "/repos/api", Branch: "feature/T-1", ExpectedSHA: "0123456789abcdef"},
		}},
	})
	m = updated.(Model)
	runBatchCommands(cmd())
	if m.modal != nil {
		t.Fatalf("follow-up modal opened after local cleanup: %T", m.modal)
	}
	if m.opRunning || m.taskCleanupExecuting != 0 {
		t.Fatal("cleanup left operation running")
	}
	if !strings.Contains(m.outputPanel.View(), "retained") {
		t.Fatalf("remote retention note missing: %s", m.outputPanel.View())
	}
}

// Accepted local cleanup with remote candidates must never chain into a
// follow-up remote-deletion prompt or execution.
func TestCleanupDialog_AcceptedLocalCleanupWithRemoteCandidatesOpensNoFollowup(t *testing.T) {
	mgr := &mockManager{}
	m := queueTaskCleanup(t, mgr)
	m = stageTaskCleanupConfirm(m)
	fingerprint := m.pendingTaskCleanupPlan.Fingerprint()
	updated, cmd := m.Update(modal.ConfirmTaskCleanupMsg{TaskID: "T-1", Generation: m.taskCleanupGeneration, Fingerprint: fingerprint})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("confirmation did not start execution")
	}
	runBatchCommands(cmd())
	if mgr.taskCleanupExecCalls != 1 {
		t.Fatalf("local cleanup calls = %d, want 1", mgr.taskCleanupExecCalls)
	}

	mgr.taskCleanupExecResult = task.TaskCleanupResult{TaskID: "T-1", Remote: []task.TaskCleanupRemoteCandidate{
		{RepoPath: "/repos/api", Branch: "feature/T-1", ExpectedSHA: "0123456789abcdef"},
	}}
	updated, cmd = m.Update(TaskCleanupDoneMsg{
		TaskID:              "T-1",
		Generation:          m.taskCleanupGeneration,
		OperationGeneration: m.operationGeneration,
		Result:              mgr.taskCleanupExecResult,
	})
	m = updated.(Model)
	runBatchCommands(cmd())

	if m.modal != nil {
		t.Fatalf("follow-up remote modal opened after accepted local cleanup: %T", m.modal)
	}
	if m.opRunning || m.taskCleanupExecuting != 0 {
		t.Fatal("cleanup left operation running")
	}
	if !strings.Contains(m.outputPanel.View(), "Remote branches for T-1 retained") {
		t.Fatalf("remote retention note missing: %s", m.outputPanel.View())
	}
}

func TestCleanupDialog_TaskSuccessAdvancesToReleaseChecklist(t *testing.T) {
	mgr := &mockManager{}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}})
	m = openCleanupDialog(t, m, []modal.CleanupCandidate{
		readyCleanupTaskCandidate("T-1", 1),
		readyCleanupReleaseCandidate("rel-1", 1),
	})
	updated, cmd := m.Update(modal.SubmitCleanupMsg{Generation: m.cleanupScanModalGen, Tasks: []string{"T-1"}, Releases: []string{"rel-1"}})
	m = updated.(Model)
	runBatchCommands(cmd())

	m = stageTaskCleanupConfirm(m)
	generation := m.taskCleanupGeneration
	fingerprint := m.pendingTaskCleanupPlan.Fingerprint()
	updated, cmd = m.Update(modal.ConfirmTaskCleanupMsg{TaskID: "T-1", Generation: generation, Fingerprint: fingerprint})
	m = updated.(Model)
	runBatchCommands(cmd())

	updated, cmd = m.Update(TaskCleanupDoneMsg{
		TaskID:              "T-1",
		Generation:          generation,
		OperationGeneration: m.operationGeneration,
		Result:              task.TaskCleanupResult{TaskID: "T-1"},
	})
	m = updated.(Model)
	runBatchCommands(cmd())
	if mgr.cleanupPlanReleaseID != "rel-1" {
		t.Fatalf("release replan = %q, want rel-1", mgr.cleanupPlanReleaseID)
	}
	if mgr.cleanupPlanSelection != (task.ReleaseCleanupSelection{RemoveRelease: true}) {
		t.Fatalf("release replan selection = %+v, want release-only", mgr.cleanupPlanSelection)
	}
	if m.focus != FocusReleases {
		t.Fatalf("focus = %v, want releases for release confirmation", m.focus)
	}
	if selected := m.releasesPanel.SelectedRelease(); selected == nil || selected.ID != "rel-1" {
		t.Fatalf("release selection = %+v, want rel-1", selected)
	}

	selection := task.ReleaseCleanupSelection{RemoveRelease: true}
	m.releaseCleanupRequest = nil
	m.opRunning = false
	m.modal = modal.NewReleaseCleanupChecklistModal(testCleanupPreview(selection))
	checklist, ok := m.modal.(*modal.ReleaseCleanupChecklistModal)
	if !ok {
		t.Fatalf("release checklist not open: %T", m.modal)
	}
	if got := checklist.Selection(); got.RemoveTasks {
		t.Fatalf("associated tasks preselected: %+v", got)
	}
}

func TestCleanupDialog_StrayPlanReadyNeverExecutes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		queue bool
	}{
		{name: "no queue at all", queue: false},
		{name: "queue on different task", queue: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr := &mockManager{}
			m := sendWindowSize(newTestModel(t, mgr), 120, 40)
			currentID := "T-9"
			if tc.queue {
				m.cleanupQueueCurrent = &cleanupQueueItem{kind: modal.CleanupKindTask, id: currentID}
			}
			m = startTaskCleanupInspectionForTest(m, "T-1")
			m.taskCleanupRequest.operationGeneration = m.operationGeneration
			planReady := TaskCleanupPlanReadyMsg{
				TaskID:              "T-1",
				Generation:          m.taskCleanupGeneration,
				OperationGeneration: m.operationGeneration,
			}

			updated, cmd := m.Update(planReady)
			m = updated.(Model)
			if cmd != nil {
				runBatchCommands(cmd())
			}
			if mgr.taskCleanupExecCalls != 0 {
				t.Fatal("stray plan executed cleanup")
			}
			if m.taskCleanupExecuting != 0 {
				t.Fatal("stray plan marked cleanup executing")
			}
			if m.pendingTaskCleanupPlan != nil {
				t.Fatal("stray plan stored approval")
			}
			if _, ok := m.modal.(*modal.TaskCleanupConfirmModal); ok {
				t.Fatal("stray plan opened confirmation")
			}
			if tc.queue && (m.cleanupQueueCurrent == nil || m.cleanupQueueCurrent.id != currentID) {
				t.Fatal("stray plan disturbed the active queue")
			}
		})
	}
}

func TestCleanupDialog_ReleaseCancelStopsQueue(t *testing.T) {
	mgr := &mockManager{}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}})
	m = openCleanupDialog(t, m, []modal.CleanupCandidate{readyCleanupReleaseCandidate("rel-1", 1)})

	updated, cmd := m.Update(modal.SubmitCleanupMsg{Generation: m.cleanupScanModalGen, Releases: []string{"rel-1"}})
	m = updated.(Model)
	runBatchCommands(cmd())
	selection := task.ReleaseCleanupSelection{RemoveRelease: true}
	m.releaseCleanupRequest = nil
	m.opRunning = false
	m.modal = modal.NewReleaseCleanupChecklistModal(testCleanupPreview(selection))
	if _, ok := m.modal.(*modal.ReleaseCleanupChecklistModal); !ok {
		t.Fatalf("checklist not open: %T", m.modal)
	}

	updated, _ = m.Update(modal.CloseModalMsg{})
	m = updated.(Model)
	if m.cleanupQueueActive() {
		t.Fatal("cancel left queue active")
	}
	if mgr.cleanupExecuteCalls != 0 {
		t.Fatal("cancel executed release cleanup")
	}
}

// executingTaskCleanup drives the queue past an exact confirmation so a task
// cleanup execution for T-1 is in flight.
func executingTaskCleanup(t *testing.T, mgr *mockManager) Model {
	t.Helper()
	m := queueTaskCleanup(t, mgr)
	m = stageTaskCleanupConfirm(m)
	fingerprint := m.pendingTaskCleanupPlan.Fingerprint()
	updated, cmd := m.Update(modal.ConfirmTaskCleanupMsg{TaskID: "T-1", Generation: m.taskCleanupGeneration, Fingerprint: fingerprint})
	m = updated.(Model)
	if cmd == nil || m.taskCleanupExecuting == 0 {
		t.Fatal("confirmation did not start execution")
	}
	return m
}

func TestCleanupDialog_TaskDoneRejectsUnderBoundCompletion(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(msg *TaskCleanupDoneMsg)
	}{
		{name: "zero operation generation", mutate: func(msg *TaskCleanupDoneMsg) { msg.OperationGeneration = 0 }},
		{name: "wrong task id", mutate: func(msg *TaskCleanupDoneMsg) { msg.TaskID = "T-2" }},
		{name: "wrong result task id", mutate: func(msg *TaskCleanupDoneMsg) { msg.Result.TaskID = "T-2" }},
		{name: "empty result task id", mutate: func(msg *TaskCleanupDoneMsg) { msg.Result.TaskID = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr := &mockManager{}
			m := executingTaskCleanup(t, mgr)
			done := TaskCleanupDoneMsg{
				TaskID:              "T-1",
				Generation:          m.taskCleanupGeneration,
				OperationGeneration: m.operationGeneration,
				Result:              task.TaskCleanupResult{TaskID: "T-1"},
			}
			tc.mutate(&done)

			updated, cmd := m.Update(done)
			m = updated.(Model)
			if cmd != nil {
				t.Fatal("under-bound completion produced a command")
			}
			if m.taskCleanupExecuting == 0 {
				t.Fatal("under-bound completion accepted")
			}
			if strings.Contains(m.outputPanel.View(), "Task cleanup done") {
				t.Fatalf("under-bound completion reported done: %s", m.outputPanel.View())
			}
		})
	}
}

func TestCleanupDialog_TaskConfirmRequiresActiveConfirmModal(t *testing.T) {
	mgr := &mockManager{}
	m := queueTaskCleanup(t, mgr)
	m = stageTaskCleanupConfirm(m)
	fingerprint := m.pendingTaskCleanupPlan.Fingerprint()
	m.modal = nil

	updated, cmd := m.Update(modal.ConfirmTaskCleanupMsg{TaskID: "T-1", Generation: m.taskCleanupGeneration, Fingerprint: fingerprint})
	m = updated.(Model)
	if cmd != nil || mgr.taskCleanupExecCalls != 0 {
		t.Fatal("confirmation without active confirm modal executed")
	}
	if m.taskCleanupExecuting != 0 {
		t.Fatal("confirmation without active confirm modal marked executing")
	}
}

func TestCleanupDialog_SubmitRequiresActiveSelector(t *testing.T) {
	mgr := &mockManager{}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m = openCleanupDialog(t, m, []modal.CleanupCandidate{readyCleanupTaskCandidate("T-1", 1)})
	m.modal = nil

	updated, cmd := m.Update(modal.SubmitCleanupMsg{Generation: m.cleanupScanModalGen, Tasks: []string{"T-1"}})
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("submit without active selector produced a command")
	}
	if m.cleanupQueueActive() || m.taskCleanupRequest != nil {
		t.Fatal("submit without active selector started cleanup")
	}
}

func TestCleanupQueue_StopsWhenReleaseNotSelectable(t *testing.T) {
	for _, tc := range []struct {
		name     string
		releases []domain.Release
		want     string
	}{
		{name: "missing from panel", want: "no longer in the list"},
		{name: "no longer released", releases: []domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusFailed}}, want: "no longer released"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr := &mockManager{}
			m := sendWindowSize(newTestModel(t, mgr), 120, 40)
			m.releasesPanel.SetReleases(tc.releases)
			m = openCleanupDialog(t, m, []modal.CleanupCandidate{readyCleanupReleaseCandidate("rel-1", 1)})

			updated, _ := m.Update(modal.SubmitCleanupMsg{Generation: m.cleanupScanModalGen, Releases: []string{"rel-1"}})
			m = updated.(Model)
			if m.releaseCleanupRequest != nil {
				t.Fatal("planning started for unselectable release")
			}
			if m.cleanupQueueActive() {
				t.Fatal("queue still active after unselectable release")
			}
			if !strings.Contains(m.outputPanel.View(), tc.want) {
				t.Fatalf("stop reason %q missing: %s", tc.want, m.outputPanel.View())
			}
		})
	}
}

func TestCleanupDialog_QueuedReleasePlanErrorStopsQueue(t *testing.T) {
	mgr := &mockManager{}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}})
	m = openCleanupDialog(t, m, []modal.CleanupCandidate{readyCleanupReleaseCandidate("rel-1", 1)})

	updated, _ := m.Update(modal.SubmitCleanupMsg{Generation: m.cleanupScanModalGen, Releases: []string{"rel-1"}})
	m = updated.(Model)
	if m.releaseCleanupRequest == nil {
		t.Fatal("release replan not in flight")
	}

	updated, cmd := m.Update(ReleaseCleanupPlanReadyMsg{Generation: m.releaseCleanupGeneration, Err: errors.New("boom")})
	m = updated.(Model)
	if m.cleanupQueueActive() {
		t.Fatal("plan error left queue active")
	}
	if m.releaseCleanupRequest != nil {
		t.Fatal("plan error leaked planning request")
	}
	if !strings.Contains(m.outputPanel.View(), "Plan release cleanup failed: boom") {
		t.Fatalf("plan error missing: %s", m.outputPanel.View())
	}
	runBatchCommands(cmd())
	if mgr.listTasksCalls == 0 || mgr.listReleasesCalls == 0 {
		t.Fatal("plan error did not refresh lists")
	}
}

func TestUpdate_KeydStartsSelectedTaskCleanupWithoutScan(t *testing.T) {
	mgr := &mockManager{listTasksResult: []domain.Task{{ID: "T-1"}}}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m.tasksPanel.SetTasks(mgr.listTasksResult)

	updated, keyCmd := m.Update(sendKey("d"))
	m = updated.(Model)
	if keyCmd == nil {
		t.Fatal("expected key d to emit CleanupTaskMsg command")
	}
	msg, ok := keyCmd().(panels.CleanupTaskMsg)
	if !ok {
		t.Fatalf("expected CleanupTaskMsg, got %T", keyCmd())
	}
	if msg.TaskID != "T-1" {
		t.Fatalf("TaskID = %q, want T-1", msg.TaskID)
	}

	updated, cmd := m.Update(msg)
	m = updated.(Model)
	if cmd == nil || m.taskCleanupRequest == nil {
		t.Fatal("selected task cleanup inspection did not start")
	}
	if m.cleanupQueueCurrent == nil || m.cleanupQueueCurrent.kind != modal.CleanupKindTask || m.cleanupQueueCurrent.id != "T-1" {
		t.Fatalf("queue current = %+v, want task T-1", m.cleanupQueueCurrent)
	}
	if m.cleanupScanning || m.cleanupScanGeneration != 0 {
		t.Fatal("selected cleanup must not run the candidate scan")
	}
	runBatchCommands(cmd())
	if mgr.taskCleanupPlanCalls != 1 {
		t.Fatalf("task cleanup plan calls = %d, want 1", mgr.taskCleanupPlanCalls)
	}
}

func TestUpdate_CleanupTaskMsgRejectsBusyAndWrongContext(t *testing.T) {
	t.Run("cleanup already in flight", func(t *testing.T) {
		mgr := &mockManager{listTasksResult: []domain.Task{{ID: "T-1"}}}
		m := sendWindowSize(newTestModel(t, mgr), 120, 40)
		m.tasksPanel.SetTasks(mgr.listTasksResult)
		m.cleanupQueueCurrent = &cleanupQueueItem{kind: modal.CleanupKindTask, id: "T-1"}

		updated, cmd := m.Update(panels.CleanupTaskMsg{TaskID: "T-1"})
		m = updated.(Model)
		if cmd != nil || mgr.taskCleanupPlanCalls != 0 {
			t.Fatal("busy cleanup accepted a duplicate start")
		}
	})

	t.Run("wrong focus", func(t *testing.T) {
		mgr := &mockManager{listTasksResult: []domain.Task{{ID: "T-1"}}}
		m := sendWindowSize(newTestModel(t, mgr), 120, 40)
		m.tasksPanel.SetTasks(mgr.listTasksResult)
		m.setFocus(FocusServices)

		updated, cmd := m.Update(panels.CleanupTaskMsg{TaskID: "T-1"})
		m = updated.(Model)
		if cmd != nil || mgr.taskCleanupPlanCalls != 0 || m.cleanupQueueCurrent != nil {
			t.Fatal("cleanup task message outside tasks focus must be ignored")
		}
	})

	t.Run("stale selection identity", func(t *testing.T) {
		mgr := &mockManager{listTasksResult: []domain.Task{{ID: "T-1"}}}
		m := sendWindowSize(newTestModel(t, mgr), 120, 40)
		m.tasksPanel.SetTasks(mgr.listTasksResult)

		updated, cmd := m.Update(panels.CleanupTaskMsg{TaskID: "T-2"})
		m = updated.(Model)
		if cmd != nil || mgr.taskCleanupPlanCalls != 0 || m.cleanupQueueCurrent != nil {
			t.Fatal("cleanup task message for a non-selected task must be ignored")
		}
	})
}

func TestUpdate_KeydStartsSelectedReleaseCleanupWithoutScan(t *testing.T) {
	mgr := &mockManager{}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}})

	updated, keyCmd := m.Update(sendKey("d"))
	m = updated.(Model)
	if keyCmd == nil {
		t.Fatal("expected key d to emit CleanupReleaseMsg command")
	}
	msg, ok := keyCmd().(panels.CleanupReleaseMsg)
	if !ok {
		t.Fatalf("expected CleanupReleaseMsg, got %T", keyCmd())
	}
	if msg.ReleaseID != "rel-1" {
		t.Fatalf("ReleaseID = %q, want rel-1", msg.ReleaseID)
	}

	updated, cmd := m.Update(msg)
	m = updated.(Model)
	if cmd == nil || m.releaseCleanupRequest == nil {
		t.Fatal("selected release cleanup planning did not start")
	}
	if m.cleanupQueueCurrent == nil || m.cleanupQueueCurrent.kind != modal.CleanupKindRelease || m.cleanupQueueCurrent.id != "rel-1" {
		t.Fatalf("queue current = %+v, want release rel-1", m.cleanupQueueCurrent)
	}
	if m.cleanupScanning || m.cleanupScanGeneration != 0 {
		t.Fatal("selected cleanup must not run the candidate scan")
	}
	runBatchCommands(cmd())
	if mgr.cleanupPlanCalls != 1 {
		t.Fatalf("release cleanup plan calls = %d, want 1", mgr.cleanupPlanCalls)
	}
	if mgr.cleanupPlanSelection != (task.ReleaseCleanupSelection{RemoveRelease: true}) {
		t.Fatalf("release selection = %+v, want release-only", mgr.cleanupPlanSelection)
	}
}

func TestUpdate_CleanupReleaseMsgRejectsUnreleasedAndWrongContext(t *testing.T) {
	t.Run("unreleased release", func(t *testing.T) {
		mgr := &mockManager{}
		m := sendWindowSize(newTestModel(t, mgr), 120, 40)
		m.setFocus(FocusReleases)
		m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusDraft}})

		updated, cmd := m.Update(panels.CleanupReleaseMsg{ReleaseID: "rel-1"})
		m = updated.(Model)
		if cmd != nil || mgr.cleanupPlanCalls != 0 || m.cleanupQueueCurrent != nil {
			t.Fatal("cleanup of an unreleased release must not start")
		}
	})

	t.Run("wrong focus", func(t *testing.T) {
		mgr := &mockManager{}
		m := sendWindowSize(newTestModel(t, mgr), 120, 40)
		m.setFocus(FocusTasks)
		m.releasesPanel.SetReleases([]domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusReleased}})

		updated, cmd := m.Update(panels.CleanupReleaseMsg{ReleaseID: "rel-1"})
		m = updated.(Model)
		if cmd != nil || mgr.cleanupPlanCalls != 0 || m.cleanupQueueCurrent != nil {
			t.Fatal("cleanup release message outside releases focus must be ignored")
		}
	})
}

func TestUpdate_KeyDeleteStillOpensRemoveTaskDialog(t *testing.T) {
	mgr := &mockManager{listTasksResult: []domain.Task{{ID: "T-1"}}}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m.tasksPanel.SetTasks(mgr.listTasksResult)

	updated, keyCmd := m.Update(tea.KeyMsg{Type: tea.KeyDelete})
	m = updated.(Model)
	if keyCmd == nil {
		t.Fatal("expected delete key to emit OpenRemoveDialogMsg command")
	}
	if _, ok := keyCmd().(panels.OpenRemoveDialogMsg); !ok {
		t.Fatalf("expected OpenRemoveDialogMsg, got %T", keyCmd())
	}
	updated, _ = m.Update(keyCmd())
	m = updated.(Model)
	if _, ok := m.modal.(*modal.RemoveTaskDialog); !ok {
		t.Fatalf("delete key modal = %T, want RemoveTaskDialog", m.modal)
	}
}
