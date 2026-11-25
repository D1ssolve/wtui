package task

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

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

// Order across namespaces: membership reservations, release, then task IDs in sorted order.
// New tasks and conversion targets need a guard even before their directory exists.
func lockTaskIDs(ctx context.Context, root, kind string, ids []string) (func(), error) {
	ordered := slices.Clone(ids)
	slices.Sort(ordered)
	ordered = slices.Compact(ordered)
	for _, id := range ordered {
		if err := validateTaskID(id); err != nil {
			return nil, err
		}
	}
	if len(ordered) > 0 {
		if err := os.MkdirAll(root, 0755); err != nil {
			return nil, err
		}
	}
	var held []func()
	unlock := func() {
		for _, release := range slices.Backward(held) {
			release()
		}
	}
	for _, id := range ordered {
		u, err := lockState(ctx, root, kind, id)
		if err != nil {
			unlock()
			return nil, err
		}
		held = append(held, u)
	}
	return unlock, nil
}

func (m *manager) reserveMembership(ctx context.Context, ids []string) (func(), error) {
	ids, err := sanitizeReleaseTaskIDs(ids)
	if err != nil {
		return nil, err
	}
	return lockTaskIDs(ctx, m.releasesRootDir(), "membership", ids)
}

// Sibling files survive removal/recreation of the directory they guard. Scans
// enumerate directories only, and '..' cannot occur in a valid task/release ID.
func lockState(ctx context.Context, root, kind, id string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name := ".wtui-" + kind + ".." + id
	if len(name)+len(".lock") > 255 {
		name = fmt.Sprintf(".wtui-%s..sha256..%x", kind, sha256.Sum256([]byte(id)))
	}
	unlock, err := fsutil.Lock(filepath.Join(root, name))
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
	record := ""
	if e.holder.PID > 0 {
		record = fmt.Sprintf(" (last recorded pid %d)", e.holder.PID)
	}
	return fmt.Sprintf("%s %s is being changed by another wtui%s; wait for it or close it", e.kind, e.id, record)
}

func (e *stateBusyError) Unwrap() error { return e.holder }
