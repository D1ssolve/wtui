package task

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// RemoveOptions selects which task resources Remove deletes.
type RemoveOptions struct {
	RemoveWorktrees      bool
	Force                bool
	DeleteLocalBranches  bool
	DeleteRemoteBranches bool
}

func (m *manager) deleteRemoteBranch(ctx context.Context, repoPath, branch string) error {
	sha, err := m.git.RemoteRefSHA(ctx, repoPath, "refs/heads/"+branch)
	if err != nil {
		return fmt.Errorf("fetch remote SHA: %w", err)
	}
	if sha == "" {
		return nil
	}
	return m.git.DeleteRemoteBranchIfUnchanged(ctx, repoPath, branch, sha)
}

func (m *manager) Remove(ctx context.Context, taskID string, opts RemoveOptions) error {
	if err := validateTaskID(taskID); err != nil {
		return err
	}

	if !opts.RemoveWorktrees {
		if opts.DeleteLocalBranches {
			return fmt.Errorf("remove: local branch deletion requires worktree removal for task %s", taskID)
		}
		if !opts.DeleteRemoteBranches {
			return fmt.Errorf("remove: no removal options selected for task %s", taskID)
		}
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

	var opErrors []error

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		subdirPath := filepath.Join(taskDir, entry.Name())

		commonDir, cdErr := m.git.CommonDir(ctx, subdirPath)
		if cdErr != nil {
			m.logger.WarnContext(ctx, "could not determine common git dir, skipping service",
				slog.String("service", entry.Name()),
				slog.String("error", cdErr.Error()),
			)
			opErrors = append(opErrors, fmt.Errorf("common-dir for %s: %w", entry.Name(), cdErr))
			continue
		}

		var branchName string
		if opts.DeleteLocalBranches || opts.DeleteRemoteBranches {
			var brErr error
			branchName, brErr = m.git.GetWorktreeBranch(ctx, subdirPath)
			if brErr != nil {
				opErrors = append(opErrors, fmt.Errorf("resolve branch for %s: %w", entry.Name(), brErr))
				continue
			}
			if branchName == "" {
				opErrors = append(opErrors, fmt.Errorf("resolve branch for %s: empty branch name", entry.Name()))
				continue
			}
			if m.IsProtectedBranch(ctx, branchName) {
				opErrors = append(opErrors, fmt.Errorf("refusing to delete protected branch %s for %s", branchName, entry.Name()))
				continue
			}
		}

		worktreeRemoved := false

		if opts.RemoveWorktrees {
			if rmErr := m.git.RemoveWorktree(ctx, commonDir, subdirPath, opts.Force); rmErr != nil {
				m.logger.WarnContext(ctx, "failed to remove worktree",
					slog.String("service", entry.Name()),
					slog.String("error", rmErr.Error()),
					slog.Bool("force", opts.Force),
				)
				opErrors = append(opErrors, fmt.Errorf("remove worktree %s: %w", entry.Name(), rmErr))
			} else {
				worktreeRemoved = true
				m.logger.InfoContext(ctx, "removed worktree", slog.String("service", entry.Name()))
			}
		}

		if opts.DeleteLocalBranches && worktreeRemoved {
			if delErr := m.git.DeleteBranch(ctx, commonDir, branchName); delErr != nil {
				m.logger.WarnContext(ctx, "failed to delete branch",
					slog.String("service", entry.Name()),
					slog.String("branch", branchName),
					slog.String("error", delErr.Error()),
				)
				opErrors = append(opErrors, fmt.Errorf("delete branch %s: %w", branchName, delErr))
			} else {
				m.logger.InfoContext(ctx, "deleted branch",
					slog.String("service", entry.Name()),
					slog.String("branch", branchName),
				)
			}
		}

		if opts.DeleteRemoteBranches {
			if remErr := m.deleteRemoteBranch(ctx, commonDir, branchName); remErr != nil {
				m.logger.WarnContext(ctx, "failed to delete remote branch",
					slog.String("service", entry.Name()),
					slog.String("branch", branchName),
					slog.String("error", remErr.Error()),
				)
				opErrors = append(opErrors, fmt.Errorf("delete remote branch %s: %w", branchName, remErr))
			}
		}
	}

	if len(opErrors) > 0 {
		return errors.Join(opErrors...)
	}

	if !opts.RemoveWorktrees {
		return nil
	}

	if err := os.RemoveAll(taskDir); err != nil {
		return fmt.Errorf("remove: delete task directory %s: %w", taskDir, err)
	}

	m.logger.InfoContext(ctx, "task removed")
	return nil
}
