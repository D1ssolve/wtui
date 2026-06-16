package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/task"
)

func TestRiderTaskArgsUsesTaskIDSolution(t *testing.T) {
	name, args := riderTaskArgs("IN-001")

	if name != "rider" {
		t.Fatalf("name = %q, want rider", name)
	}
	if len(args) != 1 || args[0] != "IN-001.sln" {
		t.Fatalf("args = %v, want [IN-001.sln]", args)
	}
}

func TestCodeWorkspaceTaskArgsUsesTaskIDWorkspace(t *testing.T) {
	name, args := codeWorkspaceTaskArgs("code", "IN-001")

	if name != "code" {
		t.Fatalf("name = %q, want code", name)
	}
	if len(args) != 1 || args[0] != "IN-001.code-workspace" {
		t.Fatalf("args = %v, want [IN-001.code-workspace]", args)
	}
}

func TestCodeWorkspaceTaskArgsUsesConfiguredEditor(t *testing.T) {
	name, _ := codeWorkspaceTaskArgs("cursor", "MY-TASK")
	if name != "cursor" {
		t.Fatalf("name = %q, want cursor", name)
	}
}

func TestExecTeaProcessReturnsOriginalErrorAndOp(t *testing.T) {
	original := errors.New("rider failed")
	msg := execProcessDoneMsg("Open Rider for IN-001", original)
	done, ok := msg.(CommandDoneMsg)
	if !ok {
		t.Fatalf("msg = %T, want CommandDoneMsg", msg)
	}
	if !errors.Is(done.Err, original) {
		t.Fatalf("err = %v, want original error", done.Err)
	}
	if strings.Contains(done.Err.Error(), "shell:") {
		t.Fatalf("err = %q, must not add shell-specific context", done.Err.Error())
	}
	if done.Op != "Open Rider for IN-001" {
		t.Fatalf("op = %q, want Open Rider for IN-001", done.Op)
	}

	msg = execProcessDoneMsg("Open Rider for IN-001", nil)
	done, ok = msg.(CommandDoneMsg)
	if !ok {
		t.Fatalf("msg = %T, want CommandDoneMsg", msg)
	}
	if done.Err != nil {
		t.Fatalf("err = %v, want nil", done.Err)
	}
}

func TestLazygitServiceArgsUsesWorktreePath(t *testing.T) {
	name, args := lazygitServiceArgs("/tmp/service")

	if name != "lazygit" {
		t.Fatalf("name = %q, want lazygit", name)
	}
	if len(args) != 2 || args[0] != "-p" || args[1] != "/tmp/service" {
		t.Fatalf("args = %v, want [-p /tmp/service]", args)
	}
}

func TestLazygitServiceExecCmdUsesWorktreeDir(t *testing.T) {
	cmd := lazygitServiceExecCmd("/tmp/service")

	if filepath.Base(cmd.Path) != "lazygit" {
		t.Fatalf("Path = %q, want lazygit executable", cmd.Path)
	}
	if cmd.Dir != "/tmp/service" {
		t.Fatalf("Dir = %q, want /tmp/service", cmd.Dir)
	}
	if len(cmd.Args) != 3 || cmd.Args[0] != "lazygit" || cmd.Args[1] != "-p" || cmd.Args[2] != "/tmp/service" {
		t.Fatalf("Args = %v, want [lazygit -p /tmp/service]", cmd.Args)
	}
}

func TestLazygitDoneMessagePreservesMetadataAndError(t *testing.T) {
	original := errors.New("lazygit failed")
	msg := lazygitServiceDoneMsg("IN-001", "collection", "/tmp/service", original)

	got, ok := msg.(LazygitDoneMsg)
	if !ok {
		t.Fatalf("msg = %T, want LazygitDoneMsg", msg)
	}
	if got.TaskID != "IN-001" {
		t.Errorf("TaskID = %q, want IN-001", got.TaskID)
	}
	if got.ServiceName != "collection" {
		t.Errorf("ServiceName = %q, want collection", got.ServiceName)
	}
	if got.WorktreePath != "/tmp/service" {
		t.Errorf("WorktreePath = %q, want /tmp/service", got.WorktreePath)
	}
	if !errors.Is(got.Err, original) {
		t.Fatalf("Err = %v, want original", got.Err)
	}
}

