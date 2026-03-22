package task

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

func (m *manager) RemoveService(
	ctx context.Context,
	taskID string,
	serviceName string,
	removeBranch bool,
) error {
	if err := validateTaskID(taskID); err != nil {
		return err
	}

	worktreePath := filepath.Join(m.taskDir(taskID), serviceName)
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return fmt.Errorf("%w: service %s not in task %s", ErrServiceNotFound, serviceName, taskID)
	} else if err != nil {
		return fmt.Errorf("remove service: stat worktree %s: %w", worktreePath, err)
	}

	commonDir, err := m.git.CommonDir(ctx, worktreePath)
	if err != nil {
		return fmt.Errorf("remove service: not found common directory for %s: %w", worktreePath, err)
	}

	var branchName string
	if removeBranch {
		branchName, err = m.git.GetWorktreeBranch(ctx, worktreePath)
		if err != nil {
			return fmt.Errorf("remove service: failed to get branch name %s: %w", worktreePath, err)
		}
	}

	err = m.git.RemoveWorktree(ctx, commonDir, worktreePath, false)
	if err != nil {
		return fmt.Errorf("remove service: failed delete worktree %s: %w", worktreePath, err)
	}

	if removeBranch {
		if err = m.git.DeleteBranch(ctx, commonDir, branchName); err != nil {
			return fmt.Errorf("remove service: failed delete branch %s: %w", branchName, err)
		}
	}

	taskDir := m.taskDir(taskID)
	if _, err := os.Stat(taskDir); err != nil {
		return fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}

	if err := m.GenerateSln(ctx, taskID); err != nil {
		m.logger.WarnContext(ctx, "sln generation failed after service removal",
			slog.String("task", taskID),
			slog.String("error", err.Error()),
		)
	}

	return nil
}
