package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	"github.com/D1ssolve/wtui/internal/tui/panels"
)

type closeTaskE2EManager struct {
	tasks    []domain.Task
	services map[string][]domain.Service

	validation    domain.TaskValidation
	validationErr error

	plan    task.ClosePlan
	planErr error

	closeResult task.CloseTaskResult
	closeErr    error

	planCalls     int
	validateCalls int
	closeCalls    int

	lastCloseParams task.CloseTaskParams
	mutationCalls   int
}

var _ task.Manager = (*closeTaskE2EManager)(nil)

func (m *closeTaskE2EManager) Init(_ context.Context, _ task.InitParams) (task.PartialFailureResult, error) {
	return task.PartialFailureResult{}, nil
}

func (m *closeTaskE2EManager) Add(_ context.Context, _ task.AddParams) (task.PartialFailureResult, error) {
	return task.PartialFailureResult{}, nil
}

func (m *closeTaskE2EManager) List(_ context.Context) ([]domain.Task, error) { return m.tasks, nil }

func (m *closeTaskE2EManager) ListServices(_ context.Context, taskID string) ([]domain.Service, error) {
	if m.services == nil {
		return nil, nil
	}
	return m.services[taskID], nil
}

func (m *closeTaskE2EManager) Remove(_ context.Context, _ string, _, _ bool) error { return nil }

func (m *closeTaskE2EManager) Repos(_ context.Context, _ bool) ([]domain.Repo, error) {
	return nil, nil
}

func (m *closeTaskE2EManager) SyncTask(_ context.Context, _ string, _ task.SyncStrategy, lineCh chan<- string) error {
	close(lineCh)
	return nil
}

func (m *closeTaskE2EManager) SyncService(_ context.Context, _, _ string, _ task.SyncStrategy, lineCh chan<- string) error {
	close(lineCh)
	return nil
}

func (m *closeTaskE2EManager) PushTask(_ context.Context, _ string, lineCh chan<- string) error {
	close(lineCh)
	return nil
}

func (m *closeTaskE2EManager) PushService(_ context.Context, _, _ string, lineCh chan<- string) error {
	close(lineCh)
	return nil
}

func (m *closeTaskE2EManager) StashService(_ context.Context, _, _ string, _, _ bool) error {
	return nil
}

func (m *closeTaskE2EManager) RemoveService(_ context.Context, _, _ string, _ bool) error { return nil }

func (m *closeTaskE2EManager) ValidateTask(_ context.Context, _ string) (domain.TaskValidation, error) {
	m.validateCalls++
	return m.validation, m.validationErr
}

func (m *closeTaskE2EManager) PlanCloseTask(_ context.Context, _ string) (task.ClosePlan, error) {
	m.planCalls++
	return m.plan, m.planErr
}

func (m *closeTaskE2EManager) CloseTask(_ context.Context, params task.CloseTaskParams) (task.CloseTaskResult, error) {
	m.closeCalls++
	m.lastCloseParams = params

	isDryRun := params.DryRun || strings.Contains(strings.TrimSpace(params.TagVersion), "dry-run")
	if !isDryRun {
		m.mutationCalls++
	}

	if params.StatusCh != nil {
		for _, step := range m.closeResult.Steps {
			params.StatusCh <- step.Name + ": " + step.Message
		}
	}

	return m.closeResult, m.closeErr
}

func (m *closeTaskE2EManager) ScanPrunableTasks(_ context.Context) ([]domain.PruneCandidate, error) {
	return nil, nil
}

func (m *closeTaskE2EManager) ListTags(_ context.Context, _ string) ([]domain.TagInfo, error) {
	return nil, nil
}

func (m *closeTaskE2EManager) ForgeCreateMR(_ context.Context, _, _ string, _ forge.CreateMRParams) (forge.MRInfo, error) {
	return forge.MRInfo{}, nil
}

func (m *closeTaskE2EManager) ForgePipelineStatus(_ context.Context, _, _ string, _ string) ([]forge.PipelineStatus, error) {
	return nil, nil
}

func (m *closeTaskE2EManager) ForgeListIssues(_ context.Context, _, _ string, _ forge.ListIssuesParams) ([]forge.IssueInfo, error) {
	return nil, nil
}

func (m *closeTaskE2EManager) ListReleases(_ context.Context) ([]domain.Release, error) {
	return nil, nil
}

func (m *closeTaskE2EManager) GetRelease(_ context.Context, _ string) (domain.Release, error) {
	return domain.Release{}, nil
}

func (m *closeTaskE2EManager) CreateRelease(_ context.Context, _ task.CreateReleaseParams) (domain.Release, error) {
	return domain.Release{}, nil
}

func (m *closeTaskE2EManager) FinishRelease(_ context.Context, _ task.FinishReleaseParams) (domain.Release, error) {
	return domain.Release{}, nil
}

func (m *closeTaskE2EManager) IsProtectedBranch(_ context.Context, _ string) bool { return false }

func (m *closeTaskE2EManager) BuildReleasePreview(_ context.Context, _ map[string]string) (task.ReleasePreview, error) {
	return task.ReleasePreview{}, nil
}