type cmdManager struct {
	mockManager

	validateTaskID string
	validateResult domain.TaskValidation
	validateErr    error

	planTaskID string
	planResult task.ClosePlan
	planErr    error

	closeParams task.CloseTaskParams
	closeResult task.CloseTaskResult
	closeErr    error

	scanCalled bool
	scanResult []domain.PruneCandidate
	scanErr    error

	removeCalls []string
	removeErrs  map[string]error

	tagTaskID string
	tagResult []domain.TagInfo
	tagErr    error

	forgeCreateMRArgs      forge.CreateMRParams
	forgePipelineStatusArg forgePipelineStatusParams
	forgeListIssuesArgs    forge.ListIssuesParams
	forgeMRResult          forge.MRInfo
	forgePipelineResult    []forge.PipelineStatus
	forgeIssuesResult      []forge.IssueInfo
	forgeErr               error
}

func (m *cmdManager) ValidateTask(_ context.Context, taskID string) (domain.TaskValidation, error) {
	m.validateTaskID = taskID
	return m.validateResult, m.validateErr
}

func (m *cmdManager) PlanCloseTask(_ context.Context, taskID string) (task.ClosePlan, error) {
	m.planTaskID = taskID
	return m.planResult, m.planErr
}

func (m *cmdManager) CloseTask(_ context.Context, params task.CloseTaskParams) (task.CloseTaskResult, error) {
	m.closeParams = params
	if params.StatusCh != nil {
		params.StatusCh <- "step 1"
		params.StatusCh <- "step 2"
		close(params.StatusCh)
	}
	time.Sleep(10 * time.Millisecond)
	return m.closeResult, m.closeErr
}

func (m *cmdManager) ScanPrunableTasks(_ context.Context) ([]domain.PruneCandidate, error) {
	m.scanCalled = true
	return m.scanResult, m.scanErr
}

func (m *cmdManager) Remove(_ context.Context, taskID string, _, _ bool) error {
	m.removeCalls = append(m.removeCalls, taskID)
	if err, ok := m.removeErrs[taskID]; ok {
		return err
	}
	return nil
}

func (m *cmdManager) ListTags(_ context.Context, taskID string) ([]domain.TagInfo, error) {
	m.tagTaskID = taskID
	return m.tagResult, m.tagErr
}

func (m *cmdManager) ForgeCreateMR(_ context.Context, _, _ string, params forge.CreateMRParams) (forge.MRInfo, error) {
	m.forgeCreateMRArgs = params
	return m.forgeMRResult, m.forgeErr
}

func (m *cmdManager) ForgePipelineStatus(_ context.Context, _, _ string, branch string) ([]forge.PipelineStatus, error) {
	m.forgePipelineStatusArg = forgePipelineStatusParams{Branch: branch}
	return m.forgePipelineResult, m.forgeErr
}

func (m *cmdManager) ForgeListIssues(_ context.Context, _, _ string, params forge.ListIssuesParams) ([]forge.IssueInfo, error) {
	m.forgeListIssuesArgs = params
	return m.forgeIssuesResult, m.forgeErr
}

func (m *cmdManager) PromoteToRelease(_ context.Context, _ task.PromoteToReleaseParams) (domain.Task, error) {
	return domain.Task{}, nil
}

func TestCmdManager_ImplementsTaskManager(t *testing.T) {
	var _ task.Manager = (*cmdManager)(nil)
}

