package task

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/diss0x/wtui/internal/domain"
)

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
		taskDir := filepath.Join(m.cfg.TasksRoot, taskID)
		task := domain.Task{
			ID:  taskID,
			Dir: taskDir,
		}

		// Stale detection: guard against race where dir is removed after ReadDir.
		if _, err := os.Stat(taskDir); os.IsNotExist(err) {
			task.Stale = true
		}

		tasks = append(tasks, task)
	}

	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].ID < tasks[j].ID
	})

	return tasks, nil
}

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

	// Collect only directory entries upfront — avoids spawning goroutines for
	// files like *.code-workspace or *.sln.
	var dirs []os.DirEntry
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry)
		}
	}

	// Fan out: one goroutine per service directory.
	type result struct {
		svc domain.Service
		ok  bool // false means the entry should be skipped (non-git dir)
	}

	results := make(chan result, len(dirs))

	var wg sync.WaitGroup
	for _, entry := range dirs {
		wg.Go(func() {
			svc, ok := m.inspectServiceDir(ctx, taskDir, entry)
			results <- result{svc: svc, ok: ok}
		})
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var services []domain.Service
	for r := range results {
		if r.ok {
			services = append(services, r.svc)
		}
	}

	// Restore deterministic order — goroutines complete in arbitrary sequence.
	sort.Slice(services, func(i, j int) bool {
		return services[i].Name < services[j].Name
	})

	return services, nil
}

// inspectServiceDir gathers all metadata for a single service subdirectory.
// Returns (service, true) when the directory is a valid git worktree, and
// (zero, false) when it should be excluded from the result (non-git dir).
func (m *manager) inspectServiceDir(ctx context.Context, taskDir string, entry os.DirEntry) (domain.Service, bool) {
	subdirPath := filepath.Join(taskDir, entry.Name())

	// Stale detection: if the subdir no longer exists (removed after ReadDir),
	// include it as stale so the panel can show [STALE] rather than silently dropping it.
	if _, statErr := os.Stat(subdirPath); os.IsNotExist(statErr) {
		return domain.Service{
			Name:         entry.Name(),
			WorktreePath: subdirPath,
			Stale:        true,
		}, true
	}

	commonDir, cdErr := m.git.CommonDir(ctx, subdirPath)
	if cdErr != nil {
		m.logger.DebugContext(ctx, "skipping non-git directory",
			slog.String("dir", subdirPath),
			slog.String("reason", cdErr.Error()),
		)
		return domain.Service{}, false
	}

	svc := domain.Service{
		Name:         entry.Name(),
		WorktreePath: subdirPath,
		RepoPath:     filepath.Dir(commonDir),
	}

	dirty, dirtyErr := m.git.IsDirty(ctx, subdirPath)
	if dirtyErr != nil {
		m.logger.WarnContext(ctx, "could not determine dirty state",
			slog.String("service", entry.Name()),
			slog.String("error", dirtyErr.Error()),
		)
	}
	svc.IsDirty = dirty
	svc.Branch = m.currentBranch(ctx, subdirPath, entry.Name())

	// Ahead/behind counts: best-effort, do not fail ListServices on error.
	if svc.Branch != "" {
		originBranch := "origin/" + svc.Branch
		ahead, behind, abErr := m.git.RevListAheadBehind(ctx, subdirPath, originBranch)
		if abErr != nil {
			m.logger.DebugContext(ctx, "could not determine ahead/behind count",
				slog.String("service", entry.Name()),
				slog.String("error", abErr.Error()),
			)
		} else {
			svc.Ahead = ahead
			svc.Behind = behind
		}
	}

	return svc, true
}

func (m *manager) currentBranch(ctx context.Context, worktreePath, serviceName string) string {
	entries, err := m.git.ListWorktrees(ctx, worktreePath)
	if err != nil {
		m.logger.WarnContext(ctx, "could not list worktrees for branch detection",
			slog.String("service", serviceName),
			slog.String("error", err.Error()),
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

	return ""
}
