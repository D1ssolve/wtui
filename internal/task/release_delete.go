package task

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
)

func ReleaseDeletionSelection() ReleaseCleanupSelection {
	return ReleaseCleanupSelection{
		DeleteRelease: true, RemoveRelease: true,
		DeleteLocalReleaseBranches: true, DeleteRemoteReleaseBranches: true,
	}
}

func (m *manager) planReleaseDeletion(ctx context.Context, plan ReleaseCleanupPlan, release domain.Release) (ReleaseCleanupPlan, error) {
	if plan.preview.Selection != ReleaseDeletionSelection() {
		plan.block("release deletion must preserve tasks and delete local and remote release resources")
		plan.finishFingerprint()
		return plan, nil
	}
	releases, err := m.ListReleases(ctx)
	if err != nil {
		return plan, err
	}
	services := slices.Clone(release.Services)
	slices.SortFunc(services, func(a, b domain.ReleaseService) int { return strings.Compare(a.Name, b.Name) })
	seen := make(map[string]bool)
	for _, svc := range services {
		if err := ctx.Err(); err != nil {
			return plan, err
		}
		if svc.Name == "" || filepath.Base(svc.Name) != svc.Name || svc.Name == "." || svc.Name == ".." || seen[svc.Name] {
			plan.block("invalid or duplicate release service %q", svc.Name)
			continue
		}
		seen[svc.Name] = true
		for _, other := range releases {
			if other.ID == release.ID {
				continue
			}
			for _, shared := range other.Services {
				if !samePath(shared.RepoPath, svc.RepoPath) {
					continue
				}
				if (svc.ReleaseSHA != "" || svc.ReleaseRef != "" || svc.PushedReleaseBranch) &&
					shared.ReleaseBranch == svc.ReleaseBranch && (shared.ReleaseSHA != "" || shared.ReleaseRef != "" || shared.PushedReleaseBranch) {
					plan.block("release branch %s is also owned by release %s", svc.ReleaseBranch, other.ID)
				}
				if (svc.TagSHA != "" || svc.TagRef != "" || svc.PushedTag) &&
					shared.Tag == svc.Tag && (shared.TagSHA != "" || shared.TagRef != "" || shared.PushedTag) {
					plan.block("release tag %s is also owned by release %s", svc.Tag, other.ID)
				}
			}
		}
		repo, err := m.discoverer.Resolve(ctx, svc.Name)
		if err != nil {
			return plan, err
		}
		if !samePath(repo, svc.RepoPath) {
			plan.block("service %s repository mismatch", svc.Name)
			continue
		}
		entries, err := m.git.ListWorktrees(ctx, repo)
		if err != nil {
			return plan, err
		}
		plan.repoPaths = append(plan.repoPaths, repo)
		preview := ReleaseCleanupServicePreview{Name: svc.Name, RepoPath: repo}
		if err := m.planReleaseDeletionWorktrees(ctx, &plan, release.Dir, svc, entries, &preview); err != nil {
			return plan, err
		}
		if err := m.planReleaseDeletionRefs(ctx, &plan, release.Status, svc, entries, &preview); err != nil {
			return plan, err
		}
		plan.preview.Services = append(plan.preview.Services, preview)
	}
	plan.steps = append(plan.steps, releaseCleanupStep{kind: cleanupReleaseDirectory, description: "remove release directory " + release.Dir, path: release.Dir})
	if err := validateReleaseDeletionDirectory(release.Dir, plan.steps); err != nil {
		plan.block("%v", err)
	}
	slices.SortStableFunc(plan.steps, func(a, b releaseCleanupStep) int {
		rank := func(kind releaseCleanupStepKind) int {
			switch kind {
			case deleteReleaseRemoteRef:
				return 0
			case cleanupReleaseWorktree:
				return 1
			case deleteReleaseLocalBranch, deleteReleaseLocalTag:
				return 2
			default:
				return 3
			}
		}
		return rank(a.kind) - rank(b.kind)
	})
	plan.finishFingerprint()
	return plan, nil
}

func (m *manager) planReleaseDeletionWorktrees(ctx context.Context, plan *ReleaseCleanupPlan, releaseDir string, svc domain.ReleaseService, entries []git.WorktreeEntry, preview *ReleaseCleanupServicePreview) error {
	paths := []struct{ path, branch string }{
		{filepath.Join(releaseDir, "services", svc.Name), svc.ReleaseBranch},
		{filepath.Join(releaseDir, ".work", svc.Name+"-integration"), ""},
		{filepath.Join(releaseDir, ".work", svc.Name+"-finalize-integration"), ""},
	}
	for _, path := range []string{svc.ReleaseWorktreePath, svc.IntegrationWorktreePath} {
		if path != "" && !slices.ContainsFunc(paths, func(p struct{ path, branch string }) bool { return samePath(p.path, path) }) {
			plan.block("service %s worktree path outside release ownership", svc.Name)
		}
	}
	for _, target := range paths {
		step := releaseCleanupStep{
			kind: cleanupReleaseWorktree, description: "remove release worktree " + target.path,
			repoPath: svc.RepoPath, path: target.path, branch: target.branch,
		}
		for _, entry := range entries {
			if samePath(entry.Path, step.path) {
				step.expectedSHA = entry.HEAD
			}
		}
		step.noop = m.validateCleanupWorktree(ctx, plan, entries, step)
		if !step.noop {
			common, err := m.git.CommonDir(ctx, step.path)
			if err != nil {
				return err
			}
			repoCommon, err := m.git.CommonDir(ctx, svc.RepoPath)
			if err != nil {
				return err
			}
			if !samePath(common, repoCommon) {
				plan.block("worktree %s repository mismatch", step.path)
			}
			preview.Worktrees = append(preview.Worktrees, step.path)
		}
		plan.steps = append(plan.steps, step)
	}
	return nil
}

// Check before pruning so unknown files never cause the retry manifest to be lost.
func validateReleaseDeletionDirectory(root string, steps []releaseCleanupStep) error {
	known := cleanupStepPaths(steps, cleanupReleaseWorktree, root)
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("release path %s is a symlink; preserved", path)
		}
		if entry.IsDir() {
			if slices.Contains(known, path) {
				return filepath.SkipDir
			}
			return nil
		}
		if path != filepath.Join(root, releaseManifestFileName) {
			return fmt.Errorf("release directory contains unknown file %s; preserved", path)
		}
		return nil
	})
}
