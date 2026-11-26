package task

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
)

type trainCleanupPin struct {
	info os.FileInfo
	sum  [sha256.Size]byte
}

func readTrainCleanupPin(root string) (trainCleanupPin, []byte, error) {
	if err := checkTaskTrainRecovery(root); err != nil {
		return trainCleanupPin{}, nil, err
	}
	path := filepath.Join(root, taskTrainFile)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return trainCleanupPin{}, nil, nil
	}
	if err != nil {
		return trainCleanupPin{}, nil, err
	}
	if !info.Mode().IsRegular() {
		return trainCleanupPin{}, nil, errors.New("task merge train sidecar is not a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return trainCleanupPin{}, nil, err
	}
	current, err := os.Lstat(path)
	if err != nil {
		return trainCleanupPin{}, nil, err
	}
	if !os.SameFile(info, current) {
		return trainCleanupPin{}, nil, errors.New("task merge train sidecar changed while reading")
	}
	return trainCleanupPin{info: info, sum: sha256.Sum256(data)}, data, nil
}

func (pin trainCleanupPin) matches(current trainCleanupPin) bool {
	if pin.info == nil || current.info == nil {
		return pin.info == nil && current.info == nil
	}
	return pin.sum == current.sum && os.SameFile(pin.info, current.info)
}

func (pin trainCleanupPin) recheck(root string) ([]byte, error) {
	current, data, err := readTrainCleanupPin(root)
	if err != nil {
		return nil, err
	}
	if !pin.matches(current) {
		return nil, errors.New("task merge train sidecar changed since cleanup preview")
	}
	return data, nil
}

// Check before removing worktrees or branches: target ancestry alone can
// reflect a replacement MR, while the saved train attempt is still pending.
func (m *manager) verifyTaskTrainCleanup(ctx context.Context, plan TaskCleanupPlan) error {
	data, err := plan.trainPin.recheck(m.taskDir(plan.preview.TaskID))
	if err != nil {
		return err
	}
	if plan.trainPin.info == nil {
		return nil
	}
	pins, err := parseTaskTrainPins(data)
	if err != nil {
		return err
	}
	for _, pin := range pins {
		// The owning clone is still available on retry after worktree removal.
		path, err := m.discoverer.Resolve(ctx, pin.Service)
		if err != nil {
			return err
		}
		svc := domain.Service{Name: pin.Service, RepoPath: path, WorktreePath: path, Branch: pin.Source}
		// Same rule as M: a pin for a target the current service rule no longer reviews is stale evidence.
		if !slices.Contains(m.taskTrainTargets(svc), pin.Target) {
			return fmt.Errorf("recorded train MR !%d targets %s, which %s no longer reviews into; check it with M before cleanup", pin.Number, pin.Target, pin.Service)
		}
		client, repo, err := m.serviceForgeTarget(ctx, svc)
		if err != nil {
			return err
		}
		if client.Provider() != forge.ForgeProviderGitLab || repo != pin.Repo {
			return fmt.Errorf("%s does not identify recorded GitLab project %s", m.cfg.ServiceRemote(), pin.Repo)
		}
		mr, err := client.MRReadinessByNumber(ctx, pin.Number, repo, path)
		if err != nil {
			return err
		}
		if err := validateMergedTaskMRIdentity(mr, pin.Source, forge.MRReadiness{
			Number: pin.Number, TargetBranch: pin.Target, HeadSHA: pin.Head,
		}); err != nil {
			return err
		}
		if err := m.verifyMergedTaskMRContained(ctx, svc, mr.MergedSHA, pin.Target); err != nil {
			return err
		}
	}
	return nil
}

func removeTaskDirectoryAfterCleanup(ctx context.Context, path string, pin trainCleanupPin) (bool, error) {
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := pin.recheck(path); err != nil {
		return false, err
	}
	if len(entries) == 1 && entries[0].Name() == taskTrainFile && pin.info != nil {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		return false, retireTaskTrainDirectory(path)
	} else if len(entries) == 1 && entries[0].Name() == emptyTaskMarkerFileName && pin.info == nil {
		// The empty-task proof goes only together with its otherwise empty directory.
		return false, os.RemoveAll(path)
	} else if len(entries) > 0 {
		return false, fmt.Errorf("task directory %s is not empty", path)
	}
	return false, os.Remove(path)
}

func taskTrainRecoveryPath(root string) string {
	return filepath.Join(filepath.Dir(root), ".wtui-train-cleanup-"+digest(filepath.Base(root)))
}

func checkTaskTrainRecovery(root string) error {
	backup := taskTrainRecoveryPath(root)
	if _, err := os.Lstat(backup); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return err
		}
		return fmt.Errorf("unfinished task train cleanup: inspect recovery file %s before retrying", backup)
	}
	return nil
}

func retireTaskTrainDirectory(root string) error {
	pin := filepath.Join(root, taskTrainFile)
	backup := taskTrainRecoveryPath(root)
	// Reserve recovery in the parent before unlinking anything. A failed
	// restore leaves the link in place so a new preview cannot forget the pin.
	if err := os.Link(pin, backup); err != nil {
		return fmt.Errorf("preserve task train sidecar before removing directory: %w", err)
	}
	if err := os.Remove(pin); err != nil {
		return errors.Join(err, os.Remove(backup))
	}
	if err := os.Remove(root); err != nil {
		if restoreErr := os.Link(backup, pin); restoreErr != nil {
			return errors.Join(err, fmt.Errorf("restore task train sidecar from %s: %w", backup, restoreErr))
		}
		return errors.Join(err, os.Remove(backup))
	}
	if err := os.Remove(backup); err != nil {
		return fmt.Errorf("task directory removed; remove retained train recovery file: %w", err)
	}
	return nil
}
