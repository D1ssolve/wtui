package task

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// A service whose worktree is already gone still belongs to the approval.
func (m *manager) checkTaskCleanupDestinations(ctx context.Context, plan TaskCleanupPlan) error {
	for _, step := range plan.steps {
		if step.kind != cleanupTaskWorktree {
			continue
		}
		url, err := m.git.PushURL(ctx, step.repoPath, m.cfg.ServiceRemote().String())
		if err != nil {
			return fmt.Errorf("%w: resolve push destination for %s: %w", ErrTaskCleanupBlocked, step.repoPath, err)
		}
		if url != step.remoteURL {
			return fmt.Errorf("%w: push destination changed for %s", ErrTaskCleanupBlocked, step.repoPath)
		}
	}
	return nil
}

// Git verifies registered worktrees separately; inventory only their roots
// and the additional entries that force would remove with the task directory.
func cleanupDirectoryScope(root string, steps []releaseCleanupStep) (map[string]os.FileInfo, error) {
	trees := map[string]bool{}
	for _, s := range steps {
		if s.kind == cleanupTaskWorktree {
			trees[s.path] = true
		}
	}
	scope := map[string]os.FileInfo{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == filepath.Join(root, pendingCleanupFile) {
			return nil // Written under the task lock and checked independently.
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		scope[path] = info
		if entry.IsDir() && trees[path] {
			return filepath.SkipDir
		}
		return nil
	})
	return scope, err
}