func TestValidateTaskCmdReturnsValidationResult(t *testing.T) {
	mgr := &cmdManager{validateResult: domain.TaskValidation{TaskID: "T14", Blocking: true}}

	msg := validateTaskCmd(mgr, "T14")()
	got, ok := msg.(ValidationResultMsg)
	if !ok {
		t.Fatalf("msg = %T, want ValidationResultMsg", msg)
	}
	if got.Validation.TaskID != "T14" {
		t.Fatalf("TaskID = %q, want T14", got.Validation.TaskID)
	}
	if mgr.validateTaskID != "T14" {
		t.Fatalf("ValidateTask called with %q, want T14", mgr.validateTaskID)
	}
}

func TestPlanCloseTaskCmdReturnsClosePlanReadyMsg(t *testing.T) {
	mgr := &cmdManager{planResult: task.ClosePlan{TaskID: "T14"}}

	msg := planCloseTaskCmd(mgr, "T14")()
	got, ok := msg.(ClosePlanReadyMsg)
	if !ok {
		t.Fatalf("msg = %T, want ClosePlanReadyMsg", msg)
	}
	if got.Plan.TaskID != "T14" {
		t.Fatalf("Plan.TaskID = %q, want T14", got.Plan.TaskID)
	}
}

func TestCloseTaskCmdStreamsOutputAndFinishes(t *testing.T) {
	mgr := &cmdManager{closeResult: task.CloseTaskResult{TaskID: "T14", Success: true}}

	cmd := closeTaskCmd(mgr, task.CloseTaskParams{TaskID: "T14"})
	msg1 := cmd()
	line1, ok := msg1.(OutputLineMsg)
	if !ok {
		t.Fatalf("msg1 = %T, want OutputLineMsg", msg1)
	}
	if line1.Line != "step 1" {
		t.Fatalf("line1 = %q, want step 1", line1.Line)
	}

	msg2 := line1.Next()
	line2, ok := msg2.(OutputLineMsg)
	if !ok {
		t.Fatalf("msg2 = %T, want OutputLineMsg", msg2)
	}
	if line2.Line != "step 2" {
		t.Fatalf("line2 = %q, want step 2", line2.Line)
	}

	msg3 := line2.Next()
	finished, ok := msg3.(CloseTaskFinishedMsg)
	if !ok {
		t.Fatalf("msg3 = %T, want CloseTaskFinishedMsg", msg3)
	}
	if !finished.Result.Success {
		t.Fatal("finished.Result.Success = false, want true")
	}
	if mgr.closeParams.StatusCh == nil {
		t.Fatal("CloseTask params.StatusCh = nil, want non-nil")
	}
}

func TestScanPrunableTasksCmdReturnsPlanReady(t *testing.T) {
	mgr := &cmdManager{scanResult: []domain.PruneCandidate{{TaskID: "T1", Prunable: true}}}

	msg := scanPrunableTasksCmd(mgr)()
	got, ok := msg.(PrunePlanReadyMsg)
	if !ok {
		t.Fatalf("msg = %T, want PrunePlanReadyMsg", msg)
	}
	if len(got.Candidates) != 1 || got.Candidates[0].TaskID != "T1" {
		t.Fatalf("candidates = %#v, want one T1", got.Candidates)
	}
}

func TestPruneTasksCmdStreamsAndReturnsSummary(t *testing.T) {
	mgr := &cmdManager{removeErrs: map[string]error{"T2": errors.New("boom")}}

	cmd := pruneTasksCmd(mgr, []string{"T1", "T2"})

	lineCount := 0
	for msg := cmd(); ; {
		switch m := msg.(type) {
		case OutputLineMsg:
			lineCount++
			msg = m.Next()
		case PruneFinishedMsg:
			if len(m.Removed) != 1 || m.Removed[0] != "T1" {
				t.Fatalf("Removed = %v, want [T1]", m.Removed)
			}
			if len(m.Errors) != 1 {
				t.Fatalf("Errors len = %d, want 1", len(m.Errors))
			}
			if lineCount < 3 {
				t.Fatalf("lineCount = %d, want >= 3", lineCount)
			}
			if len(mgr.removeCalls) != 2 {
				t.Fatalf("remove calls = %v, want [T1 T2]", mgr.removeCalls)
			}
			return
		default:
			t.Fatalf("unexpected msg type %T", msg)
		}
	}
}

