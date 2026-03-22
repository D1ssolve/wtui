package task

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// PushService runs `git push -u origin HEAD` for a single service worktree.
// Lines written to lineCh describe progress.
func (m *manager) PushService(ctx context.Context, taskID, serviceName string, lineCh chan<- string) error {
	if err := validateTaskID(taskID); err != nil {
		return err
	}

	worktreePath := filepath.Join(m.taskDir(taskID), serviceName)
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return fmt.Errorf("%w: service %s not in task %s", ErrServiceNotFound, serviceName, taskID)
	} else if err != nil {
		return fmt.Errorf("push service: stat worktree %s: %w", worktreePath, err)
	}

	lineCh <- fmt.Sprintf("[%s] pushing...", serviceName)
	if err := m.git.Push(ctx, worktreePath, lineCh); err != nil {
		return fmt.Errorf("push %s/%s: %w", taskID, serviceName, err)
	}
	lineCh <- fmt.Sprintf("[%s] pushed.", serviceName)

	return nil
}
