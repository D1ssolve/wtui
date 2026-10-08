package task

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
)

// TaskCleanupResult reports task cleanup progress. Completed lists executed
// steps, Retained lists plan-proven steps whose resources were already absent
// plus local/remote branches that are never deleted, and Deferred lists remote
// branches left retained because their remote tip diverged from the proven
// source SHA. Remote carries the retained remote candidates for read-only
// reporting; no remote deletion path exists.
type TaskCleanupResult struct {
	TaskID    string
	Completed []string
	Retained  []string
	Deferred  []string
	Remote    []TaskCleanupRemoteCandidate
}

type TaskCleanupExecutionSelection struct {
	Force bool
}

// ExecuteTaskCleanup revalidates an approved task cleanup plan and executes
// its local steps in safety order: worktrees first, then the task directory
// only when empty. Local and remote branches are never deleted (no atomic
// local branch steps as retention-only. Remote branches are deleted only when
// the approved plan selected them. Execution stops on the first failure; steps
// already completed stay in the result. Retained remote candidates are
// populated only after every local Git step succeeds.
func (m *manager) ExecuteTaskCleanup(ctx context.Context, plan TaskCleanupPlan, statusCh chan<- string, selections ...TaskCleanupExecutionSelection) (TaskCleanupResult, error) {
	selection := TaskCleanupExecutionSelection{}
	if len(selections) > 0 {
		selection = selections[0]
	}
	result := TaskCleanupResult{TaskID: plan.preview.TaskID}
	if plan.preview.TaskID == "" || len(plan.preview.Blockers) > 0 {
		return result, fmt.Errorf("%w: %s", ErrTaskCleanupBlocked, strings.Join(plan.preview.Blockers, "; "))
	}
	if !plan.fingerprintAuthentic() {
		return result, fmt.Errorf("%w: approved plan fingerprint mismatch", ErrTaskCleanupBlocked)
	}
	if !selection.Force {
		fresh, err := m.PlanTaskCleanup(ctx, TaskCleanupRequest{TaskID: plan.preview.TaskID, DeleteRemoteTaskBranches: taskCleanupDeletesRemoteBranches(plan.steps)})
		if err != nil {
			return result, err
		}
		if len(fresh.preview.Blockers) > 0 || fresh.fingerprint != plan.fingerprint {
			return result, fmt.Errorf("%w: approved plan is stale", ErrTaskCleanupBlocked)
		}
	}

	ordered := taskCleanupExecutionOrder(plan.steps)
	for _, step := range ordered {
		if step.kind == cleanupTaskDirectory {
			continue
		}
		if err := m.runTaskCleanupStep(ctx, statusCh, &result, plan, selection, step); err != nil {
			return result, err
		}
	}

	if !taskCleanupDeletesRemoteBranches(plan.steps) {
		result.Remote = slices.Clone(plan.preview.Remote)
	}
	for _, candidate := range plan.preview.DeferredRemote {
		result.Deferred = append(result.Deferred, "retain remote branch "+candidate.Branch+": remote tip diverges from proven source")
	}

	for _, step := range ordered {
		if step.kind != cleanupTaskDirectory {
			continue
		}
		if err := m.runTaskCleanupStep(ctx, statusCh, &result, plan, selection, step); err != nil {
			return result, err
		}
	}
	return result, nil
}

// runTaskCleanupStep executes one local cleanup step.
func (m *manager) runTaskCleanupStep(ctx context.Context, statusCh chan<- string, result *TaskCleanupResult, plan TaskCleanupPlan, selection TaskCleanupExecutionSelection, step releaseCleanupStep) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	line := step.description
	noop := step.noop
	if step.kind == cleanupRemoteTaskBranch && step.noop {
		line += " (already absent)"
		result.Retained = append(result.Retained, line)
		return sendReleaseCleanupStatus(ctx, statusCh, line)
	}
	if !noop {
		executed, execErr := m.executeTaskCleanupStep(ctx, plan, selection, step)
		if execErr != nil {
			return fmt.Errorf("%s: %w", step.description, execErr)
		}
		noop = executed
	}
	if noop {
		line += " (already absent)"
		result.Retained = append(result.Retained, line)
	} else {
		result.Completed = append(result.Completed, line)
	}
	if err := sendReleaseCleanupStatus(ctx, statusCh, line); err != nil {
		return err
	}
	if m.logger != nil {
		m.logger.InfoContext(ctx, "task cleanup step completed", "task_id", plan.preview.TaskID, "step", line)
	}
	return nil
}

