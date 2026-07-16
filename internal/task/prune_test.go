package task

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func TestScanPrunableTasks_AllServicesMerged_PrunableTrue(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskID := "IN-1"

	services := createTaskWithServices(t, tasksRoot, taskID, "svc-a", "svc-b")

	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			service := filepath.Base(path)
			return filepath.Join(rootDir, "repos", service, ".git"), nil
		},
		listWorktreesRes: worktreesForTask(taskID, services),
		branchExistsFn:   func(_, _ string) (bool, error) { return true, nil },
		isAncestorFn:     func(_, _, _ string) (bool, error) { return true, nil },
	}

	mgr := newPruneManager(t, rootDir, tasksRoot, gitMock, 4)

	candidates, err := mgr.ScanPrunableTasks(context.Background())
	if err != nil {
		t.Fatalf("ScanPrunableTasks error: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates len = %d, want 1", len(candidates))
	}

	c := candidates[0]
	if !c.Prunable {
		t.Fatalf("Prunable = false, want true")
	}
	for _, svc := range c.Services {
		if !svc.IsMerged || svc.IsStale {
			t.Fatalf("service %+v expected merged and not stale", svc)
		}
		if svc.MergeTarget != "origin/develop" {
			t.Fatalf("MergeTarget = %q, want origin/develop", svc.MergeTarget)
		}
	}
}

func TestScanPrunableTasks_OneServiceNotMerged_PrunableFalse(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskID := "IN-2"

	services := createTaskWithServices(t, tasksRoot, taskID, "svc-a", "svc-b")

	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			service := filepath.Base(path)
			return filepath.Join(rootDir, "repos", service, ".git"), nil
		},
		listWorktreesRes: worktreesForTask(taskID, services),
		branchExistsFn:   func(_, _ string) (bool, error) { return true, nil },
		isAncestorFn: func(repoPath, _, _ string) (bool, error) {
			if filepath.Base(repoPath) == "svc-b" {
				return false, nil
			}
			return true, nil
		},
	}

	mgr := newPruneManager(t, rootDir, tasksRoot, gitMock, 4)
	candidates, err := mgr.ScanPrunableTasks(context.Background())
	if err != nil {
		t.Fatalf("ScanPrunableTasks error: %v", err)
	}

	if candidates[0].Prunable {
		t.Fatalf("Prunable = true, want false")
	}
}

func TestScanPrunableTasks_StaleServiceIncluded(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskID := "IN-3"

	services := createTaskWithServices(t, tasksRoot, taskID, "svc-stale", "svc-merged")

	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			service := filepath.Base(path)
			return filepath.Join(rootDir, "repos", service, ".git"), nil
		},
		listWorktreesRes: worktreesForTask(taskID, services),
		branchExistsFn: func(repoPath, _ string) (bool, error) {
			if filepath.Base(repoPath) == "svc-stale" {
				return false, nil
			}
			return true, nil
		},
		isAncestorFn: func(_, _, _ string) (bool, error) { return true, nil },
	}

	mgr := newPruneManager(t, rootDir, tasksRoot, gitMock, 4)
	candidates, err := mgr.ScanPrunableTasks(context.Background())
	if err != nil {
		t.Fatalf("ScanPrunableTasks error: %v", err)
	}

	if !candidates[0].Prunable {
		t.Fatalf("Prunable = false, want true")
	}

	var staleFound bool
	for _, svc := range candidates[0].Services {
		if svc.ServiceName == "svc-stale" {
			staleFound = true
			if !svc.IsStale {
				t.Fatalf("svc-stale IsStale = false, want true")
			}
		}
	}
	if !staleFound {
		t.Fatal("svc-stale not found")
	}
}

func TestScanPrunableTasks_ConcurrentScanMultipleTasks(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	allWorktrees := make([]git.WorktreeEntry, 0)
	for i := 1; i <= 8; i++ {
		taskID := fmt.Sprintf("IN-%02d", i)
		services := createTaskWithServices(t, tasksRoot, taskID, "svc")
		allWorktrees = append(allWorktrees, worktreesForTask(taskID, services)...)
	}

	var (
		mu        sync.Mutex
		active    int
		maxActive int
	)

	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			service := filepath.Base(path)
			return filepath.Join(rootDir, "repos", service, ".git"), nil
		},
		listWorktreesRes: allWorktrees,
		branchExistsFn:   func(_, _ string) (bool, error) { return true, nil },
		isAncestorFn: func(_, _, _ string) (bool, error) {
			mu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			mu.Unlock()

			time.Sleep(40 * time.Millisecond)

			mu.Lock()
			active--
			mu.Unlock()
			return true, nil
		},
	}

	mgr := newPruneManager(t, rootDir, tasksRoot, gitMock, 4)

	start := time.Now()
	candidates, err := mgr.ScanPrunableTasks(context.Background())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("ScanPrunableTasks error: %v", err)
	}
	if len(candidates) != 8 {
		t.Fatalf("candidates len = %d, want 8", len(candidates))
	}
	if maxActive < 2 {
		t.Fatalf("maxActive = %d, want >= 2", maxActive)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("elapsed = %s, unexpectedly slow", elapsed)
	}
}

