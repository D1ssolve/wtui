package task

import (
	"context"
	"fmt"
	"os"
)

// ForceTaskCleanup removes task-owned resources without lifecycle or clean-worktree proof.
// It still refuses protected branches and paths outside the task directory.
func (m *manager) ForceTaskCleanup(ctx context.Context, taskID string, deleteRemoteBranches bool, statusCh chan<- string) (TaskCleanupResult, error) {
	if err := validateTaskID(taskID); err != nil {
		return TaskCleanupResult{}, err
	}
	result := TaskCleanupResult{TaskID: taskID}
	taskDir := m.taskDir(taskID)
	services, err := m.ListServices(ctx, taskID)
	if err != nil {
		return result, err
	}
	for _, svc := range services {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if !pathWithin(taskDir, svc.WorktreePath) {
			return result, fmt.Errorf("service %s worktree outside task directory", svc.Name)
		}
		if svc.WorktreePath != "" {
			if _, err := os.Stat(svc.WorktreePath); err == nil {
				commonDir, err := m.git.CommonDir(ctx, svc.RepoPath)
				if err != nil {
					return result, err
				}
				if err := m.git.RemoveWorktree(ctx, commonDir, svc.WorktreePath, true); err != nil {
					return result, err
				}
				line := "force remove task worktree " + svc.WorktreePath
				result.Completed = append(result.Completed, line)
				if err := sendReleaseCleanupStatus(ctx, statusCh, line); err != nil {
					return result, err
				}
			}
		}
		if svc.Branch == "" || m.isCleanupProtectedBranch(ctx, svc.Branch, taskID) {
			continue
		}
		exists, err := m.git.BranchExists(ctx, svc.RepoPath, svc.Branch)
		if err != nil || !exists {
			continue
		}
		sha, err := m.git.ResolveRef(ctx, svc.RepoPath, "refs/heads/"+svc.Branch)
		if err != nil {
			return result, err
		}
		if sha == "" {
			continue
		}
		if deleteRemoteBranches {
			remoteSHA, err := m.git.RemoteRefSHA(ctx, svc.RepoPath, "refs/heads/"+svc.Branch)
			if err != nil {
				return result, err
			}
			if remoteSHA != "" {
				if err := m.git.DeleteRemoteBranchIfUnchanged(ctx, svc.RepoPath, svc.Branch, remoteSHA); err != nil {
					return result, err
				}
			}
		}
		if err := m.git.DeleteBranchIfUnchanged(ctx, svc.RepoPath, svc.Branch, sha); err != nil {
			return result, err
		}
		line := "force delete local task branch " + svc.Branch
		result.Completed = append(result.Completed, line)
		if err := sendReleaseCleanupStatus(ctx, statusCh, line); err != nil {
			return result, err
		}
	}
	if err := os.RemoveAll(taskDir); err != nil {
		return result, err
	}
	line := "force remove task directory " + taskDir
	result.Completed = append(result.Completed, line)
	if err := sendReleaseCleanupStatus(ctx, statusCh, line); err != nil {
		return result, err
	}
	return result, nil
}
