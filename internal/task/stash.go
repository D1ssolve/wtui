package task

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func (m *manager) StashService(ctx context.Context, taskID, serviceName string, pop bool, includeUntracked bool) error {
	if err := validateTaskID(taskID); err != nil {
		return err
	}

	worktreePath := filepath.Join(m.taskDir(taskID), serviceName)
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return fmt.Errorf("%w: service %s not in task %s", ErrServiceNotFound, serviceName, taskID)
	} else if err != nil {
		return fmt.Errorf("stash service: stat worktree %s: %w", worktreePath, err)
	}

	if err := m.git.Stash(ctx, worktreePath, pop, includeUntracked); err != nil {
		op := "stash"
		if pop {
			op = "stash pop"
		}
		return fmt.Errorf("%s %s/%s: %w", op, taskID, serviceName, err)
	}

	return nil
}