func TestScanPrunableTasks_ReadOnly_NoWriteGitCommands(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskID := "IN-RO"

	services := createTaskWithServices(t, tasksRoot, taskID, "svc-a")

	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			service := filepath.Base(path)
			return filepath.Join(rootDir, "repos", service, ".git"), nil
		},
		listWorktreesRes: worktreesForTask(taskID, services),
		branchExistsFn:   func(_, _ string) (bool, error) { return true, nil },
		isAncestorFn:     func(_, _, _ string) (bool, error) { return true, nil },
	}

	mgr := newPruneManager(t, rootDir, tasksRoot, gitMock, 4)

	if _, err := mgr.ScanPrunableTasks(context.Background()); err != nil {
		t.Fatalf("ScanPrunableTasks error: %v", err)
	}

	gitMock.mu.Lock()
	defer gitMock.mu.Unlock()

	if len(gitMock.addWorktreeCalls) != 0 || len(gitMock.addWorktreeWithTrackingCalls) != 0 ||
		len(gitMock.removeWorktreeCalls) != 0 || len(gitMock.fetchCalls) != 0 ||
		len(gitMock.mergeCalls) != 0 || len(gitMock.rebaseCalls) != 0 ||
		len(gitMock.pushCalls) != 0 || len(gitMock.stashCalls) != 0 ||
		gitMock.createTagCalls != 0 || gitMock.pushTagCalls != 0 || gitMock.deleteBranchCalls != 0 {
		t.Fatal("scan invoked git write command(s)")
	}
}

func newPruneManager(t *testing.T, rootDir, tasksRoot string, gitMock *mockGitClient, concurrency int) *manager {
	t.Helper()

	cfg := &config.Config{
		RootDir:      rootDir,
		TasksRoot:    tasksRoot,
		BranchPrefix: "feature/",
		BaseBranch:   "develop",
		Editor:       "code",
		Prune: &config.PruneConfig{
			Concurrency: concurrency,
		},
	}
	if _, err := cfg.Effective(); err != nil {
		t.Fatalf("cfg.Effective(): %v", err)
	}
	cfg.RootDir = rootDir
	cfg.TasksRoot = tasksRoot
	cfg.Prune.Concurrency = concurrency

	flow := &gitflow.ResolvedGitFlow{
		ProductionBranch:  "master",
		IntegrationBranch: "develop",
		DefaultBranchType: gitflow.BranchTypeFeature,
		BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
			gitflow.BranchTypeFeature: {Prefixes: []string{"feature/"}},
			gitflow.BranchTypeHotfix:  {Prefixes: []string{"hotfix/"}},
			gitflow.BranchTypeRelease: {Prefixes: []string{"release/"}},
		},
	}

	return &manager{cfg: cfg, git: gitMock, flow: flow, logger: newTestLogger()}
}

func createTaskWithServices(t *testing.T, tasksRoot, taskID string, serviceNames ...string) []domain.Service {
	t.Helper()

	taskDir := filepath.Join(tasksRoot, taskID)
	services := make([]domain.Service, 0, len(serviceNames))
	for _, service := range serviceNames {
		servicePath := filepath.Join(taskDir, service)
		if err := os.MkdirAll(servicePath, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", servicePath, err)
		}
		services = append(services, domain.Service{Name: service, WorktreePath: servicePath})
	}
	return services
}

func worktreesForTask(taskID string, services []domain.Service) []git.WorktreeEntry {
	out := make([]git.WorktreeEntry, 0, len(services))
	for _, svc := range services {
		out = append(out, git.WorktreeEntry{
			Path:   svc.WorktreePath,
			Branch: "refs/heads/feature/" + taskID,
		})
	}
	return out
}
