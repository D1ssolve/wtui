package tui

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	"github.com/D1ssolve/wtui/internal/tui/panels"
)

func TestExecTeaProcessReturnsOriginalErrorAndOp(t *testing.T) {
	original := errors.New("rider failed")
	msg := execProcessDoneMsg("Open Rider for IN-001", original, 7)
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

	msg = execProcessDoneMsg("Open Rider for IN-001", nil, 7)
	done, ok = msg.(CommandDoneMsg)
	if !ok {
		t.Fatalf("msg = %T, want CommandDoneMsg", msg)
	}
	if done.Err != nil {
		t.Fatalf("err = %v, want nil", done.Err)
	}
}

func TestShellExecCommandUsesShAndTaskDir(t *testing.T) {
	cmd := shellExecCommand("git status", "/tmp/.tasks/IN-001")

	if filepath.Base(cmd.Path) != "sh" {
		t.Fatalf("Path = %q, want sh", cmd.Path)
	}
	if strings.Join(cmd.Args, "\x00") != strings.Join([]string{"sh", "-c", "git status"}, "\x00") {
		t.Fatalf("Args = %v, want [sh -c git status]", cmd.Args)
	}
	if cmd.Dir != "/tmp/.tasks/IN-001" {
		t.Fatalf("Dir = %q, want /tmp/.tasks/IN-001", cmd.Dir)
	}
}

type processTestModel struct {
	cmd  tea.Cmd
	done *CommandDoneMsg
}

func (m processTestModel) Init() tea.Cmd { return m.cmd }
func (m processTestModel) View() string  { return "" }
func (m processTestModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if done, ok := msg.(CommandDoneMsg); ok {
		m.done = &done
		return m, tea.Quit
	}
	return m, nil
}

func runProcessTestCmd(t *testing.T, cmd tea.Cmd) CommandDoneMsg {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	model, err := tea.NewProgram(processTestModel{cmd: cmd}, tea.WithContext(ctx),
		tea.WithInput(strings.NewReader("")), tea.WithOutput(io.Discard), tea.WithoutRenderer()).Run()
	if err != nil {
		t.Fatal(err)
	}
	done := model.(processTestModel).done
	if done == nil {
		t.Fatal("process did not produce CommandDoneMsg")
	}
	return *done
}

func fakeIDE(t *testing.T, name string) (string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake IDE uses a POSIX executable script")
	}
	dir := t.TempDir()
	executable := filepath.Join(dir, name)
	record := filepath.Join(dir, "launch.txt")
	t.Setenv("WTUI_TEST_IDE_RECORD", record)
	t.Setenv("WTUI_TEST_IDE_EXIT", "0")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nprintf '%s\\n' \"$#\" \"$1\" \"$PWD\" >> \"$WTUI_TEST_IDE_RECORD\"\nexit \"$WTUI_TEST_IDE_EXIT\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return executable, record
}

func TestOpenReleaseFolderCmd_LiteralDirectoryAndCompletion(t *testing.T) {
	executable, record := fakeIDE(t, "fake IDE")
	root := t.TempDir()
	t.Chdir(root)
	dir := "release space ; $(touch injected) & 'quoted'"
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	wantDir := filepath.Join(root, dir)
	for _, exit := range []string{"0", "7"} {
		t.Run("exit_"+exit, func(t *testing.T) {
			t.Setenv("WTUI_TEST_IDE_EXIT", exit)
			done := runProcessTestCmd(t, openReleaseFolderCmd(executable, "rel-123", "./"+dir+"/../"+dir+"/.", 7))
			for _, value := range []string{executable, "rel-123", wantDir} {
				if !strings.Contains(done.Op, value) {
					t.Fatalf("Op = %q, missing %q", done.Op, value)
				}
			}
			if exit == "0" && done.Err != nil {
				t.Fatalf("success error = %v", done.Err)
			}
			if exit == "7" {
				var exitErr *exec.ExitError
				if !errors.As(done.Err, &exitErr) || exitErr.ExitCode() != 7 {
					t.Fatalf("error = %v, want exit 7", done.Err)
				}
			}
		})
	}
	got, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Repeat("1\n"+wantDir+"\n"+wantDir+"\n", 2); string(got) != want {
		t.Fatalf("launch records = %q, want %q", got, want)
	}
}

func TestOpenReleaseFolderCmd_InvalidDirectoryDoesNotLaunch(t *testing.T) {
	executable, record := fakeIDE(t, "fake-editor")
	file := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(file, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"", filepath.Join(t.TempDir(), "missing"), file, file + string(os.PathSeparator) + "child"} {
		t.Run(dir, func(t *testing.T) {
			msg := openReleaseFolderCmd(executable, "rel-invalid", dir, 7)()
			done, ok := msg.(CommandDoneMsg)
			if !ok || done.Err == nil {
				t.Fatalf("message = %#v, want completion error without process", msg)
			}
			for _, value := range []string{executable, "rel-invalid", dir} {
				if !strings.Contains(done.Op, value) {
					t.Fatalf("Op = %q, missing %q", done.Op, value)
				}
			}
		})
	}
	if _, err := os.Stat(record); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid path launched IDE: stat error = %v", err)
	}
}

func TestOpenReleaseFolderCmd_MissingExecutable(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "missing-editor")
	done := runProcessTestCmd(t, openReleaseFolderCmd(executable, "rel-missing", dir, 7))
	if !errors.Is(done.Err, os.ErrNotExist) {
		t.Fatalf("error = %v, want missing executable", done.Err)
	}
	for _, value := range []string{executable, "rel-missing", dir} {
		if !strings.Contains(done.Op, value) {
			t.Fatalf("Op = %q, missing %q", done.Op, value)
		}
	}
}

