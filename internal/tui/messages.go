package tui

import (
	"github.com/D1ssolve/wtui/internal/domain"
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

type FinishReleaseDoneMsg struct {
	Release domain.Release
	Err     error
}
