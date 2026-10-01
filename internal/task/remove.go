package task

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/D1ssolve/wtui/internal/gitflow"
)

// RemoveOptions selects which task resources Remove deletes.
type RemoveOptions struct {
	RemoveWorktrees      bool
	Force                bool
	DeleteLocalBranches  bool
	DeleteRemoteBranches bool
}

// taskBranchCandidates returns the exact branch names owned by taskID under
// the resolved git-flow rules: every registered prefix joined with the task
// ID. Names outside this set are not task-owned and must not be deleted.
func (m *manager) taskBranchCandidates(taskID string) map[string]gitflow.BranchType {
	candidates := make(map[string]gitflow.BranchType)
	if m.flow == nil || taskID == "" {
		return candidates
	}
	for bt, rule := range m.flow.BranchTypes {
		for _, p := range rule.Prefixes {
			if p == "" {
				continue
			}
			candidates[p+taskID] = bt
		}
	}
	return candidates
}

func (m *manager) isRemoveProtectedBranch(ctx context.Context, branch, taskID string) bool {
	branch = strings.TrimSpace(branch)
	if branch == "" || branch == "HEAD" || (strings.HasPrefix(branch, "(") && strings.HasSuffix(branch, ")")) {
		return true
	}
	exact, _ := m.protectedBranchPolicy()
	for _, b := range exact {
		if b == branch {
			return true
		}
	}
	if m.flow != nil {
		if rule, ok := m.flow.BranchTypes[gitflow.BranchTypeRelease]; ok {
			for _, p := range rule.Prefixes {
				if p != "" && strings.HasPrefix(branch, p) {
					return true
				}
			}
		}
	}
	_, owned := m.taskBranchCandidates(taskID)[branch]
	return !owned
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

	// Remote source deletion has no atomic target guard (git push may omit
	// no-op ref updates), so it fails closed before any mutation; retry
	// without the remote branch option.
	if opts.DeleteRemoteBranches {
		return fmt.Errorf("%w: task %s: remove worktrees/local branches only, or delete the remote branch on the forge", ErrRemoteAtomicGuardUnsupported, taskID)
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
			if m.isRemoveProtectedBranch(ctx, branchName, taskID) {
				opErrors = append(opErrors, fmt.Errorf("refusing to delete protected branch %s for %s", branchName, entry.Name()))
				continue
			}
		}

		var localBranchSHA string
		if opts.DeleteLocalBranches {
			sha, shaErr := m.git.ResolveRef(ctx, commonDir, "refs/heads/"+branchName)
			if shaErr != nil {
				opErrors = append(opErrors, fmt.Errorf("resolve local SHA for %s: %w", entry.Name(), shaErr))
				continue
			}
			if sha == "" {
				opErrors = append(opErrors, fmt.Errorf("resolve local SHA for %s: empty SHA for branch %s", entry.Name(), branchName))
				continue
			}
			localBranchSHA = sha
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
			if delErr := m.git.DeleteBranchIfUnchanged(ctx, commonDir, branchName, localBranchSHA); delErr != nil {
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

	}

	if len(opErrors) > 0 {
		return errors.Join(opErrors...)
	}

	if !opts.RemoveWorktrees {
		return nil
	}

	if err := removeGeneratedTaskFiles(taskDir, taskID); err != nil {
		return err
	}

	remaining, err := os.ReadDir(taskDir)
	if err != nil {
		return fmt.Errorf("remove: read task dir %s: %w", taskDir, err)
	}
	if len(remaining) > 0 {
		names := make([]string, 0, len(remaining))
		for _, entry := range remaining {
			names = append(names, entry.Name())
		}
		m.logger.WarnContext(ctx, "preserving unknown task entries",
			slog.String("task_id", taskID),
			slog.Any("entries", names),
		)
		return fmt.Errorf("remove: task %s preserved unknown entries: %s", taskID, strings.Join(names, ", "))
	}

	if err := os.Remove(taskDir); err != nil {
		return fmt.Errorf("remove: delete task directory %s: %w", taskDir, err)
	}

	m.logger.InfoContext(ctx, "task removed")
	return nil
}
