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
	Retained  []string
}

func (m *manager) ExecuteReleaseCleanup(ctx context.Context, plan ReleaseCleanupPlan, statusCh chan<- string) (ReleaseCleanupResult, error) {
	result := ReleaseCleanupResult{ReleaseID: plan.preview.ReleaseID}
	if plan.preview.ReleaseID == "" || len(plan.preview.Blockers) > 0 {
		return result, fmt.Errorf("%w: %s", ErrReleaseCleanupBlocked, strings.Join(plan.preview.Blockers, "; "))
	}
	if !plan.fingerprintAuthentic() {
		return result, fmt.Errorf("%w: approved plan fingerprint mismatch", ErrReleaseCleanupBlocked)
	}
	fresh, err := m.PlanReleaseCleanup(ctx, plan.preview.ReleaseID, plan.preview.Selection)
	if err != nil {
		return result, err
	}
	if len(fresh.preview.Blockers) > 0 || fresh.fingerprint != plan.fingerprint || fresh.manifestDigest != plan.manifestDigest {
		return result, fmt.Errorf("%w: approved plan is stale", ErrReleaseCleanupBlocked)
	}
	if err := blockUnsupportedRemoteDeletion(plan.steps); err != nil {
		return result, err
	}

	for _, step := range plan.steps {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		line := step.description
		if step.kind == cleanupLocalTaskBranch || step.kind == cleanupLocalReleaseBranch {
			// Local branch deletion is never executed: the authoritative
			// merge targets live on the remote and no atomic guard exists,
			// so the branch is reported retained and never mutated.
			result.Retained = append(result.Retained, line)
			if err := sendReleaseCleanupStatus(ctx, statusCh, line); err != nil {
				return result, err
			}
			if m.logger != nil {
				m.logger.InfoContext(ctx, "release cleanup step retained", "release_id", plan.preview.ReleaseID, "step", line)
			}
			continue
		}
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

// blockUnsupportedRemoteDeletion fails closed before any mutation when the
// approved plan still authorizes remote source deletion. git push cannot
// atomically lease an unchanged target ref (no-op ref updates may be
// omitted), so no genuine atomic target guard exists and the remote branch
// must be retained; local cleanup alone stays authorized.
func blockUnsupportedRemoteDeletion(steps []releaseCleanupStep) error {
	for _, step := range steps {
		switch step.kind {
		case cleanupRemoteTaskBranch, cleanupRemoteReleaseBranch:
			if !step.noop {
				return fmt.Errorf("%w: %s: %w", ErrReleaseCleanupBlocked, step.description, ErrRemoteAtomicGuardUnsupported)
			}
		}
	}
	return nil
}

// retainLocalBranchReason is the plan/execution description for local branch
// steps: the branch is kept because no atomic remote target guard exists.
func retainLocalBranchReason(kind, branch string) string {
	return fmt.Sprintf("retain %s branch %s (no atomic remote target guard)", kind, branch)
}

func remoteSourceDeletionUnsupported(branch string) error {
	return fmt.Errorf("%w: remote branch %s: git push cannot atomically lease an unchanged target ref (no-op ref updates may be omitted)", ErrRemoteAtomicGuardUnsupported, branch)
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
		_, err := m.removeCleanupWorktree(ctx, step)
		return err
	case cleanupTaskDirectory:
		if !pathWithin(m.cfg.TasksRoot, step.path) {
			return fmt.Errorf("task directory %s outside task ownership", step.path)
		}
		if err := m.ensureNoRegisteredWorktreeBelow(ctx, step.path, plan.steps); err != nil {
			return err
		}
		if err := removeGeneratedTaskFiles(step.path, filepath.Base(step.path)); err != nil {
			return err
		}
		if err := ensureNoPresentCleanupPaths(plan.steps, cleanupTaskWorktree, step.path); err != nil {
			return err
		}
		return removeKnownEntriesOrPreserve(step.path, cleanupStepPaths(plan.steps, cleanupTaskWorktree, step.path))
	case cleanupLocalTaskBranch, cleanupLocalReleaseBranch:
		// Retention-only step: never mutated, reported by the caller.
		return nil
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
		return remoteSourceDeletionUnsupported(step.branch)
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
		if err := ensureNoPresentCleanupPaths(plan.steps, cleanupReleaseWorktree, step.path); err != nil {
			return err
		}
		known := append(cleanupStepPaths(plan.steps, cleanupReleaseWorktree, step.path), filepath.Join(step.path, releaseManifestFileName))
		return removeKnownEntriesOrPreserve(step.path, known)
	default:
		return fmt.Errorf("unknown cleanup step %d", step.kind)
	}
}

// removeCleanupWorktree removes a registered worktree after revalidating its
// full identity. The returned boolean reports whether a worktree was actually
// removed; false with a nil error means the worktree was already absent.
func (m *manager) removeCleanupWorktree(ctx context.Context, step releaseCleanupStep) (bool, error) {
	entries, err := m.git.ListWorktrees(ctx, step.repoPath)
	if err != nil {
		return false, err
	}
	var found *git.WorktreeEntry
	for i := range entries {
		if samePath(entries[i].Path, step.path) {
			if found != nil {
				return false, errors.New("duplicate worktree registration")
			}
			found = &entries[i]
		}
	}
	if found == nil {
		if _, statErr := os.Stat(step.path); os.IsNotExist(statErr) {
			return false, nil
		}
		return false, errors.New("worktree is not registered")
	}
	if found.Locked || found.HEAD != step.expectedSHA {
		return false, errors.New("worktree identity changed or locked")
	}
	wantBranch := "(detached)"
	if step.branch != "" {
		wantBranch = "refs/heads/" + step.branch
	}
	if found.Branch != wantBranch {
		return false, errors.New("worktree branch changed")
	}
	dirty, err := m.git.IsDirty(ctx, step.path)
	if err != nil {
		return false, err
	}
	if dirty {
		return false, errors.New("worktree became dirty")
	}
	commonDir, err := m.git.CommonDir(ctx, step.path)
	if err != nil {
		return false, err
	}
	repoCommonDir, err := m.git.CommonDir(ctx, step.repoPath)
	if err != nil {
		return false, err
	}
	if !samePath(commonDir, repoCommonDir) {
		return false, errors.New("worktree common repository mismatch")
	}
	if err := m.git.RemoveWorktree(ctx, commonDir, step.path, false); err != nil {
		return false, err
	}
	// Fail closed on any replacement: a path still present after git
	// worktree removal (file, directory, or symlink) is user data cleanup
	// never validated, so it is preserved and reported, never removed.
	if _, err := os.Lstat(step.path); err == nil {
		return true, fmt.Errorf("worktree path %s replaced after removal; preserved", step.path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return true, fmt.Errorf("worktree path %s unreadable after removal: %w", step.path, err)
	}
	return true, nil
}

func (m *manager) recheckCleanupTargets(ctx context.Context, step releaseCleanupStep) error {
	if len(step.targets) == 0 {
		return errors.New("cleanup branch has no planned merge target")
	}
	for _, target := range step.targets {
		ancestrySHA := target.integratedSHA
		if ancestrySHA == "" {
			ancestrySHA = step.integratedSHA
		}
		if ancestrySHA == "" {
			ancestrySHA = step.expectedSHA
		}
		sha, err := m.git.RemoteRefSHA(ctx, step.repoPath, target.ref)
		if err != nil {
			return err
		}
		if sha == "" {
			return fmt.Errorf("merge target %s disappeared", target.ref)
		}
		if err := m.git.EnsureCommit(ctx, step.repoPath, sha); err != nil {
			return fmt.Errorf("merge target %s object unavailable: %w", target.ref, err)
		}
		merged, err := m.git.IsAncestor(ctx, step.repoPath, ancestrySHA, sha)
		if err != nil {
			return err
		}
		if !merged {
			return fmt.Errorf("merge target %s no longer contains %s", target.ref, ancestrySHA)
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

// cleanupStepPaths returns the plan-registered paths of the given step kind
// that lie within root.
func cleanupStepPaths(steps []releaseCleanupStep, kind releaseCleanupStepKind, root string) []string {
	var paths []string
	for _, step := range steps {
		if step.kind == kind && step.path != "" && pathWithin(root, step.path) {
			paths = append(paths, step.path)
		}
	}
	return paths
}

// ensureNoPresentCleanupPaths fails closed when a plan-registered worktree
// path of the given kind still exists under root immediately before parent
// directory cleanup. Worktree removal and its post-removal validation belong
// to removeCleanupWorktree alone, so a path present here was never removed by
// cleanup (or reappeared after removal): it is unknown user data and must be
// preserved, never deleted as a plan-known entry.
func ensureNoPresentCleanupPaths(steps []releaseCleanupStep, kind releaseCleanupStepKind, root string) error {
	for _, step := range steps {
		if step.kind != kind || step.path == "" || !pathWithin(root, step.path) {
			continue
		}
		if _, err := os.Lstat(step.path); err == nil {
			return fmt.Errorf("worktree path %s present before parent cleanup; preserved", step.path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("worktree path %s unreadable before parent cleanup: %w", step.path, err)
		}
	}
	return nil
}

// removeKnownEntriesOrPreserve deletes root only when every entry below it is
// plan-known; unknown entries are preserved and named in the returned error
// so user files never die with a recursive delete.
func removeKnownEntriesOrPreserve(root string, knownPaths []string) error {
	known := make(map[string]bool, len(knownPaths))
	for _, p := range knownPaths {
		known[filepath.Clean(p)] = true
	}
	unknown, err := pruneKnownEntries(root, known)
	if err != nil {
		return err
	}
	if len(unknown) > 0 {
		return fmt.Errorf("directory %s retains unknown entries: %s", root, strings.Join(unknown, ", "))
	}
	// Only a proven-empty root may go away; a recreated or still-nonempty
	// root fails closed here instead of being recursively deleted.
	if err := os.Remove(root); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("directory %s not empty: %w", root, err)
	}
	return nil
}

// pruneScanHook is a test seam: invoked after each directory scan during
// known-entry pruning so tests can deterministically recreate entries
// between the scan and the removal.
var pruneScanHook func(dir string)

// pruneKnownEntries deletes known paths under dir and prunes directories that
// become empty, returning the names of preserved unknown entries.
func pruneKnownEntries(dir string, known map[string]bool) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if pruneScanHook != nil {
		pruneScanHook(dir)
	}
	var unknown []string
	for _, entry := range entries {
		full := filepath.Join(dir, entry.Name())
		if known[filepath.Clean(full)] {
			// Remove exactly this path (a symlink dies as a link, never its
			// target); anything still inside fails closed.
			if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("known entry %s not empty: %w", full, err)
			}
			continue
		}
		if !entry.IsDir() {
			unknown = append(unknown, entry.Name())
			continue
		}
		child, err := pruneKnownEntries(full, known)
		if err != nil {
			return nil, err
		}
		unknown = append(unknown, child...)
		if remaining, readErr := os.ReadDir(full); readErr == nil && len(remaining) == 0 {
			if err := os.Remove(full); err != nil {
				return nil, err
			}
		}
	}
	return unknown, nil
}
