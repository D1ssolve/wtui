package task

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/D1ssolve/wtui/internal/domain"
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

	var branchDeleteErr error
	if removeBranch {
		if err = m.git.DeleteBranch(ctx, commonDir, branchName); err != nil {
			branchDeleteErr = fmt.Errorf("remove service: failed delete branch %s: %w", branchName, err)
		}
	}

	if _, err := os.Stat(m.taskDir(taskID)); err != nil {
		return fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}

	taskDir := m.taskDir(taskID)
	remainingServices, err := discoverServicesFromTaskDir(taskDir)
	if err != nil {
		return fmt.Errorf("remove service: discover remaining services in %s: %w", taskDir, err)
	}

	if len(remainingServices) == 0 {
		if err := removeGeneratedTaskFiles(taskDir, taskID); err != nil {
			m.logger.WarnContext(ctx, "failed to remove generated task files after removing last service",
				slog.String("task_id", taskID),
				slog.String("error", err.Error()),
			)
		}
		return branchDeleteErr
	}

	if err := generateWorkspaceFile(taskID, taskDir); err != nil {
		m.logger.WarnContext(ctx, "failed to regenerate workspace file after service removal",
			slog.String("task_id", taskID),
			slog.String("error", err.Error()),
		)
	}

	services := make([]domain.Service, 0, len(remainingServices))
	for _, svc := range remainingServices {
		services = append(services, domain.Service{
			Name:         svc.Name,
			WorktreePath: svc.RepoPath,
		})
	}

	if err := m.slnMgr.Generate(ctx, taskDir, taskID, services); err != nil {
		m.logger.WarnContext(ctx, "sln generation failed after service removal",
			slog.String("task_id", taskID),
			slog.String("error", err.Error()),
		)
	}

	if branchDeleteErr != nil {
		return errors.Join(branchDeleteErr)
	}

	return nil
}
