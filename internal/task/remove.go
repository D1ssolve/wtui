package task

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Remove removes a task and all its linked git worktrees.
//
// This is the Go translation of `cmd_remove` from taskflow.sh.
//
// Behaviour:
//   - Returns ErrTaskNotFound if the task directory does not exist.
//   - For each service subdirectory: obtains the common git dir and calls
//     git worktree remove. Errors per service are recorded but do not abort
//     the loop.
//   - If any worktree removal failed AND force is false: returns a combined
//     error WITHOUT calling os.RemoveAll (task directory is preserved).
//   - If all removals succeeded OR force is true: calls os.RemoveAll(taskDir)
//     to delete the task directory regardless of individual worktree errors.
func (m *manager) Remove(ctx context.Context, taskID string, force bool) error {
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

		// Obtain the common git directory (required by git worktree remove --git-dir).
		commonDir, cdErr := m.git.CommonDir(ctx, subdirPath)
		if cdErr != nil {
			m.logger.WarnContext(ctx, "could not determine common git dir, skipping worktree removal",
				"service", entry.Name(),
				"error", cdErr.Error(),
			)
			removeErrors = append(removeErrors, fmt.Errorf("common-dir for %s: %w", entry.Name(), cdErr))
			continue
		}

		// Remove the linked worktree.
		if rmErr := m.git.RemoveWorktree(ctx, commonDir, subdirPath, force); rmErr != nil {
			m.logger.WarnContext(ctx, "failed to remove worktree",
				"service", entry.Name(),
				"error", rmErr.Error(),
				"force", force,
			)
			removeErrors = append(removeErrors, fmt.Errorf("remove worktree %s: %w", entry.Name(), rmErr))
		} else {
			m.logger.InfoContext(ctx, "removed worktree",
				"service", entry.Name(),
				"task_id", taskID,
			)
		}
	}

	// If there were errors and the caller did not request force-removal, preserve
	// the task directory and surface the combined error (spec AC-14).
	if len(removeErrors) > 0 && !force {
		return combineErrors(removeErrors)
	}

	// All worktrees removed successfully, or force=true — clean up the task dir.
	if err := os.RemoveAll(taskDir); err != nil {
		return fmt.Errorf("remove: delete task directory %s: %w", taskDir, err)
	}

	m.logger.InfoContext(ctx, "task removed", "task_id", taskID)
	return nil
}

// combineErrors returns a single error that includes the messages from all errors
// in the slice. The errors are joined with "; " for readability.
func combineErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}

	msg := errs[0].Error()
	for _, e := range errs[1:] {
		msg += "; " + e.Error()
	}
	return fmt.Errorf("%s", msg)
}