func TestListTagsCmdReturnsTagListMsg(t *testing.T) {
	mgr := &cmdManager{tagResult: []domain.TagInfo{{Name: "v1.2.3"}}}

	msg := listTagsCmd(mgr, "T14")()
	got, ok := msg.(TagListMsg)
	if !ok {
		t.Fatalf("msg = %T, want TagListMsg", msg)
	}
	if got.TaskID != "T14" {
		t.Fatalf("TaskID = %q, want T14", got.TaskID)
	}
	if len(got.Tags) != 1 || got.Tags[0].Name != "v1.2.3" {
		t.Fatalf("Tags = %#v, want [v1.2.3]", got.Tags)
	}
}

func TestForgeOpCmdDelegatesCreateMR(t *testing.T) {
	mgr := &cmdManager{forgeMRResult: forge.MRInfo{URL: "https://mr"}}
	params := forge.CreateMRParams{SourceBranch: "feature/T14", TargetBranch: "develop"}

	msg := forgeOpCmd(mgr, "create_mr", "T14", "svc", params)()
	got, ok := msg.(ForgeResultMsg)
	if !ok {
		t.Fatalf("msg = %T, want ForgeResultMsg", msg)
	}
	data, ok := got.Data.(forge.MRInfo)
	if !ok {
		t.Fatalf("data = %T, want forge.MRInfo", got.Data)
	}
	if data.URL != "https://mr" {
		t.Fatalf("mr url = %q, want https://mr", data.URL)
	}
	if mgr.forgeCreateMRArgs.SourceBranch != "feature/T14" {
		t.Fatalf("source branch = %q, want feature/T14", mgr.forgeCreateMRArgs.SourceBranch)
	}
}

func TestForgeOpCmdDelegatesPipelineStatus(t *testing.T) {
	mgr := &cmdManager{forgePipelineResult: []forge.PipelineStatus{{ID: "1"}}}
	params := forgePipelineStatusParams{Branch: "develop"}

	msg := forgeOpCmd(mgr, "pipeline_status", "T14", "svc", params)()
	got := msg.(ForgeResultMsg)
	data, ok := got.Data.([]forge.PipelineStatus)
	if !ok {
		t.Fatalf("data = %T, want []forge.PipelineStatus", got.Data)
	}
	if len(data) != 1 || data[0].ID != "1" {
		t.Fatalf("data = %#v, want [{ID:1}]", data)
	}
}

func TestForgeOpCmdDelegatesListIssues(t *testing.T) {
	mgr := &cmdManager{forgeIssuesResult: []forge.IssueInfo{{Number: 7}}}
	params := forge.ListIssuesParams{State: "open"}

	msg := forgeOpCmd(mgr, "list_issues", "T14", "svc", params)()
	got := msg.(ForgeResultMsg)
	data, ok := got.Data.([]forge.IssueInfo)
	if !ok {
		t.Fatalf("data = %T, want []forge.IssueInfo", got.Data)
	}
	if len(data) != 1 || data[0].Number != 7 {
		t.Fatalf("data = %#v, want [{Number:7}]", data)
	}
}

func TestForgeOpCmdUnsupportedOperation(t *testing.T) {
	mgr := &cmdManager{}

	msg := forgeOpCmd(mgr, "unknown", "T14", "svc", nil)()
	got := msg.(ForgeResultMsg)
	if got.Err == nil {
		t.Fatal("Err = nil, want error")
	}
}

func TestForgeOpCmdManagerWithoutForgeSupport(t *testing.T) {
	mgr := &cmdManager{forgeErr: errors.New("forge unavailable")}
	msg := forgeOpCmd(mgr, "create_mr", "T14", "svc", forge.CreateMRParams{})()
	got := msg.(ForgeResultMsg)
	if got.Err == nil {
		t.Fatal("Err = nil, want error")
	}
}
