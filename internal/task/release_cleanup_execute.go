package task

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/D1ssolve/wtui/internal/git"
)

type ReleaseCleanupResult struct {
	ReleaseID string
	Completed []string
}

func (m *manager) ExecuteReleaseCleanup(ctx context.Context, plan ReleaseCleanupPlan, statusCh chan<- string) (ReleaseCleanupResult, error) {
	result := ReleaseCleanupResult{ReleaseID: plan.preview.ReleaseID}
	if plan.preview.ReleaseID == "" || len(plan.preview.Blockers) > 0 {
		return result, fmt.Errorf("%w: %s", ErrReleaseCleanupBlocked, strings.Join(plan.preview.Blockers, "; "))
	}
	fresh, err := m.PlanReleaseCleanup(ctx, plan.preview.ReleaseID, plan.preview.Selection)
	if err != nil {
		return result, err
	}
	if len(fresh.preview.Blockers) > 0 || fresh.fingerprint != plan.fingerprint || fresh.manifestDigest != plan.manifestDigest {
		return result, fmt.Errorf("%w: approved plan is stale", ErrReleaseCleanupBlocked)
	}

	for _, step := range plan.steps {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		line := step.description
		if step.noop {
			line += " (already absent)"
		} else if err := m.executeReleaseCleanupStep(ctx, plan, step); err != nil {
			return result, fmt.Errorf("%s: %w", step.description, err)
		}
		result.Completed = append(result.Completed, line)
		if err := sendReleaseCleanupStatus(ctx, statusCh, line); err != nil {
			return result, err
		}
		if m.logger != nil {
			m.logger.InfoContext(ctx, "release cleanup step completed", "release_id", plan.preview.ReleaseID, "step", line)
		}
	}
	return result, nil
}

