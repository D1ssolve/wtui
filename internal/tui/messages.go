package tui

import (
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
)

type ValidationResultMsg struct {
	Generation uint64
	Validation domain.TaskValidation
}

type ClosePlanReadyMsg struct {
	Generation uint64
	Plan       task.ClosePlan
	Err        error
}

type CloseTaskFinishedMsg struct {
	Generation uint64
	Result     task.CloseTaskResult
	DryRun     bool
	Err        error
}

type ConvertHotfixDoneMsg struct {
	Generation   uint64
	SourceTaskID string
	TargetTaskID string
	Err          error
}

type CleanupScanReadyMsg struct {
	Generation uint64
	Candidates []modal.CleanupCandidate
	Err        error
}

type TagListMsg struct {
	Generation uint64
	TaskID     string
	Tags       []domain.TagInfo
	Err        error
}

type ForgeResultMsg struct {
	Generation  uint64
	TaskID      string
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
	Generation uint64
	Release    domain.Release
	Err        error
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
	Generation uint64
	Result     task.TaskMergeResult
	Err        error
}

type ReleaseMergeDoneMsg struct {
	Generation uint64
	Release    domain.Release
	Result     task.ReleaseMergeResult
	Err        error
}

type ReleaseActionDoneMsg struct {
	Generation uint64
	Action     string
	Release    domain.Release
	Err        error
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
	Err        error
}

type ReleaseTaskMergePlanReadyMsg struct {
	Generation uint64
	Plan       task.ReleaseTaskMergePlan
	Err        error
}

type ReleaseTaskMergeRetryPlanReadyMsg struct {
	ReleaseID  string
	Generation uint64
	Plan       task.ReleaseTaskMergePlan
	Err        error
}

type ReleaseCleanupDoneMsg struct {
	Generation uint64
	Result     task.ReleaseCleanupResult
	Err        error
}

type TaskCleanupPlanReadyMsg struct {
	TaskID              string
	Generation          uint64
	OperationGeneration uint64
	Plan                task.TaskCleanupPlan
	Err                 error
}

type TaskCleanupDoneMsg struct {
	TaskID              string
	Generation          uint64
	OperationGeneration uint64
	Result              task.TaskCleanupResult
	Err                 error
}
