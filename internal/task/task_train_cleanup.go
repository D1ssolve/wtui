package task

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
)

// Check before removing worktrees or branches: target ancestry alone can
// reflect a replacement MR, while the saved train attempt is still pending.
func (m *manager) verifyTaskTrainCleanup(ctx context.Context, taskID string) (os.FileInfo, error) {
	path := filepath.Join(m.taskDir(taskID), taskTrainFile)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("task merge train sidecar is not a regular file")
	}
	pins, err := m.loadTaskTrainPins(taskID)
	if err != nil {
		return nil, err
	}
	if len(pins) == 0 {
		return nil, errors.New("task merge train sidecar disappeared during verification")
	}
	for _, pin := range pins {
		// The owning clone is still available on retry after worktree removal.
		path, err := m.discoverer.Resolve(ctx, pin.Service)
		if err != nil {
			return nil, err
		}
		svc := domain.Service{Name: pin.Service, RepoPath: path, WorktreePath: path, Branch: pin.Source}
		client, repo, err := m.serviceForgeTarget(ctx, svc)
		if err != nil {
			return nil, err
		}
		if client.Provider() != forge.ForgeProviderGitLab || repo != pin.Repo {
			return nil, fmt.Errorf("origin does not identify recorded GitLab project %s", pin.Repo)
		}
		mr, err := client.MRReadinessByNumber(ctx, pin.Number, repo, path)
		if err != nil {
			return nil, err
		}
		if err := validateMergedTaskMRIdentity(mr, pin.Source, forge.MRReadiness{
			Number: pin.Number, TargetBranch: pin.Target, HeadSHA: pin.Head,
		}); err != nil {
			return nil, err
		}
		if err := m.verifyMergedTaskMRContained(ctx, svc, mr.MergedSHA, pin.Target); err != nil {
			return nil, err
		}
	}
	return info, nil
}

func removeTaskDirectoryAfterCleanup(ctx context.Context, path string, trainSidecar os.FileInfo) (bool, error) {
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if len(entries) == 1 && entries[0].Name() == taskTrainFile && trainSidecar != nil {
		current, err := entries[0].Info()
		if err != nil {
			return false, err
		}
		if !os.SameFile(trainSidecar, current) || !current.Mode().IsRegular() ||
			current.Size() != trainSidecar.Size() || !current.ModTime().Equal(trainSidecar.ModTime()) {
			return false, errors.New("task merge train sidecar changed during cleanup")
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if err := os.Remove(filepath.Join(path, taskTrainFile)); err != nil {
			return false, err
		}
	} else if len(entries) > 0 {
		return false, fmt.Errorf("task directory %s is not empty", path)
	}
	return false, os.Remove(path)
}