func sendReleaseCleanupStatus(ctx context.Context, statusCh chan<- string, line string) error {
	if statusCh == nil {
		return nil
	}
	select {
	case statusCh <- line:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *manager) executeReleaseCleanupStep(ctx context.Context, plan ReleaseCleanupPlan, step releaseCleanupStep) error {
	switch step.kind {
	case cleanupReleaseWorktree, cleanupTaskWorktree:
		return m.removeCleanupWorktree(ctx, step)
	case cleanupTaskDirectory:
		if err := m.ensureNoRegisteredWorktreeBelow(ctx, step.path, plan.steps); err != nil {
			return err
		}
		return removeAllRetrySafe(step.path)
	case cleanupLocalTaskBranch:
		if err := m.recheckCleanupTargets(ctx, step); err != nil {
			return err
		}
		exists, err := m.recheckCleanupLocalBranch(ctx, step)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
		return m.git.DeleteBranchIfUnchanged(ctx, step.repoPath, step.branch, step.expectedSHA)
	case cleanupLocalReleaseBranch:
		if err := m.recheckCleanupTargets(ctx, step); err != nil {
			return err
		}
		exists, err := m.recheckCleanupLocalBranch(ctx, step)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
		return m.git.DeleteBranchIfUnchanged(ctx, step.repoPath, step.branch, step.expectedSHA)
	case cleanupRemoteTaskBranch, cleanupRemoteReleaseBranch:
		if err := m.recheckCleanupTargets(ctx, step); err != nil {
			return err
		}
		sha, err := m.git.RemoteRefSHA(ctx, step.repoPath, "refs/heads/"+step.branch)
		if err != nil {
			return err
		}
		if sha == "" {
			return nil
		}
		if sha != step.expectedSHA {
			return fmt.Errorf("remote branch moved: expected %s, got %s", step.expectedSHA, sha)
		}
		return m.git.DeleteRemoteBranchIfUnchanged(ctx, step.repoPath, step.branch, step.expectedSHA)
	case cleanupReleaseDirectory:
		data, err := os.ReadFile(m.releaseManifestPath(plan.preview.ReleaseID))
		if err != nil {
			return err
		}
		if sha256.Sum256(data) != plan.manifestDigest {
			return errors.New("release manifest changed")
		}
		if err := m.ensureReleaseDirUnregistered(ctx, step.path, plan.repoPaths); err != nil {
			return err
		}
		return removeAllRetrySafe(step.path)
	default:
		return fmt.Errorf("unknown cleanup step %d", step.kind)
	}
}

func (m *manager) removeCleanupWorktree(ctx context.Context, step releaseCleanupStep) error {
	entries, err := m.git.ListWorktrees(ctx, step.repoPath)
	if err != nil {
		return err
	}
	var found *git.WorktreeEntry
	for i := range entries {
		if samePath(entries[i].Path, step.path) {
			if found != nil {
				return errors.New("duplicate worktree registration")
			}
			found = &entries[i]
		}
	}
	if found == nil {
		if _, statErr := os.Stat(step.path); os.IsNotExist(statErr) {
			return nil
		}
		return errors.New("worktree is not registered")
	}
	if found.Locked || found.HEAD != step.expectedSHA {
		return errors.New("worktree identity changed or locked")
	}
	wantBranch := "(detached)"
	if step.branch != "" {
		wantBranch = "refs/heads/" + step.branch
	}
	if found.Branch != wantBranch {
		return errors.New("worktree branch changed")
	}
	dirty, err := m.git.IsDirty(ctx, step.path)
	if err != nil {
		return err
	}
	if dirty {
		return errors.New("worktree became dirty")
	}
	commonDir, err := m.git.CommonDir(ctx, step.path)
	if err != nil {
		return err
	}
	repoCommonDir, err := m.git.CommonDir(ctx, step.repoPath)
	if err != nil {
		return err
	}
	if !samePath(commonDir, repoCommonDir) {
		return errors.New("worktree common repository mismatch")
	}
	if err := m.git.RemoveWorktree(ctx, commonDir, step.path, false); err != nil {
		return err
	}
	return removeAllRetrySafe(step.path)
}

func (m *manager) recheckCleanupLocalBranch(ctx context.Context, step releaseCleanupStep) (bool, error) {
	entries, err := m.git.ListWorktrees(ctx, step.repoPath)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.Branch == "refs/heads/"+step.branch {
			return false, errors.New("branch remains checked out")
		}
	}
	exists, err := m.git.BranchExists(ctx, step.repoPath, step.branch)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	sha, err := m.git.ResolveRef(ctx, step.repoPath, "refs/heads/"+step.branch)
	if err != nil {
		return false, err
	}
	if sha != step.expectedSHA {
		return false, errors.New("local branch moved")
	}
	return true, nil
}

func (m *manager) recheckCleanupTargets(ctx context.Context, step releaseCleanupStep) error {
	if len(step.targets) == 0 {
		return errors.New("cleanup branch has no planned merge target")
	}
	for _, target := range step.targets {
		sha, err := m.git.RemoteRefSHA(ctx, step.repoPath, target.ref)
		if err != nil {
			return err
		}
		if sha == "" {
			return fmt.Errorf("merge target %s disappeared", target.ref)
		}
		merged, err := m.git.IsAncestor(ctx, step.repoPath, step.expectedSHA, sha)
		if err != nil {
			return err
		}
		if !merged {
			return fmt.Errorf("merge target %s no longer contains %s", target.ref, step.expectedSHA)
		}
	}
	return nil
}

func (m *manager) ensureNoRegisteredWorktreeBelow(ctx context.Context, root string, steps []releaseCleanupStep) error {
	for _, step := range steps {
		if step.repoPath == "" {
			continue
		}
		entries, err := m.git.ListWorktrees(ctx, step.repoPath)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if pathWithin(root, entry.Path) {
				return fmt.Errorf("registered worktree remains at %s", entry.Path)
			}
		}
	}
	return nil
}

func (m *manager) ensureReleaseDirUnregistered(ctx context.Context, root string, repoPaths []string) error {
	for _, repoPath := range repoPaths {
		entries, err := m.git.ListWorktrees(ctx, repoPath)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if pathWithin(root, entry.Path) {
				return fmt.Errorf("registered worktree remains at %s", entry.Path)
			}
		}
	}
	return nil
}

func pathWithin(root, child string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(child))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func removeAllRetrySafe(path string) error {
	if err := os.RemoveAll(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
