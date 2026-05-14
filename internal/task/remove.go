package task

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

func (m *manager) Remove(ctx context.Context, taskID string, force, deleteBranches bool) error {
	if err := validateTaskID(taskID); err != nil {
		return err
	}

	taskDir := m.taskDir(taskID)

	if _, err := os.Stat(taskDir); os.IsNotExist(err) {
		return fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	} else if err != nil {
		return fmt.Errorf("remove: stat task dir %s: %w", taskDir, err)
	}

	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return fmt.Errorf("remove: read task dir %s: %w", taskDir, err)
	}

	var removeErrors []error

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		subdirPath := filepath.Join(taskDir, entry.Name())

		commonDir, cdErr := m.git.CommonDir(ctx, subdirPath)
		if cdErr != nil {
			m.logger.WarnContext(ctx, "could not determine common git dir, skipping worktree removal",
				slog.String("service", entry.Name()),
				slog.String("error", cdErr.Error()),
			)
			removeErrors = append(removeErrors, fmt.Errorf("common-dir for %s: %w", entry.Name(), cdErr))
			continue
		}

		var branchName string
		if deleteBranches {
			branchName, _ = m.git.GetWorktreeBranch(ctx, subdirPath)
		}

		if rmErr := m.git.RemoveWorktree(ctx, commonDir, subdirPath, force); rmErr != nil {
			m.logger.WarnContext(ctx, "failed to remove worktree",
				slog.String("service", entry.Name()),
				slog.String("error", rmErr.Error()),
				slog.Bool("force", force),
			)
			removeErrors = append(removeErrors, fmt.Errorf("remove worktree %s: %w", entry.Name(), rmErr))
			continue
		}

		m.logger.InfoContext(ctx, "removed worktree", slog.String("service", entry.Name()))

		if deleteBranches && branchName != "" {
			if delErr := m.git.DeleteBranch(ctx, commonDir, branchName); delErr != nil {
				m.logger.WarnContext(ctx, "failed to delete branch",
					slog.String("service", entry.Name()),
					slog.String("branch", branchName),
					slog.String("error", delErr.Error()),
				)
				removeErrors = append(removeErrors, fmt.Errorf("delete branch %s: %w", branchName, delErr))
			} else {
				m.logger.InfoContext(ctx, "deleted branch",
					slog.String("service", entry.Name()),
					slog.String("branch", branchName),
				)
			}
		}
	}

	if len(removeErrors) > 0 && !force {
		return errors.Join(removeErrors...)
	}

	if err := os.RemoveAll(taskDir); err != nil {
		return fmt.Errorf("remove: delete task directory %s: %w", taskDir, err)
	}

	m.logger.InfoContext(ctx, "task removed")
	return nil
}
