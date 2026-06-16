package task

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
)

func TestList_IgnoresDefaultReleaseStorageAndInternals(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	for _, dir := range []string{"APP-100", "APP-100-release", ".releases", ".release-work", ".release-meta"} {
		if err := mkdirAll(filepath.Join(tasksRoot, dir)); err != nil {
			t.Fatalf("setup: create dir %s: %v", dir, err)
		}
	}

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})

	tasks, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("List error: %v", err)
	}

	ids := taskIDs(tasks)
	if !slices.Equal(ids, []string{"APP-100", "APP-100-release"}) {
		t.Fatalf("task IDs = %v, want %v", ids, []string{"APP-100", "APP-100-release"})
	}
}

func TestList_IgnoresConfiguredReleaseRootInsideTasksRoot(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	releaseRoot := filepath.Join(tasksRoot, "custom-releases")

	for _, dir := range []string{"APP-200", "APP-200-release", "custom-releases"} {
		if err := mkdirAll(filepath.Join(tasksRoot, dir)); err != nil {
			t.Fatalf("setup: create dir %s: %v", dir, err)
		}
	}

	cfg := &config.Config{
		TasksRoot:    tasksRoot,
		RootDir:      rootDir,
		BranchPrefix: "feature/",
		Editor:       "code",
		Release: &config.ReleaseConfig{
			RootDir: releaseRoot,
		},
	}
	if _, err := cfg.Effective(); err != nil {
		t.Fatalf("cfg.Effective(): %v", err)
	}
	cfg.TasksRoot = tasksRoot
	cfg.RootDir = rootDir

	mgr := newTestManagerWithCfg(t, cfg, &mockGitClient{})

	tasks, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("List error: %v", err)
	}

	ids := taskIDs(tasks)
	if !slices.Equal(ids, []string{"APP-200", "APP-200-release"}) {
		t.Fatalf("task IDs = %v, want %v", ids, []string{"APP-200", "APP-200-release"})
	}
}

func mkdirAll(path string) error {
	return os.MkdirAll(path, 0o755)
}

func taskIDs(tasks []domain.Task) []string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return ids
}
