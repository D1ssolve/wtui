package task

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
)

// removeOwnedIntegrationWorktree removes a release-owned integration
// worktree without force and without recursive deletion. The persisted
// manifest path is untrusted input: it is honoured only when it names the
// exact worktree this release owns inside its .work directory, is
// registered exactly once for the service repository, is unlocked and
// clean, and matches the HEAD recorded in the manifest. Any deviation
// blocks the removal and preserves the path, so concurrent or replacement
// content is never destroyed.
func (m *manager) removeOwnedIntegrationWorktree(ctx context.Context, release *domain.Release, svc *domain.ReleaseService, worktreePath, expectedHEAD string) error {
	workDir := filepath.Join(release.Dir, ".work")
	cleaned := filepath.Clean(worktreePath)
	if !pathWithin(workDir, cleaned) {
		return fmt.Errorf("integration worktree path %s is outside the release-owned directory %s", cleaned, workDir)
	}
	base := filepath.Base(cleaned)
	if base != svc.Name+"-finalize-integration" && base != svc.Name+"-integration" {
		return fmt.Errorf("integration worktree path %s does not name an owned worktree for service %s", cleaned, svc.Name)
	}

	entries, err := m.git.ListWorktrees(ctx, svc.RepoPath)
	if err != nil {
		return fmt.Errorf("list worktrees for %s: %w", svc.RepoPath, err)
	}
	matches := 0
	var entry git.WorktreeEntry
	for _, candidate := range entries {
		if filepath.Clean(candidate.Path) == cleaned {
			matches++
			entry = candidate
		}
	}
	if matches != 1 {
		return fmt.Errorf("expected exactly one registered worktree at %s, found %d", cleaned, matches)
	}
	if entry.Locked {
		return fmt.Errorf("integration worktree %s is locked", cleaned)
	}
	if entry.Branch != "(detached)" {
		return fmt.Errorf("integration worktree %s is checked out at branch %s, want detached", cleaned, entry.Branch)
	}
	if expectedHEAD != "" && entry.HEAD != expectedHEAD {
		return fmt.Errorf("integration worktree %s HEAD = %s, want %s from manifest", cleaned, entry.HEAD, expectedHEAD)
	}
	commonDir, err := m.git.CommonDir(ctx, cleaned)
	if err != nil {
		return fmt.Errorf("resolve common dir for %s: %w", cleaned, err)
	}
	if filepath.Clean(filepath.Dir(commonDir)) != filepath.Clean(svc.RepoPath) {
		return fmt.Errorf("integration worktree %s belongs to a different repository", cleaned)
	}
	status, err := m.git.RepoStatus(ctx, cleaned)
	if err != nil {
		return fmt.Errorf("inspect status for %s: %w", cleaned, err)
	}
	if len(status.ChangedEntries) > 0 || len(status.UntrackedPaths) > 0 || len(status.ConflictPaths) > 0 {
		return fmt.Errorf("integration worktree %s is dirty", cleaned)
	}
	if err := m.git.RemoveWorktree(ctx, commonDir, cleaned, false); err != nil {
		return fmt.Errorf("remove integration worktree %s: %w", cleaned, err)
	}
	if _, err := os.Stat(cleaned); err == nil {
		return fmt.Errorf("integration worktree %s is still present after removal", cleaned)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat integration worktree %s: %w", cleaned, err)
	}
	return nil
}
