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

	"github.com/D1ssolve/wtui/internal/domain"
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

		if _, err := os.Stat(taskDir); os.IsNotExist(err) {
			task.Stale = true
		}

		tasks = append(tasks, task)
	}

	allTaskIDs := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		allTaskIDs[task.ID] = struct{}{}
	}

	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i := range tasks {
		i := i
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()

			services, listErr := m.ListServices(ctx, tasks[i].ID)
			if listErr != nil {
				m.logger.DebugContext(ctx, "list: could not load services for phase detection",
					slog.String("task_id", tasks[i].ID),
					slog.String("error", listErr.Error()),
				)
				return
			}

			phase, version := detectTaskPhase(services, m.flow)
			tasks[i].Phase = phase
			tasks[i].Version = version
		})
	}
	wg.Wait()

	for i := range tasks {
		tasks[i].ParentID = detectTaskRelationship(tasks[i].ID, allTaskIDs, m.cfg.TasksRoot, m.flow)
	}

	hasChildren := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		if task.ParentID == "" {
			continue
		}
		hasChildren[task.ParentID] = struct{}{}
	}

	for i := range tasks {
		_, tasks[i].IsGroup = hasChildren[tasks[i].ID]
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

	var dirs []os.DirEntry
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry)
		}
	}

	type result struct {
		svc domain.Service
		ok  bool
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

	sort.Slice(services, func(i, j int) bool {
		return services[i].Name < services[j].Name
	})

	return services, nil
}

func (m *manager) inspectServiceDir(ctx context.Context, taskDir string, entry os.DirEntry) (domain.Service, bool) {
	subdirPath := filepath.Join(taskDir, entry.Name())

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

	remoteURL, _ := m.git.RemoteURL(ctx, subdirPath, "origin")

	svc := domain.Service{
		Name:         entry.Name(),
		WorktreePath: subdirPath,
		RepoPath:     filepath.Dir(commonDir),
		RemoteURL:    remoteURL,
	}

	svc.Branch = m.currentBranch(ctx, subdirPath, entry.Name())

	type statusResult struct {
		dirty          bool
		ahead          int
		behind         int
		dirtyErr       error
		aheadBehindErr error
	}

	var status statusResult
	var statusWG sync.WaitGroup
	statusWG.Go(func() {
		status.dirty, status.dirtyErr = m.git.IsDirty(ctx, subdirPath)
	})

	if svc.Branch != "" {
		originBranch := "origin/" + svc.Branch
		statusWG.Go(func() {
			status.ahead, status.behind, status.aheadBehindErr = m.git.RevListAheadBehind(ctx, subdirPath, originBranch)
		})
	}
	statusWG.Wait()

	if status.dirtyErr != nil {
		m.logger.WarnContext(ctx, "could not determine dirty state",
			slog.String("service", entry.Name()),
			slog.String("error", status.dirtyErr.Error()),
		)
	}
	svc.IsDirty = status.dirty

	if status.aheadBehindErr != nil {
		m.logger.DebugContext(ctx, "could not determine ahead/behind count",
			slog.String("service", entry.Name()),
			slog.String("error", status.aheadBehindErr.Error()),
		)
	} else {
		svc.Ahead = status.ahead
		svc.Behind = status.behind
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