// taskCleanupExecutionOrder orders local steps by safety: worktree removal,
// then local branch retention reporting, then the task directory last.
func taskCleanupExecutionOrder(steps []releaseCleanupStep) []releaseCleanupStep {
	rank := func(kind releaseCleanupStepKind) int {
		switch kind {
		case cleanupTaskWorktree:
			return 0
		case cleanupLocalTaskBranch:
			return 1
		case cleanupRemoteTaskBranch:
			return 2
		case cleanupTaskDirectory:
			return 3
		default:
			return 3
		}
	}
	ordered := slices.Clone(steps)
	slices.SortStableFunc(ordered, func(a, b releaseCleanupStep) int { return rank(a.kind) - rank(b.kind) })
	return ordered
}

// executeTaskCleanupStep executes one local cleanup step, returning true when
// the target resource was already absent and nothing was mutated.
func (m *manager) executeTaskCleanupStep(ctx context.Context, plan TaskCleanupPlan, selection TaskCleanupExecutionSelection, step releaseCleanupStep) (bool, error) {
	switch step.kind {
	case cleanupTaskWorktree:
		if !pathWithin(m.cfg.TasksRoot, step.path) {
			return false, fmt.Errorf("worktree %s outside task ownership", step.path)
		}
		removed, err := m.removeCleanupWorktree(ctx, step, selection.Force)
		if err != nil {
			return false, err
		}
		return !removed, nil
	case cleanupLocalTaskBranch:
		exists, err := m.git.BranchExists(ctx, step.repoPath, step.branch)
		if err != nil {
			return false, err
		}
		if !exists {
			return true, nil
		}
		if err := m.recheckCleanupTargets(ctx, step); err != nil {
			return false, err
		}
		guards, err := localCleanupBranchGuards(step)
		if err != nil {
			return false, err
		}
		return false, m.git.DeleteBranchIfUnchanged(ctx, step.repoPath, step.branch, step.expectedSHA, guards...)
	case cleanupRemoteTaskBranch:
		if err := m.recheckCleanupTargets(ctx, step); err != nil {
			return false, err
		}
		sha, err := m.git.RemoteRefSHA(ctx, step.repoPath, "refs/heads/"+step.branch)
		if err != nil {
			return false, err
		}
		if sha == "" {
			return true, nil
		}
		if sha != step.expectedSHA {
			return false, fmt.Errorf("remote branch moved: expected %s, got %s", step.expectedSHA, sha)
		}
		return false, m.git.DeleteRemoteBranchIfUnchanged(ctx, step.repoPath, step.branch, step.expectedSHA)
	case cleanupTaskDirectory:
		if !pathWithin(m.cfg.TasksRoot, step.path) {
			return false, fmt.Errorf("task directory %s outside task ownership", step.path)
		}
		if err := m.ensureNoRegisteredWorktreeBelow(ctx, step.path, plan.steps); err != nil {
			return false, err
		}
		if err := removeGeneratedTaskFiles(step.path, plan.preview.TaskID); err != nil {
			return false, err
		}
		entries, err := os.ReadDir(step.path)
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		if len(entries) > 0 && !selection.Force {
			return false, fmt.Errorf("task directory %s is not empty", step.path)
		}
		if selection.Force {
			return false, os.RemoveAll(step.path)
		}
		return false, os.Remove(step.path)
	default:
		return false, fmt.Errorf("unknown cleanup step %d", step.kind)
	}
}

func taskCleanupDeletesRemoteBranches(steps []releaseCleanupStep) bool {
	return slices.ContainsFunc(steps, func(step releaseCleanupStep) bool { return step.kind == cleanupRemoteTaskBranch })
}

func taskCleanupBlockedByActiveRelease(releases []domain.Release, taskID, serviceName, branch string) (string, bool) {
	for _, release := range releases {
		involved := slices.Contains(release.TaskIDs, taskID)
		if !involved {
			for _, releaseSvc := range release.Services {
				for _, fb := range releaseSvc.FeatureBranches {
					if fb.TaskID == taskID && fb.ServiceName == serviceName && fb.Branch == branch {
						involved = true
					}
				}
			}
		}
		if involved && classifyCleanupDependency(release).blocks {
			return release.ID, true
		}
	}
	return "", false
}
