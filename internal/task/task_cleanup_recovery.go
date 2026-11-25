package task

import (
	"io/fs"
	"os"
	"path/filepath"
)

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
