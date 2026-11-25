package task

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/D1ssolve/wtui/internal/fsutil"
)

func (m *manager) lockRelease(ctx context.Context, id string) (func(), error) {
	if err := validateReleaseID(id); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrReleaseNotFound, err)
	}
	unlock, err := lockState(ctx, m.releasesRootDir(), "release", id)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrReleaseNotFound, id)
	}
	return unlock, err
}

func (m *manager) lockTask(ctx context.Context, id string) (func(), error) {
	if err := validateTaskID(id); err != nil {
		return nil, err
	}
	// Do not leave a permanent sidecar for a nonexistent task. The workflow
	// itself is still loaded and validated only after acquisition.
	if _, err := os.Stat(m.taskDir(id)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrTaskNotFound, id)
		}
		return nil, err
	}
	unlock, err := lockState(ctx, m.cfg.TasksRoot, "task", id)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrTaskNotFound, id)
	}
	return unlock, err
}

// Sibling files survive removal/recreation of the directory they guard. Scans
// enumerate directories only, and '..' cannot occur in a valid task/release ID.
func lockState(ctx context.Context, root, kind, id string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	unlock, err := fsutil.Lock(filepath.Join(root, ".wtui-"+kind+".."+id))
	var holder *fsutil.LockedError
	if errors.As(err, &holder) {
		return nil, &stateBusyError{kind, id, holder}
	}
	return unlock, err
}

type stateBusyError struct {
	kind, id string
	holder   *fsutil.LockedError
}

func (e *stateBusyError) Error() string {
	return fmt.Sprintf("%s %s is being changed by another wtui (pid %d); wait for it or close it", e.kind, e.id, e.holder.PID)
}

func (e *stateBusyError) Unwrap() error { return e.holder }
