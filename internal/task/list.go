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
	"golang.org/x/sync/semaphore"
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
		if m.shouldIgnoreTaskDir(entry.Name()) {
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

	sem := semaphore.NewWeighted(int64(m.concurrency()))
	var wg sync.WaitGroup
	for i := range tasks {
		i := i
		dirs, dirsErr := m.listServiceDirs(tasks[i].Dir)
		if dirsErr != nil {
			m.logger.DebugContext(ctx, "list: could not read service directories",
				slog.String("task_id", tasks[i].ID),
				slog.String("error", dirsErr.Error()),
			)
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()

			services, listErr := m.listServicesWithSem(ctx, tasks[i].ID, dirs, sem)
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
		}()
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

func (m *manager) shouldIgnoreTaskDir(name string) bool {
	if name == ".releases" {
		return true
	}

	if m.cfg.Release != nil && m.cfg.Release.RootDir != "" {
		releaseRoot := filepath.Clean(m.cfg.Release.RootDir)
		tasksRoot := filepath.Clean(m.cfg.TasksRoot)

		rel, err := filepath.Rel(tasksRoot, releaseRoot)
		if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			topLevel := strings.Split(rel, string(filepath.Separator))[0]
			if topLevel == name {
				return true
			}
		}
	}

	if strings.HasPrefix(name, ".") && strings.Contains(strings.ToLower(name), "release") {
		return true
	}

	return false
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

	dirs := filterDirEntries(entries)

	sem := semaphore.NewWeighted(int64(m.concurrency()))
	return m.listServicesWithSem(ctx, taskID, dirs, sem)
}

func (m *manager) listServiceDirs(taskDir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return nil, err
	}
	return filterDirEntries(entries), nil
}

func filterDirEntries(entries []os.DirEntry) []os.DirEntry {
	var dirs []os.DirEntry
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry)
		}
	}
	return dirs
}

func (m *manager) listServicesWithSem(ctx context.Context, taskID string, dirs []os.DirEntry, sem *semaphore.Weighted) ([]domain.Service, error) {
	type result struct {
		svc domain.Service
		ok  bool
	}

	results := make(chan result, len(dirs))
	cache := newGitCache()

	var wg sync.WaitGroup
	var acquireErr error
	for _, entry := range dirs {
		if err := sem.Acquire(ctx, 1); err != nil {
			acquireErr = err
			break
		}

		wg.Go(func() {
			defer sem.Release(1)
			svc, ok := m.inspectServiceDir(ctx, cache, m.taskDir(taskID), entry)
			results <- result{svc: svc, ok: ok}
		})
	}

	if acquireErr != nil {
		wg.Wait()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, acquireErr
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

func (m *manager) inspectServiceDir(ctx context.Context, cache *gitCache, taskDir string, entry os.DirEntry) (domain.Service, bool) {
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

	svc.Branch = m.currentBranch(ctx, cache, subdirPath, svc.RepoPath, entry.Name())

	var (
		dirty          bool
		dirtyErr       error
		ahead          int
		behind         int
		aheadBehindErr error
	)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		dirty, dirtyErr = m.git.IsDirty(ctx, subdirPath)
	}()
	go func() {
		defer wg.Done()
		if svc.Branch != "" {
			ahead, behind, aheadBehindErr = m.git.RevListAheadBehind(ctx, subdirPath, "origin/"+svc.Branch)
		}
	}()
	wg.Wait()

	if dirtyErr != nil {
		m.logger.WarnContext(ctx, "could not determine dirty state",
			slog.String("service", entry.Name()),
			slog.String("error", dirtyErr.Error()),
		)
	}
	svc.IsDirty = dirty

	if aheadBehindErr != nil {
		m.logger.DebugContext(ctx, "could not determine ahead/behind count",
			slog.String("service", entry.Name()),
			slog.String("error", aheadBehindErr.Error()),
		)
	} else {
		svc.Ahead = ahead
		svc.Behind = behind
	}

	return svc, true
}

func (m *manager) currentBranch(ctx context.Context, cache *gitCache, worktreePath, repoPath, serviceName string) string {
	entries, err := cache.listWorktrees(ctx, m.git, repoPath)
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