func TestTaskIDECommands_PreserveTargetsAndWorkingDirectory(t *testing.T) {
	executable, record := fakeIDE(t, "rider")
	t.Setenv("PATH", filepath.Dir(executable)+string(os.PathListSeparator)+os.Getenv("PATH"))
	dir := t.TempDir()
	for _, cmd := range []tea.Cmd{riderTaskCmd("TASK-1", dir, 7), codeWorkspaceTaskCmd(executable, "TASK-1", dir, 7)} {
		if done := runProcessTestCmd(t, cmd); done.Err != nil {
			t.Fatal(done.Err)
		}
	}
	got, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if want := "1\nTASK-1.sln\n" + dir + "\n1\nTASK-1.code-workspace\n" + dir + "\n"; string(got) != want {
		t.Fatalf("task launch records = %q, want %q", got, want)
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

type cmdManager struct {
	mockManager

	initPartial task.PartialFailureResult
	initErr     error

	addPartial task.PartialFailureResult
	addErr     error

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
	tagCalls  []string
	tagResult []domain.TagInfo
	tagErr    error

	listReleasesCalled bool
	listReleasesCtx    context.Context
	listReleasesResult []domain.Release
	listReleasesErr    error

	createReleaseCtx    context.Context
	createReleaseParams task.CreateReleaseParams
	createReleaseResult domain.Release
	createReleaseErr    error

	finishReleaseCtx    context.Context
	finishReleaseParams task.FinishReleaseParams
	finishReleaseResult domain.Release
	finishReleaseErr    error

	inspectTaskID      string
	inspectTaskResult  task.TaskMergeInspection
	mergeTaskID        string
	mergeTaskResult    task.TaskMergeResult
	mergeServiceTask   string
	mergeServiceName   string
	mergeSelection     []task.MRSelection
	updateServiceTask  string
	updateServiceName  string
	updateTarget       string
	updateErr          error
	promoteReleaseID   string
	promoteResult      domain.Release
	retryReleaseCtx    context.Context
	retryReleaseID     string
	retryReleaseResult domain.Release
	retryReleaseErr    error

	forgeCreateMRTitle     string
	forgePipelineStatusArg forgePipelineStatusParams
	forgeListIssuesArgs    forge.ListIssuesParams
	forgeMRResult          task.TaskMRCreateResult
	forgePipelineResult    []forge.PipelineStatus
	forgeIssuesResult      []forge.IssueInfo
	forgeErr               error

	cleanupPlanCtx       context.Context
	cleanupPlanReleaseID string
	cleanupPlanSelection task.ReleaseCleanupSelection
	cleanupPlanResult    task.ReleaseCleanupPlan
	cleanupPlanErr       error
	cleanupExecuteCtx    context.Context
	cleanupExecuteCalls  int
	cleanupExecuteResult task.ReleaseCleanupResult
	cleanupExecuteErr    error

	planTaskMergeCtx         context.Context
	planTaskMergeParams      task.CreateReleaseParams
	planTaskMergeResult      task.ReleaseTaskMergePlan
	planTaskMergeErr         error
	planTaskMergeRetryCtx    context.Context
	planTaskMergeRetryID     string
	planTaskMergeRetryResult task.ReleaseTaskMergePlan
	planTaskMergeRetryErr    error
	retryTaskMergeCtx        context.Context
	retryTaskMergeID         string
	retryTaskMergePlan       *task.ReleaseTaskMergePlan
	retryTaskMergeResult     domain.Release
	retryTaskMergeErr        error
}

func TestConvertHotfixCmd_CallsManager(t *testing.T) {
	mgr := &cmdManager{}
	params := task.ConvertHotfixParams{SourceTaskID: "IN-1", TargetTaskID: "IN-2"}
	msg := convertHotfixCmd(mgr, params, 7)()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("msg = %T, want tea.BatchMsg", msg)
	}

	var done ConvertHotfixDoneMsg
	for _, cmd := range batch {
		if value, ok := cmd().(ConvertHotfixDoneMsg); ok {
			done = value
		}
	}
	if mgr.convertCalls != 1 || mgr.convertParams.SourceTaskID != "IN-1" || mgr.convertParams.TargetTaskID != "IN-2" {
		t.Fatalf("convert call = %d/%+v", mgr.convertCalls, mgr.convertParams)
	}
	if done.SourceTaskID != "IN-1" || done.TargetTaskID != "IN-2" || done.Err != nil {
		t.Fatalf("ConvertHotfixDoneMsg = %#v", done)
	}
}

func (m *cmdManager) ValidateTask(_ context.Context, taskID string) (domain.TaskValidation, error) {
	m.validateTaskID = taskID
	return m.validateResult, m.validateErr
}

func (m *cmdManager) Init(_ context.Context, _ task.InitParams) (task.PartialFailureResult, error) {
	return m.initPartial, m.initErr
}

func (m *cmdManager) Add(_ context.Context, _ task.AddParams) (task.PartialFailureResult, error) {
	return m.addPartial, m.addErr
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

func (m *cmdManager) Remove(_ context.Context, taskID string, _ task.RemoveOptions) error {
	m.removeCalls = append(m.removeCalls, taskID)
	if err, ok := m.removeErrs[taskID]; ok {
		return err
	}
	return nil
}

func (m *cmdManager) ListTags(_ context.Context, taskID string) ([]domain.TagInfo, error) {
	m.tagTaskID = taskID
	m.tagCalls = append(m.tagCalls, taskID)
	return m.tagResult, m.tagErr
}

func (m *cmdManager) ForgeCreateMissingMRs(_ context.Context, _ string, title string, _ bool) (task.TaskMRCreateResult, error) {
	m.forgeCreateMRTitle = title
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

func (m *cmdManager) ListReleases(ctx context.Context) ([]domain.Release, error) {
	m.listReleasesCalled = true
	m.listReleasesCtx = ctx
	return m.listReleasesResult, m.listReleasesErr
}

func (m *cmdManager) GetRelease(_ context.Context, _ string) (domain.Release, error) {
	return domain.Release{}, nil
}

func (m *cmdManager) CreateRelease(ctx context.Context, params task.CreateReleaseParams) (domain.Release, error) {
	m.createReleaseCtx = ctx
	m.createReleaseParams = params
	if params.StatusCh != nil {
		params.StatusCh <- "create release: validating"
		params.StatusCh <- "create release: tagging"
	}
	return m.createReleaseResult, m.createReleaseErr
}

func (m *cmdManager) FinalizeRelease(ctx context.Context, params task.FinishReleaseParams) (domain.Release, error) {
	m.finishReleaseCtx = ctx
	m.finishReleaseParams = params
	return m.finishReleaseResult, m.finishReleaseErr
}

func (m *cmdManager) InspectTaskMerge(_ context.Context, taskID string) (task.TaskMergeInspection, error) {
	m.inspectTaskID = taskID
	return m.inspectTaskResult, nil
}

func (m *cmdManager) MergeTaskMRs(_ context.Context, taskID string) (task.TaskMergeResult, error) {
	m.mergeTaskID = taskID
	return m.mergeTaskResult, nil
}

func (m *cmdManager) MergeServiceMR(_ context.Context, taskID, serviceName string, selection ...task.MRSelection) (task.TaskMergeResult, error) {
	m.mergeSelection = selection
	m.mergeServiceTask = taskID
	m.mergeServiceName = serviceName
	return m.mergeTaskResult, nil
}

func (m *cmdManager) PromoteRelease(_ context.Context, releaseID string, statusCh chan<- string) (domain.Release, error) {
	m.promoteReleaseID = releaseID
	statusCh <- "promoting"
	return m.promoteResult, nil
}

func (m *cmdManager) RetryRelease(ctx context.Context, releaseID string) (domain.Release, error) {
	m.retryReleaseCtx = ctx
	m.retryReleaseID = releaseID
	return m.retryReleaseResult, m.retryReleaseErr
}

func (m *cmdManager) PlanReleaseTaskMerges(ctx context.Context, params task.CreateReleaseParams) (task.ReleaseTaskMergePlan, error) {
	m.planTaskMergeCtx = ctx
	m.planTaskMergeParams = params
	return m.planTaskMergeResult, m.planTaskMergeErr
}

func (m *cmdManager) PlanReleaseTaskMergeRetry(ctx context.Context, releaseID string) (task.ReleaseTaskMergePlan, error) {
	m.planTaskMergeRetryCtx = ctx
	m.planTaskMergeRetryID = releaseID
	return m.planTaskMergeRetryResult, m.planTaskMergeRetryErr
}

func (m *cmdManager) RetryReleaseTaskMerges(ctx context.Context, releaseID string, plan *task.ReleaseTaskMergePlan) (domain.Release, error) {
	m.retryTaskMergeCtx = ctx
	m.retryTaskMergeID = releaseID
	m.retryTaskMergePlan = plan
	return m.retryTaskMergeResult, m.retryTaskMergeErr
}

func (m *cmdManager) RejectRelease(_ context.Context, _ string) (domain.Release, error) {
	return domain.Release{}, nil
}

func (m *cmdManager) RemoveRelease(_ context.Context, _ string) error { return nil }
func (m *cmdManager) PlanReleaseCleanup(ctx context.Context, releaseID string, selection task.ReleaseCleanupSelection) (task.ReleaseCleanupPlan, error) {
	m.cleanupPlanCtx = ctx
	m.cleanupPlanReleaseID = releaseID
	m.cleanupPlanSelection = selection
	return m.cleanupPlanResult, m.cleanupPlanErr
}
func (m *cmdManager) ExecuteReleaseCleanup(ctx context.Context, _ task.ReleaseCleanupPlan, statusCh chan<- string) (task.ReleaseCleanupResult, error) {
	m.cleanupExecuteCtx = ctx
	m.cleanupExecuteCalls++
	statusCh <- "remove release worktree"
	statusCh <- "remove task worktree"
	return m.cleanupExecuteResult, m.cleanupExecuteErr
}

func TestCmdManager_ImplementsTaskManager(t *testing.T) {
	var _ task.Manager = (*cmdManager)(nil)
}

func TestValidateTaskCmdReturnsValidationResult(t *testing.T) {
	mgr := &cmdManager{validateResult: domain.TaskValidation{TaskID: "T14", Blocking: true}}

	msg := validateTaskCmd(mgr, "T14", 7)()
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

	msg := planCloseTaskCmd(mgr, "T14", 7)()
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

	cmd := closeTaskCmd(mgr, task.CloseTaskParams{TaskID: "T14"}, 7)
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

func TestScanCleanupCandidatesCmdReadOnlyAndPreservesIdentity(t *testing.T) {
	mgr := &mockManager{
		listTasksResult: []domain.Task{{ID: "T-1", Phase: "release"}, {ID: "T-2"}},
		listReleasesResult: []domain.Release{
			{ID: "rel-1", Status: domain.ReleaseStatusReleased},
			{ID: "rel-2", Status: domain.ReleaseStatusDraft},
		},
	}

	msg := scanCleanupCandidatesCmd(mgr, 7)()
	got, ok := msg.(CleanupScanReadyMsg)
	if !ok {
		t.Fatalf("msg = %T, want CleanupScanReadyMsg", msg)
	}
	if got.Err != nil {
		t.Fatalf("scan err = %v", got.Err)
	}
	if got.Generation != 7 {
		t.Fatalf("generation = %d, want 7", got.Generation)
	}
	if len(got.Candidates) != 3 {
		t.Fatalf("candidates = %+v, want 3", got.Candidates)
	}
	if got.Candidates[0].Kind != modal.CleanupKindTask || got.Candidates[0].ID != "T-1" {
		t.Fatalf("release-phase task lost task identity: %+v", got.Candidates[0])
	}
	if got.Candidates[1].Kind != modal.CleanupKindTask || got.Candidates[1].ID != "T-2" {
		t.Fatalf("task candidate = %+v", got.Candidates[1])
	}
	if got.Candidates[2].Kind != modal.CleanupKindRelease || got.Candidates[2].ID != "rel-1" {
		t.Fatalf("release candidate = %+v", got.Candidates[2])
	}
	if mgr.cleanupPlanSelection != (task.ReleaseCleanupSelection{RemoveRelease: true}) {
		t.Fatalf("release scan selection = %+v, want release-only", mgr.cleanupPlanSelection)
	}
	if mgr.taskCleanupExecCalls != 0 || mgr.cleanupExecuteCalls != 0 {
		t.Fatal("scan invoked mutation methods")
	}
}

func TestScanCleanupCandidatesCmdListErrorSurfaces(t *testing.T) {
	mgr := &mockManager{listTasksErr: errors.New("disk gone")}

	msg := scanCleanupCandidatesCmd(mgr, 7)()
	got, ok := msg.(CleanupScanReadyMsg)
	if !ok {
		t.Fatalf("msg = %T, want CleanupScanReadyMsg", msg)
	}
	if got.Err == nil {
		t.Fatal("list error not reported")
	}
	if len(got.Candidates) != 0 {
		t.Fatalf("error scan returned candidates: %+v", got.Candidates)
	}
}

func TestScanCleanupCandidatesCmdPlanErrorMarksCandidateBlocked(t *testing.T) {
	mgr := &mockManager{
		listTasksResult:    []domain.Task{{ID: "T-1"}},
		taskCleanupPlanErr: errors.New("planner boom"),
	}

	msg := scanCleanupCandidatesCmd(mgr, 7)()
	got, ok := msg.(CleanupScanReadyMsg)
	if !ok {
		t.Fatalf("msg = %T, want CleanupScanReadyMsg", msg)
	}
	if got.Err != nil {
		t.Fatalf("per-item plan error failed whole scan: %v", got.Err)
	}
	if len(got.Candidates) != 1 || got.Candidates[0].Ready {
		t.Fatalf("candidates = %+v, want one blocked", got.Candidates)
	}
	if !strings.Contains(got.Candidates[0].Reason, "planner boom") {
		t.Fatalf("block reason = %q, want planner error", got.Candidates[0].Reason)
	}
}

func TestListTagsCmdReturnsTagListMsg(t *testing.T) {
	mgr := &cmdManager{tagResult: []domain.TagInfo{{Name: "v1.2.3"}}}

	msg := listTagsCmd(mgr, "T14", 7)()
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

func TestForgeOpCmdDelegatesCreateMissingMRs(t *testing.T) {
	mgr := &cmdManager{forgeMRResult: task.TaskMRCreateResult{TaskID: "T14"}}

	msg := forgeOpCmd(mgr, "create_missing_mrs", "T14", "", forgeCreateMRParams{Title: "Shared title"}, 7)()
	got, ok := msg.(ForgeResultMsg)
	if !ok {
		t.Fatalf("msg = %T, want ForgeResultMsg", msg)
	}
	data, ok := got.Data.(task.TaskMRCreateResult)
	if !ok {
		t.Fatalf("data = %T, want task.TaskMRCreateResult", got.Data)
	}
	if data.TaskID != "T14" {
		t.Fatalf("TaskID = %q, want T14", data.TaskID)
	}
	if mgr.forgeCreateMRTitle != "Shared title" {
		t.Fatalf("title = %q, want Shared title", mgr.forgeCreateMRTitle)
	}
}

func TestForgeOpCmdDelegatesPipelineStatus(t *testing.T) {
	mgr := &cmdManager{forgePipelineResult: []forge.PipelineStatus{{ID: "1"}}}
	params := forgePipelineStatusParams{Branch: "develop", Provider: forge.ForgeProviderGitLab}

	msg := forgeOpCmd(mgr, "pipeline_status", "T14", "svc", params, 7)()
	got := msg.(ForgeResultMsg)
	data, ok := got.Data.([]forge.PipelineStatus)
	if !ok {
		t.Fatalf("data = %T, want []forge.PipelineStatus", got.Data)
	}
	if len(data) != 1 || data[0].ID != "1" {
		t.Fatalf("data = %#v, want [{ID:1}]", data)
	}
	if got.Provider != forge.ForgeProviderGitLab {
		t.Fatalf("Provider = %q, want gitlab", got.Provider)
	}
}

func TestForgeOpCmdDelegatesListIssues(t *testing.T) {
	mgr := &cmdManager{forgeIssuesResult: []forge.IssueInfo{{Number: 7}}}
	params := forge.ListIssuesParams{State: "open"}

	msg := forgeOpCmd(mgr, "list_issues", "T14", "svc", params, 7)()
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

	msg := forgeOpCmd(mgr, "unknown", "T14", "svc", nil, 7)()
	got := msg.(ForgeResultMsg)
	if got.Err == nil {
		t.Fatal("Err = nil, want error")
	}
}

func TestForgeOpCmdManagerWithoutForgeSupport(t *testing.T) {
	mgr := &cmdManager{forgeErr: errors.New("forge unavailable")}
	msg := forgeOpCmd(mgr, "create_missing_mrs", "T14", "", forgeCreateMRParams{Title: "title"}, 7)()
	got := msg.(ForgeResultMsg)
	if got.Err == nil {
		t.Fatal("Err = nil, want error")
	}
}

func TestLoadReleasesCmdReturnsReleasesLoadedMsgAndUsesTimeout(t *testing.T) {
	now := time.Now()
	mgr := &cmdManager{listReleasesResult: []domain.Release{{ID: "rel-1", CreatedAt: now}}}

	msg := loadReleasesCmd(mgr)()
	got, ok := msg.(ReleasesLoadedMsg)
	if !ok {
		t.Fatalf("msg = %T, want ReleasesLoadedMsg", msg)
	}
	if len(got.Releases) != 1 || got.Releases[0].ID != "rel-1" {
		t.Fatalf("Releases = %#v, want one rel-1", got.Releases)
	}
	if got.Err != nil {
		t.Fatalf("Err = %v, want nil", got.Err)
	}
	if !mgr.listReleasesCalled {
		t.Fatal("ListReleases not called")
	}
	deadline, ok := mgr.listReleasesCtx.Deadline()
	if !ok {
		t.Fatal("ListReleases ctx has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining > 30*time.Second || remaining < 29*time.Second {
		t.Fatalf("ListReleases timeout ~30s, got remaining=%s", remaining)
	}
}

func TestLoadReleasesCmdReturnsErrorInMessage(t *testing.T) {
	expectedErr := errors.New("boom")
	mgr := &cmdManager{listReleasesErr: expectedErr}

	msg := loadReleasesCmd(mgr)()
	got := msg.(ReleasesLoadedMsg)
	if !errors.Is(got.Err, expectedErr) {
		t.Fatalf("Err = %v, want %v", got.Err, expectedErr)
	}
}

func TestPlanReleaseCleanupCmdCarriesSelectionAndGeneration(t *testing.T) {
	selection := task.DefaultReleaseCleanupSelection()
	mgr := &cmdManager{}
	msg := planReleaseCleanupCmd(mgr, "rel-1", selection, 9)()
	ready, ok := msg.(ReleaseCleanupPlanReadyMsg)
	if !ok {
		t.Fatalf("msg = %T, want ReleaseCleanupPlanReadyMsg", msg)
	}
	if ready.Generation != 9 || mgr.cleanupPlanReleaseID != "rel-1" || mgr.cleanupPlanSelection != selection {
		t.Fatalf("ready = %+v, manager ID=%q selection=%+v", ready, mgr.cleanupPlanReleaseID, mgr.cleanupPlanSelection)
	}
	deadline, ok := mgr.cleanupPlanCtx.Deadline()
	if !ok || time.Until(deadline) > 2*time.Minute || time.Until(deadline) < 119*time.Second {
		t.Fatalf("planning deadline = %v, ok=%v", deadline, ok)
	}
}

func TestExecuteReleaseCleanupCmdStreamsAndReturnsGeneration(t *testing.T) {
	mgr := &cmdManager{cleanupExecuteResult: task.ReleaseCleanupResult{ReleaseID: "rel-1"}}
	cmd := executeReleaseCleanupCmd(mgr, task.ReleaseCleanupPlan{}, "rel-1", 12)

	first, ok := cmd().(OutputLineMsg)
	if !ok || first.Line != "remove release worktree" {
		t.Fatalf("first = %#v", first)
	}
	second, ok := first.Next().(OutputLineMsg)
	if !ok || second.Line != "remove task worktree" {
		t.Fatalf("second = %#v", second)
	}
	done, ok := second.Next().(ReleaseCleanupDoneMsg)
	if !ok || done.Generation != 12 || done.Result.ReleaseID != "rel-1" || done.Err != nil {
		t.Fatalf("done = %#v", done)
	}
	if mgr.cleanupExecuteCalls != 1 {
		t.Fatalf("execute calls = %d", mgr.cleanupExecuteCalls)
	}
	deadline, ok := mgr.cleanupExecuteCtx.Deadline()
	if !ok || time.Until(deadline) > 10*time.Minute || time.Until(deadline) < 9*time.Minute+59*time.Second {
		t.Fatalf("execution deadline = %v, ok=%v", deadline, ok)
	}
}

func TestPlanTaskCleanupCmdReturnsReadyMsg(t *testing.T) {
	mgr := &cmdManager{}
	msg := planTaskCleanupCmd(mgr, "TASK-9", 4, 11)()
	ready, ok := msg.(TaskCleanupPlanReadyMsg)
	if !ok {
		t.Fatalf("msg = %T, want TaskCleanupPlanReadyMsg", msg)
	}
	if ready.TaskID != "TASK-9" || ready.Generation != 4 || ready.OperationGeneration != 11 || ready.Err != nil {
		t.Fatalf("ready = %+v", ready)
	}
	if len(mgr.taskCleanupPlanTaskIDs) != 1 || mgr.taskCleanupPlanTaskIDs[0] != "TASK-9" {
		t.Fatalf("plan calls = %v", mgr.taskCleanupPlanTaskIDs)
	}
}

func TestExecuteTaskCleanupCmdStreamsAndReturnsGeneration(t *testing.T) {
	mgr := &cmdManager{}
	mgr.taskCleanupExecStatuses = []string{"remove task worktree w1"}
	mgr.taskCleanupExecResult = task.TaskCleanupResult{TaskID: "TASK-9", Completed: []string{"remove task worktree w1"}}
	cmd := executeTaskCleanupCmd(mgr, task.TaskCleanupPlan{}, "TASK-9", 7, 12)

	first, ok := cmd().(OutputLineMsg)
	if !ok || first.Line != "remove task worktree w1" {
		t.Fatalf("first = %#v", first)
	}
	done, ok := first.Next().(TaskCleanupDoneMsg)
	if !ok || done.TaskID != "TASK-9" || done.Generation != 7 || done.OperationGeneration != 12 || done.Result.TaskID != "TASK-9" || done.Err != nil {
		t.Fatalf("done = %#v", done)
	}
	if len(done.Result.Completed) != 1 {
		t.Fatalf("completed = %v", done.Result.Completed)
	}
	if mgr.taskCleanupExecCalls != 1 {
		t.Fatalf("execute calls = %d", mgr.taskCleanupExecCalls)
	}
}

func TestExecuteReleaseCleanupCmdStampsConfirmedReleaseIDOnError(t *testing.T) {
	mgr := &cmdManager{cleanupExecuteErr: errors.New("boom")}
	cmd := executeReleaseCleanupCmd(mgr, task.ReleaseCleanupPlan{}, "rel-1", 12)

	msg := cmd()
	for {
		line, ok := msg.(OutputLineMsg)
		if !ok {
			break
		}
		msg = line.Next()
	}
	done, ok := msg.(ReleaseCleanupDoneMsg)
	if !ok {
		t.Fatalf("msg = %T, want ReleaseCleanupDoneMsg", msg)
	}
	if done.Result.ReleaseID != "rel-1" {
		t.Fatalf("done result identity = %q, want rel-1", done.Result.ReleaseID)
	}
	if done.Err == nil {
		t.Fatal("error not propagated")
	}
}

func TestExecuteTaskCleanupCmdStampsConfirmedTaskIDOnError(t *testing.T) {
	mgr := &cmdManager{}
	mgr.taskCleanupExecErr = errors.New("boom")
	cmd := executeTaskCleanupCmd(mgr, task.TaskCleanupPlan{}, "TASK-9", 7, 12)

	msg := cmd()
	for {
		line, ok := msg.(OutputLineMsg)
		if !ok {
			break
		}
		msg = line.Next()
	}
	done, ok := msg.(TaskCleanupDoneMsg)
	if !ok {
		t.Fatalf("msg = %T, want TaskCleanupDoneMsg", msg)
	}
	if done.TaskID != "TASK-9" || done.Result.TaskID != "TASK-9" {
		t.Fatalf("done identity = (%q, %q), want TASK-9", done.TaskID, done.Result.TaskID)
	}
	if done.Err == nil {
		t.Fatal("error not propagated")
	}
}

func TestCreateReleaseCmdStreamsStatusAndReturnsDone(t *testing.T) {
	expected := domain.Release{ID: "rel-1", Status: domain.ReleaseStatusReleased}
	mgr := &cmdManager{createReleaseResult: expected}

	cmd := createReleaseCmd(mgr, task.CreateReleaseParams{TaskIDs: []string{"T-1"}}, 5)

	msg1 := cmd()
	line1, ok := msg1.(OutputLineMsg)
	if !ok {
		t.Fatalf("msg1 = %T, want OutputLineMsg", msg1)
	}
	if line1.Line != "create release: validating" {
		t.Fatalf("line1 = %q, want first status line", line1.Line)
	}

	msg2 := line1.Next()
	line2, ok := msg2.(OutputLineMsg)
	if !ok {
		t.Fatalf("msg2 = %T, want OutputLineMsg", msg2)
	}
	if line2.Line != "create release: tagging" {
		t.Fatalf("line2 = %q, want second status line", line2.Line)
	}

	msg3 := line2.Next()
	done, ok := msg3.(CreateReleaseDoneMsg)
	if !ok {
		t.Fatalf("msg3 = %T, want CreateReleaseDoneMsg", msg3)
	}
	if done.Release.ID != "rel-1" {
		t.Fatalf("done.Release.ID = %q, want rel-1", done.Release.ID)
	}
	if done.Err != nil {
		t.Fatalf("done.Err = %v, want nil", done.Err)
	}
	if mgr.createReleaseParams.StatusCh == nil {
		t.Fatal("CreateRelease params.StatusCh = nil, want non-nil")
	}
	deadline, ok := mgr.createReleaseCtx.Deadline()
	if !ok {
		t.Fatal("CreateRelease ctx has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining > 10*time.Minute || remaining < 9*time.Minute+59*time.Second {
		t.Fatalf("CreateRelease timeout ~10m, got remaining=%s", remaining)
	}
}

func TestCreateReleaseCmdReturnsErrorInMessage(t *testing.T) {
	expectedErr := errors.New("create failed")
	mgr := &cmdManager{createReleaseErr: expectedErr}

	cmd := createReleaseCmd(mgr, task.CreateReleaseParams{TaskIDs: []string{"T-1"}}, 3)
	msg := cmd()
	line, ok := msg.(OutputLineMsg)
	if !ok {
		t.Fatalf("msg = %T, want OutputLineMsg", msg)
	}
	msg = line.Next()
	line, ok = msg.(OutputLineMsg)
	if !ok {
		t.Fatalf("msg = %T, want OutputLineMsg", msg)
	}
	msg = line.Next()
	done := msg.(CreateReleaseDoneMsg)
	if done.Generation != 3 {
		t.Fatalf("done.Generation = %d, want 3", done.Generation)
	}
	if !errors.Is(done.Err, expectedErr) {
		t.Fatalf("Err = %v, want %v", done.Err, expectedErr)
	}
}

func TestFinalizeReleaseCmdReturnsDoneMsg(t *testing.T) {
	expected := domain.Release{ID: "rel-1", Status: domain.ReleaseStatusPrepared}
	mgr := &cmdManager{finishReleaseResult: expected}

	cmd := finalizeReleaseCmd(mgr, "rel-1", 11)

	msg2 := cmd()
	done, ok := msg2.(ReleaseActionDoneMsg)
	if !ok {
		t.Fatalf("msg2 = %T, want ReleaseActionDoneMsg", msg2)
	}
	if done.Action != "finalize" {
		t.Fatalf("action = %q, want finalize", done.Action)
	}
	if done.Generation != 11 {
		t.Fatalf("done.Generation = %d, want 11", done.Generation)
	}
	if done.Release.ID != "rel-1" {
		t.Fatalf("done.Release.ID = %q, want rel-1", done.Release.ID)
	}
	if done.Err != nil {
		t.Fatalf("done.Err = %v, want nil", done.Err)
	}
	if mgr.finishReleaseCtx == nil {
		t.Fatal("FinishRelease ctx = nil, want non-nil")
	}
	if mgr.finishReleaseParams.ReleaseID != "rel-1" {
		t.Fatalf("FinishRelease params.ReleaseID = %q, want rel-1", mgr.finishReleaseParams.ReleaseID)
	}
	if mgr.finishReleaseParams.StatusCh == nil {
		t.Fatal("FinishRelease params.StatusCh = nil, want non-nil")
	}
	deadline, ok := mgr.finishReleaseCtx.Deadline()
	if !ok {
		t.Fatal("FinishRelease ctx has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining > 10*time.Minute || remaining < 9*time.Minute+59*time.Second {
		t.Fatalf("FinishRelease timeout ~10m, got remaining=%s", remaining)
	}
}

func TestRetryReleaseCmdReturnsDoneMsg(t *testing.T) {
	expected := domain.Release{ID: "rel-1", Status: domain.ReleaseStatusPrepared}
	mgr := &cmdManager{retryReleaseResult: expected}

	done, ok := retryReleaseCmd(mgr, "rel-1", 13)().(ReleaseActionDoneMsg)
	if !ok {
		t.Fatalf("message type = %T, want ReleaseActionDoneMsg", done)
	}
	if done.Action != "retry" || done.Release.ID != "rel-1" || done.Err != nil {
		t.Fatalf("done = %#v", done)
	}
	if done.Generation != 13 {
		t.Fatalf("done.Generation = %d, want 13", done.Generation)
	}
	if mgr.retryReleaseID != "rel-1" || mgr.retryReleaseCtx == nil {
		t.Fatalf("RetryRelease called with id=%q ctx=%v", mgr.retryReleaseID, mgr.retryReleaseCtx)
	}
	deadline, ok := mgr.retryReleaseCtx.Deadline()
	if !ok {
		t.Fatal("RetryRelease ctx has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining > 10*time.Minute || remaining < 9*time.Minute+59*time.Second {
		t.Fatalf("RetryRelease timeout ~10m, got remaining=%s", remaining)
	}
}

func TestInspectAndMergeTaskCommandsPreserveTaskID(t *testing.T) {
	mgr := &cmdManager{
		inspectTaskResult: task.TaskMergeInspection{TaskID: "TASK-1"},
		mergeTaskResult:   task.TaskMergeResult{TaskID: "TASK-1", Merged: []string{"api"}},
	}

	inspection := inspectTaskMergeCmd(mgr, "TASK-1", 7)().(TaskMergeInspectionMsg)
	if inspection.Inspection.TaskID != "TASK-1" || mgr.inspectTaskID != "TASK-1" {
		t.Fatalf("inspection = %#v, called with %q", inspection, mgr.inspectTaskID)
	}
	if inspection.Generation != 7 {
		t.Fatalf("generation = %d, want 7", inspection.Generation)
	}
	merged := mergeTaskMRsCmd(mgr, "TASK-1", 8)().(TaskMergeDoneMsg)
	if len(merged.Result.Merged) != 1 || mgr.mergeTaskID != "TASK-1" {
		t.Fatalf("merge = %#v, called with %q", merged, mgr.mergeTaskID)
	}
	if merged.Generation != 8 {
		t.Fatalf("merged.Generation = %d, want 8", merged.Generation)
	}
}

func TestMergeServiceMRCmdPreservesTaskAndService(t *testing.T) {
	mgr := &cmdManager{mergeTaskResult: task.TaskMergeResult{TaskID: "TASK-1", Merged: []string{"api"}}}

	merged := mergeServiceMRCmd(mgr, "TASK-1", "api", 9)().(TaskMergeDoneMsg)
	if len(merged.Result.Merged) != 1 || mgr.mergeServiceTask != "TASK-1" || mgr.mergeServiceName != "api" {
		t.Fatalf("merge = %#v, called with task=%q service=%q", merged, mgr.mergeServiceTask, mgr.mergeServiceName)
	}
	if merged.Generation != 9 {
		t.Fatalf("merged.Generation = %d, want 9", merged.Generation)
	}
}

func TestPromoteReleaseCmdStreamsStatusAndReturnsDone(t *testing.T) {
	mgr := &cmdManager{promoteResult: domain.Release{ID: "rel-1"}}
	msg := promoteReleaseCmd(mgr, "rel-1", 21)()
	line, ok := msg.(OutputLineMsg)
	if !ok || line.Line != "promoting" {
		t.Fatalf("first message = %#v, want promoting output", msg)
	}
	done, ok := line.Next().(ReleaseActionDoneMsg)
	if !ok || done.Action != "promote" || done.Release.ID != "rel-1" || mgr.promoteReleaseID != "rel-1" {
		t.Fatalf("done = %#v, release ID call = %q", done, mgr.promoteReleaseID)
	}
}

func TestLoadReleaseVersionsCmdUsesManagerProposals(t *testing.T) {
	mgr := &cmdManager{}
	mgr.proposedVersions = map[string]string{"api": "1.2.4", "worker": "2.0.1"}

	msg := loadReleaseVersionsCmd(mgr, []string{"T-1", "T-2"}, 7)()
	loaded, ok := msg.(panels.ReleaseVersionsLoadedMsg)
	if !ok {
		t.Fatalf("msg = %T, want panels.ReleaseVersionsLoadedMsg", msg)
	}
	if got := loaded.Versions["api"]; got != "1.2.4" {
		t.Fatalf("api version = %q, want 1.2.4", got)
	}
	if got := loaded.Versions["worker"]; got != "2.0.1" {
		t.Fatalf("worker version = %q, want 2.0.1", got)
	}
	if !slices.Equal(mgr.proposedVersionIDs, []string{"T-1", "T-2"}) {
		t.Fatalf("ProposeReleaseVersions task IDs = %+v", mgr.proposedVersionIDs)
	}
}

func TestLoadReleaseVersionsCmdReturnsCommandDoneOnManagerError(t *testing.T) {
	expectedErr := errors.New("versions failed")
	mgr := &cmdManager{}
	mgr.proposedVersionErr = expectedErr

	msg := loadReleaseVersionsCmd(mgr, []string{"T-1"}, 7)()
	done, ok := msg.(CommandDoneMsg)
	if !ok {
		t.Fatalf("msg = %T, want CommandDoneMsg", msg)
	}
	if !errors.Is(done.Err, expectedErr) {
		t.Fatalf("Err = %v, want %v", done.Err, expectedErr)
	}
	if done.Op != "Load release versions" {
		t.Fatalf("Op = %q, want release versions op", done.Op)
	}
}

func TestInitTaskCmd_PartialFailureEmitsPartialInitDoneMsg(t *testing.T) {
	mgr := &cmdManager{
		initPartial: task.PartialFailureResult{
			TaskID:            "T-1",
			Operation:         "init",
			RequestedCount:    2,
			SucceededServices: []string{"svc-a"},
			FailedServices: []task.FailedService{{
				Name:  "svc-b",
				Cause: errors.New("boom"),
			}},
			Retryable: true,
		},
		initErr: errors.New("partial failure"),
	}

	msg := initTaskCmd(mgr, task.InitParams{TaskID: "T-1"}, 7)()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("msg = %T, want tea.BatchMsg", msg)
	}

	var partial PartialInitDoneMsg
	var found bool
	for _, cmd := range batch {
		if cmd == nil {
			continue
		}
		if m, ok := cmd().(PartialInitDoneMsg); ok {
			partial = m
			found = true
			break
		}
	}

	if !found {
		t.Fatal("PartialInitDoneMsg not found")
	}
	if partial.Result.TaskID != "T-1" || partial.Op != "Init task T-1" {
		t.Fatalf("partial msg = %+v, want task/op set", partial)
	}
}

func TestAddServiceCmd_PartialFailureEmitsPartialAddDoneMsg(t *testing.T) {
	mgr := &cmdManager{
		addPartial: task.PartialFailureResult{
			TaskID:            "T-2",
			Operation:         "add",
			RequestedCount:    2,
			SucceededServices: []string{"svc-a"},
			FailedServices: []task.FailedService{{
				Name:  "svc-b",
				Cause: errors.New("boom"),
			}},
			Retryable: true,
		},
		addErr: errors.New("partial failure"),
	}

	msg := addServiceCmd(mgr, task.AddParams{TaskID: "T-2"}, 7)()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("msg = %T, want tea.BatchMsg", msg)
	}

	var partial PartialAddDoneMsg
	var found bool
	for _, cmd := range batch {
		if cmd == nil {
			continue
		}
		if m, ok := cmd().(PartialAddDoneMsg); ok {
			partial = m
			found = true
			break
		}
	}

	if !found {
		t.Fatal("PartialAddDoneMsg not found")
	}
	if partial.Result.TaskID != "T-2" || partial.Op != "Add services to T-2" {
		t.Fatalf("partial msg = %+v, want task/op set", partial)
	}
}

func TestPlanReleaseTaskMergesCmdCarriesParamsAndGeneration(t *testing.T) {
	plan := task.ReleaseTaskMergePlan{Rows: []task.ReleaseTaskMergeRow{{ServiceName: "api", Ready: true}}}
	mgr := &cmdManager{planTaskMergeResult: plan}

	msg := planReleaseTaskMergesCmd(mgr, task.CreateReleaseParams{TaskIDs: []string{"T-1"}, ServiceVersions: map[string]string{"api": "1.2.3"}}, 7)()
	ready, ok := msg.(ReleaseTaskMergePlanReadyMsg)
	if !ok {
		t.Fatalf("msg = %T, want ReleaseTaskMergePlanReadyMsg", msg)
	}
	if ready.Generation != 7 || ready.Err != nil || len(ready.Plan.Rows) != 1 {
		t.Fatalf("ready = %+v", ready)
	}
	if len(mgr.planTaskMergeParams.TaskIDs) != 1 || mgr.planTaskMergeParams.TaskIDs[0] != "T-1" {
		t.Fatalf("plan params = %+v", mgr.planTaskMergeParams)
	}
}

func TestPlanReleaseTaskMergesCmdReturnsError(t *testing.T) {
	expectedErr := errors.New("plan failed")
	mgr := &cmdManager{planTaskMergeErr: expectedErr}

	msg := planReleaseTaskMergesCmd(mgr, task.CreateReleaseParams{TaskIDs: []string{"T-1"}}, 3)()
	ready := msg.(ReleaseTaskMergePlanReadyMsg)
	if !errors.Is(ready.Err, expectedErr) {
		t.Fatalf("Err = %v, want %v", ready.Err, expectedErr)
	}
}

func TestPlanReleaseTaskMergeRetryCmdCarriesReleaseAndGeneration(t *testing.T) {
	plan := task.ReleaseTaskMergePlan{Rows: []task.ReleaseTaskMergeRow{{ServiceName: "api", Status: "merged", Ready: true}}}
	mgr := &cmdManager{planTaskMergeRetryResult: plan}

	msg := planReleaseTaskMergeRetryCmd(mgr, "rel-1", 11)()
	ready, ok := msg.(ReleaseTaskMergeRetryPlanReadyMsg)
	if !ok {
		t.Fatalf("msg = %T, want ReleaseTaskMergeRetryPlanReadyMsg", msg)
	}
	if ready.ReleaseID != "rel-1" || ready.Generation != 11 || len(ready.Plan.Rows) != 1 {
		t.Fatalf("ready = %+v", ready)
	}
	if mgr.planTaskMergeRetryID != "rel-1" {
		t.Fatalf("retry plan release ID = %q", mgr.planTaskMergeRetryID)
	}
}

func TestRetryReleaseTaskMergesCmdPassesExactPlan(t *testing.T) {
	mgr := &cmdManager{retryTaskMergeResult: domain.Release{ID: "rel-1", Status: domain.ReleaseStatusPrepared}}
	plan := &task.ReleaseTaskMergePlan{Rows: []task.ReleaseTaskMergeRow{{ServiceName: "api", Ready: true}}}

	done, ok := retryReleaseTaskMergesCmd(mgr, "rel-1", plan, 17)().(ReleaseActionDoneMsg)
	if !ok {
		t.Fatalf("message type unexpected")
	}
	if done.Action != "retry" || done.Release.ID != "rel-1" || done.Err != nil {
		t.Fatalf("done = %#v", done)
	}
	if done.Generation != 17 {
		t.Fatalf("done.Generation = %d, want 17", done.Generation)
	}
	if mgr.retryTaskMergeID != "rel-1" || mgr.retryTaskMergePlan != plan {
		t.Fatalf("RetryReleaseTaskMerges called with id=%q plan=%p, want id=rel-1 exact plan", mgr.retryTaskMergeID, mgr.retryTaskMergePlan)
	}
}

// runStatusBatch executes a status-channel command batch and returns the
// CommandDoneMsg plus whether the reader observed a drained (closed) channel.
func runStatusBatch(t *testing.T, cmd tea.Cmd) (CommandDoneMsg, bool) {
	t.Helper()
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("msg = %T, want tea.BatchMsg", batch)
	}
	var done CommandDoneMsg
	drained := false
	for _, sub := range batch {
		switch v := sub().(type) {
		case CommandDoneMsg:
			done = v
		case channelDrainedMsg:
			drained = true
		}
	}
	return done, drained
}

func TestSyncTaskCmd_ClosesChannelAndReaderDrains(t *testing.T) {
	mgr := &cmdManager{}
	done, drained := runStatusBatch(t, syncTaskCmd(mgr, "IN-1", task.SyncStrategyRebase, 7))
	if done.Err != nil || done.Op != "Sync task IN-1" {
		t.Fatalf("done = %#v", done)
	}
	if mgr.syncTaskTaskID != "IN-1" || mgr.syncTaskStrategy != task.SyncStrategyRebase {
		t.Fatalf("SyncTask called with id=%q strategy=%v", mgr.syncTaskTaskID, mgr.syncTaskStrategy)
	}
	if !drained {
		t.Fatal("status channel was not closed by syncTaskCmd; reader did not drain")
	}
}

func TestSyncTaskCmd_ErrorPathStillClosesChannel(t *testing.T) {
	mgr := &cmdManager{}
	mgr.syncTaskErr = errors.New("sync failed")
	done, drained := runStatusBatch(t, syncTaskCmd(mgr, "IN-1", task.SyncStrategyMerge, 7))
	if done.Err == nil {
		t.Fatal("done.Err = nil, want sync error")
	}
	if !drained {
		t.Fatal("status channel was not closed on error path")
	}
}

func TestSyncServiceCmd_ClosesChannelAndReaderDrains(t *testing.T) {
	mgr := &cmdManager{}
	done, drained := runStatusBatch(t, syncServiceCmd(mgr, "IN-1", "api", task.SyncStrategyMerge, 7))
	if done.Err != nil || done.Op != "Sync service api" {
		t.Fatalf("done = %#v", done)
	}
	if !drained {
		t.Fatal("status channel was not closed by syncServiceCmd; reader did not drain")
	}
}

func TestSyncServiceCmd_ErrorPathStillClosesChannel(t *testing.T) {
	mgr := &cmdManager{}
	mgr.syncServiceErr = errors.New("sync failed")
	done, drained := runStatusBatch(t, syncServiceCmd(mgr, "IN-1", "api", task.SyncStrategyMerge, 7))
	if done.Err == nil {
		t.Fatal("done.Err = nil, want sync error")
	}
	if !drained {
		t.Fatal("status channel was not closed on error path")
	}
}

func TestPushTaskCmd_ClosesChannelAndReaderDrains(t *testing.T) {
	mgr := &cmdManager{}
	done, drained := runStatusBatch(t, pushTaskCmd(mgr, "IN-1", 7))
	if done.Err != nil || done.Op != "Push task IN-1" {
		t.Fatalf("done = %#v", done)
	}
	if mgr.pushTaskID != "IN-1" {
		t.Fatalf("PushTask called with id=%q", mgr.pushTaskID)
	}
	if !drained {
		t.Fatal("status channel was not closed by pushTaskCmd; reader did not drain")
	}
}

func TestPushTaskCmd_ErrorPathStillClosesChannel(t *testing.T) {
	mgr := &cmdManager{}
	mgr.pushTaskErr = errors.New("push failed")
	done, drained := runStatusBatch(t, pushTaskCmd(mgr, "IN-1", 7))
	if done.Err == nil {
		t.Fatal("done.Err = nil, want push error")
	}
	if !drained {
		t.Fatal("status channel was not closed on error path")
	}
}
