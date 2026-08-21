package tui

import (
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/task"
)

type ValidationResultMsg struct {
	Validation domain.TaskValidation
}

type ClosePlanReadyMsg struct {
	Plan task.ClosePlan
	Err  error
}

type CloseTaskFinishedMsg struct {
	Result task.CloseTaskResult
	Err    error
}

type ConvertHotfixDoneMsg struct {
	SourceTaskID string
	TargetTaskID string
	Err          error
}

type PrunePlanReadyMsg struct {
	Candidates []domain.PruneCandidate
	Err        error
}

type PruneFinishedMsg struct {
	Removed []string
	Errors  []error
}

type TagListMsg struct {
	TaskID string
	Tags   []domain.TagInfo
	Err    error
}

type ForgeResultMsg struct {
	ServiceName string
	Op          string
	Provider    forge.ForgeProvider
	Data        any
	Err         error
}

type ReleasesLoadedMsg struct {
	Releases []domain.Release
	Err      error
}

type CreateReleaseDoneMsg struct {
	Release domain.Release
	Err     error
}

type TaskMergeInspectionMsg struct {
	TaskID     string
	Generation uint64
	Inspection task.TaskMergeInspection
	Err        error
}

type ReleaseMergeInspectionMsg struct {
	ReleaseID  string
	Generation uint64
	Inspection task.ReleaseMergeInspection
	Err        error
}

type TaskMergeDoneMsg struct {
	Result task.TaskMergeResult
	Err    error
}

type ReleaseMergeDoneMsg struct {
	Release domain.Release
	Result  task.ReleaseMergeResult
	Err     error
}

type ReleaseActionDoneMsg struct {
	Action  string
	Release domain.Release
	Err     error
}

type TaskWorkflowLoadedMsg struct {
	TaskID     string
	Generation uint64
	Workflow   domain.WorkflowSummary
	Err        error
}

type ReleaseCleanupPlanReadyMsg struct {
	Generation uint64
	Plan       task.ReleaseCleanupPlan
	Preview    task.ReleaseCleanupPreview
	Err        error
}

type ReleaseCleanupDoneMsg struct {
	Generation uint64
	Result     task.ReleaseCleanupResult
	Err        error
}
