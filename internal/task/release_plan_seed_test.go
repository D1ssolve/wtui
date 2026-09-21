package task

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/D1ssolve/wtui/internal/git"
)

func seedReleasePlanTasks(t *testing.T, tasksRoot string, gitMock *mockGitClient, specs ...releasePlanTaskService) {
	t.Helper()

	commonByWorktree := map[string]string{}
	for _, spec := range specs {
		worktreePath := filepath.Join(tasksRoot, spec.TaskID, spec.ServiceName)
		if err := os.MkdirAll(worktreePath, 0o755); err != nil {
			t.Fatalf("mkdir worktree path: %v", err)
		}

		commonDir := filepath.Join(spec.RepoPath, ".git")
		if err := os.MkdirAll(commonDir, 0o755); err != nil {
			t.Fatalf("mkdir common dir: %v", err)
		}

		commonByWorktree[worktreePath] = commonDir
		gitMock.listWorktreesRes = append(gitMock.listWorktreesRes, git.WorktreeEntry{
			Path:   worktreePath,
			Branch: "refs/heads/" + spec.Branch,
		})
	}

	addWorktree := gitMock.addWorktreeFn
	gitMock.addWorktreeFn = func(repo, dest, branch string, newBranch bool, base string) error {
		if addWorktree != nil {
			if err := addWorktree(repo, dest, branch, newBranch, base); err != nil {
				return err
			}
		} else if gitMock.addWorktreeErr != nil {
			return gitMock.addWorktreeErr
		}
		commonByWorktree[dest] = filepath.Join(repo, ".git")
		return nil
	}
	gitMock.commonDirFn = func(path string) (string, error) {
		common, ok := commonByWorktree[path]
		if !ok {
			return "", errors.New("not a git repo")
		}
		return common, nil
	}
}
