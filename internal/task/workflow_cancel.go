package task

import (
	"context"
	"errors"
	"fmt"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/shell"
)

// TaskWorkflowCancelled identifies a cancellation with a saved retry state.
type TaskWorkflowCancelled struct {
	State string
	cause error
}

func (e *TaskWorkflowCancelled) Error() string { return "workflow cancelled in " + e.State }
func (e *TaskWorkflowCancelled) Unwrap() error { return e.cause }

func (m *manager) saveReleaseInterruption(release domain.Release, err error) (domain.Release, error) {
	var stopErr *shell.HookStopError
	releaseErr := &domain.ReleaseError{
		Code: "ERR_RELEASE_INTERRUPTED", Message: "release operation interrupted",
		Stage: release.State, Cause: err.Error(), Recoverable: !errors.As(err, &stopErr),
	}
	if saveErr := m.failRelease(&release, releaseErr); saveErr != nil {
		return domain.Release{}, errors.Join(err, fmt.Errorf("save interrupted release: %w", saveErr))
	}
	return release, err
}

func (m *manager) saveTaskInterruption(tf *taskFlow, err error) error {
	var stopErr *shell.HookStopError
	tf.state.HookStopUnconfirmed = errors.As(err, &stopErr)
	if saveErr := m.saveTaskFlow(tf); saveErr != nil {
		return errors.Join(err, fmt.Errorf("save interrupted task workflow: %w", saveErr))
	}
	if errors.Is(err, context.Canceled) {
		return &TaskWorkflowCancelled{State: tf.state.State, cause: err}
	}
	return err
}

func workflowInterrupted(err error) bool {
	var stopErr *shell.HookStopError
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.As(err, &stopErr)
}