func (m *closeTaskE2EManager) RetryRelease(_ context.Context, _ string) (domain.Release, error) {
	return domain.Release{}, nil
}

func (m *closeTaskE2EManager) RejectRelease(_ context.Context, _ string) (domain.Release, error) {
	return domain.Release{}, nil
}

func (m *closeTaskE2EManager) RemoveRelease(_ context.Context, _ string) error { return nil }

func TestE2E_CloseTask_DirtyTask_ShowsValidationModalAndDoesNotExecuteClose(t *testing.T) {
	mgr := &closeTaskE2EManager{
		tasks:   []domain.Task{{ID: "IN-200", Dir: "/tmp/.tasks/IN-200"}},
		planErr: errors.New("task validation failed"),
		validation: domain.TaskValidation{
			TaskID:   "IN-200",
			Blocking: true,
			Services: []domain.ServiceValidation{{
				ServiceName: "api",
				Branch:      "feature/IN-200",
				States:      []domain.RepoState{domain.RepoStateDirty},
			}},
		},
	}

	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	m.tasksPanel.SetTasks(mgr.tasks)

	updated, keyCmd := m.Update(sendKey("C"))
	m = updated.(Model)
	if keyCmd == nil {
		t.Fatal("expected key C to emit PlanCloseTaskMsg command")
	}

	planMsg, ok := keyCmd().(panels.PlanCloseTaskMsg)
	if !ok {
		t.Fatalf("expected PlanCloseTaskMsg, got %T", keyCmd())
	}

	updated, planBatchCmd := m.Update(planMsg)
	m = updated.(Model)
	planReady, ok := extractClosePlanReadyFromBatch(planBatchCmd)
	if !ok {
		t.Fatal("expected ClosePlanReadyMsg from planning batch")
	}

	updated, _ = m.Update(planReady)
	m = updated.(Model)

	if m.modal != nil {
		t.Fatalf("expected no close confirm modal on dirty task, got %T", m.modal)
	}

	validationMsg := validateTaskCmd(mgr, "IN-200")()
	updated, _ = m.Update(validationMsg)
	m = updated.(Model)

	if _, ok := m.modal.(*modal.ValidationErrorModal); !ok {
		t.Fatalf("expected ValidationErrorModal, got %T", m.modal)
	}
	if mgr.closeCalls != 0 {
		t.Fatalf("close execution must not start on dirty task, got calls=%d", mgr.closeCalls)
	}
}

func TestE2E_CloseTask_CleanTask_ConfirmThenSummaryWithStatuses(t *testing.T) {
	mgr := &closeTaskE2EManager{
		tasks: []domain.Task{{ID: "IN-201", Dir: "/tmp/.tasks/IN-201"}},
		plan: task.ClosePlan{
			TaskID:     "IN-201",
			BranchType: gitflow.BranchTypeFeature,
			Services: []task.ServiceClosePlan{{
				ServiceName:    "api",
				SourceBranch:   "feature/IN-201",
				TargetBranches: []string{"develop"},
				CloseStrategy:  gitflow.CloseStrategyDirectMerge,
			}},
		},
		closeResult: task.CloseTaskResult{
			TaskID:  "IN-201",
			Success: true,
			Steps: []task.CloseTaskStep{
				{Name: "api:fetch", Status: task.StepStatusOK, Message: "fetched"},
				{Name: "api:merge:develop", Status: task.StepStatusSkipped, Message: "already merged"},
				{Name: "api:push:develop", Status: task.StepStatusOK, Message: "pushed"},
			},
		},
	}

	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	m.tasksPanel.SetTasks(mgr.tasks)

	m = runClosePlanKeyFlow(t, m)

	if _, ok := m.modal.(*modal.CloseTaskConfirmModal); !ok {
		t.Fatalf("expected CloseTaskConfirmModal, got %T", m.modal)
	}

	updated, submitCmd := m.Update(sendSpecialKey(KeyEnter))
	m = updated.(Model)
	if submitCmd == nil {
		t.Fatal("expected Enter on confirm modal to emit submit command")
	}

	submitMsg, ok := submitCmd().(modal.SubmitCloseTaskMsg)
	if !ok {
		t.Fatalf("expected SubmitCloseTaskMsg, got %T", submitCmd())
	}

	updated, closeCmd := m.Update(submitMsg)
	m = updated.(Model)
	finished, ok := drainCloseCommand(closeCmd)
	if !ok {
		t.Fatal("expected CloseTaskFinishedMsg from close command")
	}

	updated, _ = m.Update(finished)
	m = updated.(Model)

	summary, ok := m.modal.(*modal.CloseTaskSummaryModal)
	if !ok {
		t.Fatalf("expected CloseTaskSummaryModal, got %T", m.modal)
	}

	view := summary.View()
	for _, want := range []string{"api:fetch", "✓ ok", "api:merge:develop", "⊘ skipped", "Overall: SUCCESS"} {
		if !strings.Contains(view, want) {
			t.Fatalf("summary missing %q\nview:\n%s", want, view)
		}
	}
}

