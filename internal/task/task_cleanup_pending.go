package task

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/D1ssolve/wtui/internal/fsutil"
)

const pendingCleanupFile = ".wtui-cleanup-pending"

// The record is a veto, never an authorization. Only an independently
// proven plan (or the original approved remainder) may account for the rows.
func (p TaskCleanupPlan) pendingBranches() string {
	var rows strings.Builder
	for _, s := range p.steps {
		if s.kind == cleanupTaskWorktree {
			fmt.Fprintf(&rows, "%q %q %q %s %x\n", s.path, s.repoPath, s.branch, s.expectedSHA, sha256.Sum256([]byte(s.remoteURL)))
		}
	}
	return rows.String()
}

func checkPendingBranches(path, record string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode().IsRegular() && record != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return true, err
		}
		if string(data) == record {
			return true, nil
		}
	}
	return true, fmt.Errorf("%w: branch ownership is unaccounted for; inspect %s and restore the recorded worktrees before retrying", ErrTaskCleanupBlocked, path)
}

func recordPendingBranches(path, record string) error {
	exists, err := checkPendingBranches(path, record)
	if err != nil || exists {
		return err
	}
	return fsutil.WriteFileAtomic(path, []byte(record), 0600)
}

// Older cleanups left no record when the last worktree disappeared. An
// exact candidate name can veto directory-only cleanup, never authorize it.
func (m *manager) checkEmptyTaskBranches(ctx context.Context, id string) error {
	repos, err := m.Repos(ctx, true)
	if err != nil {
		return err
	}
	candidates := m.taskCleanupBranchCandidates(id)
	for _, repo := range repos {
		for branch := range candidates {
			if m.isCleanupProtectedBranch(ctx, branch, id) {
				continue
			}
			found, err := m.git.BranchExists(ctx, repo.Path, branch)
			if err != nil {
				return err
			}
			if found {
				return fmt.Errorf("%w: no registered service accounts for %s in %s; restore its worktree or reconcile the branch before retrying", ErrTaskCleanupBlocked, branch, repo.Path)
			}
		}
	}
	return nil
}

func (m *manager) planTaskCleanup(ctx context.Context, req TaskCleanupRequest, retry *TaskCleanupPlan) (TaskCleanupPlan, error) {
	p, err := m.buildTaskCleanupPlan(ctx, req, retry)
	if err != nil || p.Blocked() {
		return p, err
	}
	if _, err := checkPendingBranches(filepath.Join(m.taskDir(req.TaskID), pendingCleanupFile), p.pendingBranches()); err != nil {
		p.block("%v", err)
	} else if len(p.preview.Services) == 0 {
		if err := m.checkEmptyTaskBranches(ctx, req.TaskID); err != nil {
			p.block("%v", err)
		}
	}
	for _, step := range p.steps {
		if step.kind != cleanupLocalTaskBranch || step.noop {
			continue
		}
		guards, err := localCleanupBranchGuards(step)
		if err == nil {
			err = m.git.VerifyLocalRefGuards(ctx, step.repoPath, guards)
		}
		if err != nil {
			p.block("%s: %v", step.description, err)
		}
	}
	if p.Blocked() {
		p.steps = nil
		p.finishFingerprint()
	}
	return p, nil
}
