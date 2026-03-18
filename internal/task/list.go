package task

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/diss0x/wtui/internal/domain"
)

// List returns all tasks in cfg.TasksRoot, sorted alphabetically by task ID.
// When TasksRoot does not exist, an empty slice and nil error are returned
// (spec AC-10: not an error condition).
func (m *manager) List(ctx context.Context) ([]domain.Task, error) {
	entries, err := os.ReadDir(m.cfg.TasksRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return []domain.Task{}, nil
		}
		return nil, fmt.Errorf("list: read tasks root %s: %w", m.cfg.TasksRoot, err)
	}

	var tasks []domain.Task
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		taskID := entry.Name()
		tasks = append(tasks, domain.Task{
			ID:  taskID,
			Dir: filepath.Join(m.cfg.TasksRoot, taskID),
		})
	}

	// os.ReadDir already returns entries in alphabetical order on most platforms,
	// but we sort explicitly to guarantee the contract regardless of OS behaviour.
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].ID < tasks[j].ID
	})

	return tasks, nil
}

// ListServices returns the services (worktrees) belonging to taskID, sorted
// alphabetically by name. Each service entry has its IsDirty and Branch fields
// populated via git calls; failures are logged and the entry is still included
// with best-effort values.
//
// Returns ErrTaskNotFound when the task directory does not exist.
func (m *manager) ListServices(ctx context.Context, taskID string) ([]domain.Service, error) {
	if err := validateTaskID(taskID); err != nil {
		return nil, err
	}

	taskDir := m.taskDir(taskID)

	if _, err := os.Stat(taskDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	} else if err != nil {
		return nil, fmt.Errorf("list services: stat task dir %s: %w", taskDir, err)
	}

	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return nil, fmt.Errorf("list services: read task dir %s: %w", taskDir, err)
	}

	var services []domain.Service
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		subdirPath := filepath.Join(taskDir, entry.Name())

		// Guard: only include valid git worktrees. Non-git directories (e.g.
		// node_modules, intermediate tool dirs) are expected and benign — skip
		// them at Debug level rather than Warn.
		commonDir, cdErr := m.git.CommonDir(ctx, subdirPath)
		if cdErr != nil {
			m.logger.DebugContext(ctx, "skipping non-git directory",
				slog.String("dir", subdirPath),
				slog.String("reason", cdErr.Error()),
			)
			continue
		}

		svc := domain.Service{
			Name:         entry.Name(),
			WorktreePath: subdirPath,
			// commonDir is the .git directory; the repo root is one level up.
			RepoPath: filepath.Dir(commonDir),
		}

		// Populate IsDirty.
		dirty, dirtyErr := m.git.IsDirty(ctx, subdirPath)
		if dirtyErr != nil {
			m.logger.WarnContext(ctx, "could not determine dirty state",
				"service", entry.Name(),
				"error", dirtyErr.Error(),
			)
		}
		svc.IsDirty = dirty

		// Populate Branch from worktree list.
		svc.Branch = m.currentBranch(ctx, subdirPath, entry.Name())

		services = append(services, svc)
	}

	sort.Slice(services, func(i, j int) bool {
		return services[i].Name < services[j].Name
	})

	return services, nil
}

// currentBranch returns the branch name currently checked out in the worktree at
// worktreePath. It queries `git worktree list` from the worktree path and extracts
// the matching entry's branch ref, stripping the "refs/heads/" prefix.
//
// Returns an empty string when the branch cannot be determined (e.g., the path is
// not a git worktree, or the worktree is in detached-HEAD state).
func (m *manager) currentBranch(ctx context.Context, worktreePath, serviceName string) string {
	entries, err := m.git.ListWorktrees(ctx, worktreePath)
	if err != nil {
		m.logger.WarnContext(ctx, "could not list worktrees for branch detection",
			"service", serviceName,
			"error", err.Error(),
		)
		return ""
	}

	for _, e := range entries {
		if e.Path == worktreePath {
			branch := strings.TrimPrefix(e.Branch, "refs/heads/")
			if branch == "(detached)" {
				return ""
			}
			return branch
		}
	}

	// No exact match — fall back to the first non-main worktree entry if there is
	// only one linked worktree (handles cases where the path resolves differently).
	return ""
}