func TestE2E_CloseTask_DryRun_ShowsPlannedStepsWithoutMutations(t *testing.T) {
	mgr := &closeTaskE2EManager{
		tasks: []domain.Task{{ID: "IN-202", Dir: "/tmp/.tasks/IN-202"}},
		plan: task.ClosePlan{
			TaskID:     "IN-202",
			BranchType: gitflow.BranchTypeFeature,
			Services: []task.ServiceClosePlan{{
				ServiceName:   "api",
				SourceBranch:  "feature/IN-202",
				CloseStrategy: gitflow.CloseStrategyDirectMerge,
				TagPlan:       &task.TagPlan{Version: "1.0.0"},
			}},
		},
		closeResult: task.CloseTaskResult{
			TaskID:  "IN-202",
			Success: true,
			Steps: []task.CloseTaskStep{
				{Name: "validate", Status: task.StepStatusOK, Message: "plan ready"},
				{Name: "api:merge:develop", Status: task.StepStatusOK, Message: "dry-run: merge source into target"},
				{Name: "api:tag", Status: task.StepStatusOK, Message: "dry-run: create and push tag v1.0.0"},
			},
		},
	}

	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	m.tasksPanel.SetTasks(mgr.tasks)

	m = runClosePlanKeyFlow(t, m)

	if _, ok := m.modal.(*modal.CloseTaskConfirmModal); !ok {
		t.Fatalf("expected CloseTaskConfirmModal, got %T", m.modal)
	}

	for _, ch := range "dry-run" {
		updated, _ := m.Update(sendKey(string(ch)))
		m = updated.(Model)
	}

	updated, submitCmd := m.Update(sendSpecialKey(KeyEnter))
	m = updated.(Model)
	if submitCmd == nil {
		t.Fatal("expected Enter on confirm modal to emit submit command")
	}

	submitMsg, ok := submitCmd().(modal.SubmitCloseTaskMsg)
	if !ok {
		t.Fatalf("expected SubmitCloseTaskMsg, got %T", submitCmd())
	}

	updated, closeCmd := m.Update(submitMsg)
	m = updated.(Model)
	finished, ok := drainCloseCommand(closeCmd)
	if !ok {
		t.Fatal("expected CloseTaskFinishedMsg from close command")
	}

	updated, _ = m.Update(finished)
	m = updated.(Model)

	summary, ok := m.modal.(*modal.CloseTaskSummaryModal)
	if !ok {
		t.Fatalf("expected CloseTaskSummaryModal, got %T", m.modal)
	}
	if mgr.mutationCalls != 0 {
		t.Fatalf("dry-run must not mutate anything, mutationCalls=%d", mgr.mutationCalls)
	}

	view := summary.View()
	for _, want := range []string{"dry-run: merge source into target", "dry-run: create and push tag", "Overall: SUCCESS"} {
		if !strings.Contains(view, want) {
			t.Fatalf("summary missing %q\nview:\n%s", want, view)
		}
	}
}

func runClosePlanKeyFlow(t *testing.T, m Model) Model {
	t.Helper()

	updated, keyCmd := m.Update(sendKey("C"))
	m = updated.(Model)
	if keyCmd == nil {
		t.Fatal("expected key C to emit PlanCloseTaskMsg command")
	}

	planMsg, ok := keyCmd().(panels.PlanCloseTaskMsg)
	if !ok {
		t.Fatalf("expected PlanCloseTaskMsg, got %T", keyCmd())
	}

	updated, planBatchCmd := m.Update(planMsg)
	m = updated.(Model)
	planReady, ok := extractClosePlanReadyFromBatch(planBatchCmd)
	if !ok {
		t.Fatal("expected ClosePlanReadyMsg from planning batch")
	}

	updated, _ = m.Update(planReady)
	return updated.(Model)
}

func extractClosePlanReadyFromBatch(cmd tea.Cmd) (ClosePlanReadyMsg, bool) {
	if cmd == nil {
		return ClosePlanReadyMsg{}, false
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		plan, ok := msg.(ClosePlanReadyMsg)
		return plan, ok
	}
	for _, c := range batch {
		if c == nil {
			continue
		}
		if plan, ok := c().(ClosePlanReadyMsg); ok {
			return plan, true
		}
	}
	return ClosePlanReadyMsg{}, false
}

func drainCloseCommand(cmd tea.Cmd) (CloseTaskFinishedMsg, bool) {
	if cmd == nil {
		return CloseTaskFinishedMsg{}, false
	}

	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c == nil {
				continue
			}
			if finished, ok := drainCloseCommand(c); ok {
				return finished, true
			}
		}
		return CloseTaskFinishedMsg{}, false
	}

	for i := 0; i < 32; i++ {
		switch m := msg.(type) {
		case CloseTaskFinishedMsg:
			return m, true
		case OutputLineMsg:
			if m.Next == nil {
				return CloseTaskFinishedMsg{}, false
			}
			msg = m.Next()
		default:
			return CloseTaskFinishedMsg{}, false
		}
	}

	return CloseTaskFinishedMsg{}, false
}
