package task

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/D1ssolve/wtui/internal/domain"
)

var retryCheckpoints = map[string]struct{}{
	"failed":               {},
	"fetch":                {},
	"integration_worktree": {},
	"merge":                {},
	"push_integration":     {},
	"branch":               {},
	"tag":                  {},
	"push_branch":          {},
	"push_tag":             {},
	"service_done":         {},
	"cleanup":              {},
	"final":                {},
}

func (m *manager) RetryRelease(ctx context.Context, releaseID string) (domain.Release, error) {
	release, err := m.GetRelease(ctx, releaseID)
	if err != nil {
		return domain.Release{}, err
	}

	if release.Status != domain.ReleaseStatusFailed {
		return domain.Release{}, fmt.Errorf("%w: %s -> validating", ErrReleaseInvalidStatusTransition, release.Status)
	}
	if release.Error == nil || !release.Error.Recoverable {
		return domain.Release{}, fmt.Errorf("%w: failed release is not recoverable", ErrReleaseInvalidStatusTransition)
	}

	if err := m.validateRetrySafety(ctx, release); err != nil {
		return domain.Release{}, err
	}

	if err := m.moveReleaseStatus(&release, domain.ReleaseStatusValidating, "validating", nil); err != nil {
		return domain.Release{}, err
	}
	if release.StartedAt == nil {
		startedAt := defaultReleaseNow().UTC()
		release.StartedAt = &startedAt
	}
	release.CompletedAt = nil
	release, err = m.writeReleaseManifest(release)
	if err != nil {
		return domain.Release{}, err
	}

	if err := m.moveReleaseStatus(&release, domain.ReleaseStatusMerging, "merging", nil); err != nil {
		return release, err
	}
	release, err = m.writeReleaseManifest(release)
	if err != nil {
		return domain.Release{}, err
	}

	for i := range release.Services {
		svc := &release.Services[i]
		if serviceRetryCompleted(*svc) {
			svc.Status = domain.ReleaseStatusReleased
			if err := m.persistCheckpoint(&release, "service_done", nil); err != nil {
				return domain.Release{}, err
			}
			continue
		}

		if execErr := m.retryReleaseService(ctx, &release, svc); execErr != nil {
			releaseErr := classifyReleaseError(execErr, svc)
			_ = m.failRelease(&release, releaseErr)
			return release, execErr
		}
	}

	if err := m.ensureRetryFinalizeStatus(&release); err != nil {
		return release, err
	}

	if err := m.moveReleaseStatus(&release, domain.ReleaseStatusReleased, "final", nil); err != nil {
		return release, err
	}
	completedAt := defaultReleaseNow().UTC()
	release.CompletedAt = &completedAt
	release.Error = nil

	release, err = m.writeReleaseManifest(release)
	if err != nil {
		return domain.Release{}, err
	}

	return release, nil
}

func (m *manager) validateRetrySafety(ctx context.Context, release domain.Release) error {
	if _, ok := retryCheckpoints[release.Checkpoint]; !ok {
		return fmt.Errorf("%w: unknown checkpoint %q", ErrReleaseRetryUnsafe, release.Checkpoint)
	}

	for _, svc := range release.Services {
		if err := m.validateRetryServiceRefs(ctx, svc); err != nil {
			return err
		}
	}

	return nil
}

func (m *manager) validateRetryServiceRefs(ctx context.Context, svc domain.ReleaseService) error {
	if svc.PushedIntegration || svc.PreIntegrationRef != "" || svc.PostIntegrationRef != "" {
		exists, err := m.git.BranchExists(ctx, svc.RepoPath, svc.IntegrationBranch)
		if err != nil {
			return fmt.Errorf("%w: service=%s integration=%s: %v", ErrReleaseRetryUnsafe, svc.Name, svc.IntegrationBranch, err)
		}
		if !exists {
			return fmt.Errorf("%w: service=%s missing integration branch %s", ErrReleaseRetryUnsafe, svc.Name, svc.IntegrationBranch)
		}
		if svc.PostIntegrationSHA != "" {
			currentSHA, resolveErr := resolveReleaseRefSHA(ctx, svc.RepoPath, svc.IntegrationBranch)
			if resolveErr != nil {
				return fmt.Errorf("%w: service=%s integration sha resolve: %v", ErrReleaseRetryUnsafe, svc.Name, resolveErr)
			}
			if currentSHA != svc.PostIntegrationSHA {
				return fmt.Errorf("%w: service=%s integration sha mismatch expected=%s actual=%s", ErrReleaseRetryUnsafe, svc.Name, svc.PostIntegrationSHA, currentSHA)
			}
		}
	}

	if svc.PushedReleaseBranch || svc.ReleaseRef != "" || svc.ReleaseWorktreePath != "" {
		exists, err := m.git.BranchExists(ctx, svc.RepoPath, svc.ReleaseBranch)
		if err != nil {
			return fmt.Errorf("%w: service=%s release branch=%s: %v", ErrReleaseRetryUnsafe, svc.Name, svc.ReleaseBranch, err)
		}
		if !exists {
			return fmt.Errorf("%w: service=%s missing release branch %s", ErrReleaseRetryUnsafe, svc.Name, svc.ReleaseBranch)
		}
		if svc.ReleaseSHA != "" {
			currentSHA, resolveErr := resolveReleaseRefSHA(ctx, svc.RepoPath, svc.ReleaseBranch)
			if resolveErr != nil {
				return fmt.Errorf("%w: service=%s release sha resolve: %v", ErrReleaseRetryUnsafe, svc.Name, resolveErr)
			}
			if currentSHA != svc.ReleaseSHA {
				return fmt.Errorf("%w: service=%s release sha mismatch expected=%s actual=%s", ErrReleaseRetryUnsafe, svc.Name, svc.ReleaseSHA, currentSHA)
			}
		}
	}

	if svc.PushedTag || svc.TagRef != "" || svc.Status == domain.ReleaseStatusReleased {
		exists, err := m.git.TagExists(ctx, svc.RepoPath, svc.Tag)
		if err != nil {
			return fmt.Errorf("%w: service=%s tag=%s: %v", ErrReleaseRetryUnsafe, svc.Name, svc.Tag, err)
		}
		if !exists {
			return fmt.Errorf("%w: service=%s missing tag %s", ErrReleaseRetryUnsafe, svc.Name, svc.Tag)
		}
		if svc.TagSHA != "" {
			currentSHA, resolveErr := resolveReleaseRefSHA(ctx, svc.RepoPath, svc.Tag+"^{}")
			if resolveErr != nil {
				return fmt.Errorf("%w: service=%s tag sha resolve: %v", ErrReleaseRetryUnsafe, svc.Name, resolveErr)
			}
			if currentSHA != svc.TagSHA {
				return fmt.Errorf("%w: service=%s tag sha mismatch expected=%s actual=%s", ErrReleaseRetryUnsafe, svc.Name, svc.TagSHA, currentSHA)
			}
		}
	}

	if svc.PostIntegrationRef != "" && svc.ReleaseRef != "" {
		if svc.PostIntegrationSHA == "" || svc.ReleaseSHA == "" {
			ok, err := m.git.IsAncestor(ctx, svc.RepoPath, svc.PostIntegrationRef, svc.ReleaseRef)
			if err != nil {
				return fmt.Errorf("%w: service=%s post_integration_ref check: %v", ErrReleaseRetryUnsafe, svc.Name, err)
			}
			if !ok {
				return fmt.Errorf("%w: service=%s post_integration_ref=%s release_ref=%s", ErrReleaseRetryUnsafe, svc.Name, svc.PostIntegrationRef, svc.ReleaseRef)
			}
		}
	}

	if svc.ReleaseRef != "" && svc.TagRef != "" {
		if svc.ReleaseSHA == "" || svc.TagSHA == "" {
			ok, err := m.git.IsAncestor(ctx, svc.RepoPath, svc.ReleaseRef, svc.TagRef)
			if err != nil {
				return fmt.Errorf("%w: service=%s tag_ref check: %v", ErrReleaseRetryUnsafe, svc.Name, err)
			}
			if !ok {
				return fmt.Errorf("%w: service=%s release_ref=%s tag_ref=%s", ErrReleaseRetryUnsafe, svc.Name, svc.ReleaseRef, svc.TagRef)
			}
		}
	}

	return nil
}

func (m *manager) retryReleaseService(ctx context.Context, release *domain.Release, svc *domain.ReleaseService) error {
	svc.Status = domain.ReleaseStatusMerging
	if err := m.persistCheckpoint(release, "fetch", nil); err != nil {
		return err
	}
	if err := m.git.Fetch(ctx, svc.RepoPath); err != nil {
		return fmt.Errorf("%w: service=%s: %v", ErrReleaseOperationInProgress, svc.Name, err)
	}

	integrationPath := svc.IntegrationWorktreePath
	if integrationPath == "" {
		integrationPath = filepath.Join(release.Dir, ".work", svc.Name+"-integration")
		if err := os.MkdirAll(filepath.Dir(integrationPath), 0o755); err != nil {
			return fmt.Errorf("%w: %v", ErrReleaseManifestInvalid, err)
		}
		if err := m.git.AddWorktree(ctx, svc.RepoPath, integrationPath, svc.IntegrationBranch, false, ""); err != nil {
			return err
		}
		svc.IntegrationWorktreePath = integrationPath
		if err := m.persistCheckpoint(release, "integration_worktree", nil); err != nil {
			return err
		}
	}

	keepIntegration := m.cfg.Release != nil && m.cfg.Release.KeepIntegrationWorktrees != nil && *m.cfg.Release.KeepIntegrationWorktrees
	cleanupIntegration := func() {
		if svc.IntegrationWorktreePath == "" {
			return
		}
		commonDir, err := m.git.CommonDir(ctx, svc.IntegrationWorktreePath)
		if err == nil {
			_ = m.git.RemoveWorktree(ctx, commonDir, svc.IntegrationWorktreePath, true)
		}
		_ = os.RemoveAll(svc.IntegrationWorktreePath)
		svc.IntegrationWorktreePath = ""
	}

	for fbIdx := range svc.FeatureBranches {
		fb := &svc.FeatureBranches[fbIdx]
		if fb.Merged {
			continue
		}
		if err := m.git.Merge(ctx, integrationPath, fb.Branch); err != nil {
			states, stateErr := m.git.OperationState(ctx, integrationPath)
			if stateErr == nil && containsMergeConflictState(states) {
				_ = m.git.MergeAbort(ctx, integrationPath)
				return fmt.Errorf("%w: service=%s branch=%s", ErrReleaseMergeConflict, svc.Name, fb.Branch)
			}
			return err
		}
		fb.Merged = true
		fb.MergeRef = fb.Branch
		if err := m.persistCheckpoint(release, "merge", nil); err != nil {
			return err
		}
	}

	if svc.PostIntegrationRef == "" {
		svc.PostIntegrationRef = svc.IntegrationBranch
	}
	if svc.PostIntegrationSHA == "" {
		postIntegrationSHA, resolveErr := resolveReleaseRefSHA(ctx, integrationPath, "HEAD")
		if resolveErr != nil {
			return resolveErr
		}
		svc.PostIntegrationSHA = postIntegrationSHA
	}

	if m.cfg.Release != nil && m.cfg.Release.PushIntegration != nil && *m.cfg.Release.PushIntegration && !svc.PushedIntegration {
		if err := m.git.PushBranchExplicit(ctx, integrationPath, svc.IntegrationBranch); err != nil {
			return fmt.Errorf("%w: service=%s integration=%s: %v", ErrReleaseOperationInProgress, svc.Name, svc.IntegrationBranch, err)
		}
		svc.PushedIntegration = true
		if err := m.persistCheckpoint(release, "push_integration", nil); err != nil {
			return err
		}
	}

	if err := m.moveReleaseStatus(release, domain.ReleaseStatusBranching, "branch", nil); err != nil {
		return err
	}
	branchExists, err := m.git.BranchExists(ctx, svc.RepoPath, svc.ReleaseBranch)
	if err != nil {
		return err
	}
	if !branchExists {
		if err := m.git.CreateBranchFromBranch(ctx, svc.RepoPath, svc.ReleaseBranch, svc.IntegrationBranch); err != nil {
			return err
		}
		svc.ReleaseRef = svc.ReleaseBranch
		if err := m.persistCheckpoint(release, "branch", nil); err != nil {
			return err
		}
	}
	if svc.ReleaseRef == "" {
		svc.ReleaseRef = svc.ReleaseBranch
	}
	if svc.ReleaseSHA == "" {
		releaseSHA, resolveErr := resolveReleaseRefSHA(ctx, svc.RepoPath, svc.ReleaseBranch)
		if resolveErr != nil {
			return resolveErr
		}
		svc.ReleaseSHA = releaseSHA
	}

	pushPath := integrationPath
	if m.cfg.Release != nil && m.cfg.Release.CreateReleaseWorktrees != nil && *m.cfg.Release.CreateReleaseWorktrees {
		if svc.ReleaseWorktreePath == "" {
			releaseWorktreePath := filepath.Join(release.Dir, "services", svc.Name)
			if err := os.MkdirAll(filepath.Dir(releaseWorktreePath), 0o755); err != nil {
				return fmt.Errorf("%w: %v", ErrReleaseManifestInvalid, err)
			}
			if err := m.git.AddWorktree(ctx, svc.RepoPath, releaseWorktreePath, svc.ReleaseBranch, false, ""); err != nil {
				return err
			}
			svc.ReleaseWorktreePath = releaseWorktreePath
		}
		pushPath = svc.ReleaseWorktreePath
	}

	if err := m.moveReleaseStatus(release, domain.ReleaseStatusTagging, "tag", nil); err != nil {
		return err
	}
	tagExists, err := m.git.TagExists(ctx, svc.RepoPath, svc.Tag)
	if err != nil {
		return err
	}
	if !tagExists {
		if err := m.git.CreateTag(ctx, svc.RepoPath, svc.Tag, svc.ReleaseBranch, "wtui release "+release.ID); err != nil {
			return err
		}
		svc.TagRef = svc.Tag
		if err := m.persistCheckpoint(release, "tag", nil); err != nil {
			return err
		}
	}
	if svc.TagRef == "" {
		svc.TagRef = svc.Tag
	}
	if svc.TagSHA == "" {
		tagSHA, resolveErr := resolveReleaseRefSHA(ctx, svc.RepoPath, svc.Tag+"^{}")
		if resolveErr != nil {
			return resolveErr
		}
		svc.TagSHA = tagSHA
	}

	if m.cfg.Release != nil && m.cfg.Release.PushReleaseBranches != nil && *m.cfg.Release.PushReleaseBranches && !svc.PushedReleaseBranch {
		if release.Status != domain.ReleaseStatusPushing {
			if err := m.moveReleaseStatus(release, domain.ReleaseStatusPushing, "push_branch", nil); err != nil {
				return err
			}
		}
		if err := m.git.PushBranchExplicit(ctx, pushPath, svc.ReleaseBranch); err != nil {
			return fmt.Errorf("%w: service=%s branch=%s: %v", ErrReleaseOperationInProgress, svc.Name, svc.ReleaseBranch, err)
		}
		svc.PushedReleaseBranch = true
		if err := m.persistCheckpoint(release, "push_branch", nil); err != nil {
			return err
		}
	}

	if m.cfg.Release != nil && m.cfg.Release.PushTags != nil && *m.cfg.Release.PushTags && !svc.PushedTag {
		if err := m.git.PushTag(ctx, pushPath, svc.Tag); err != nil {
			return fmt.Errorf("%w: service=%s tag=%s: %v", ErrReleaseOperationInProgress, svc.Name, svc.Tag, err)
		}
		svc.PushedTag = true
		if err := m.persistCheckpoint(release, "push_tag", nil); err != nil {
			return err
		}
	}

	svc.Status = domain.ReleaseStatusReleased
	if err := m.persistCheckpoint(release, "service_done", nil); err != nil {
		return err
	}

	if !keepIntegration {
		cleanupIntegration()
		_ = m.persistCheckpoint(release, "cleanup", nil)
	}

	return nil
}

func serviceRetryCompleted(svc domain.ReleaseService) bool {
	return svc.Status == domain.ReleaseStatusReleased || svc.PushedTag
}

func (m *manager) ensureRetryFinalizeStatus(release *domain.Release) error {
	switch release.Status {
	case domain.ReleaseStatusMerging:
		if err := m.moveReleaseStatus(release, domain.ReleaseStatusBranching, "branch", nil); err != nil {
			return err
		}
		fallthrough
	case domain.ReleaseStatusBranching:
		if err := m.moveReleaseStatus(release, domain.ReleaseStatusTagging, "tag", nil); err != nil {
			return err
		}
		fallthrough
	case domain.ReleaseStatusTagging:
		if err := m.moveReleaseStatus(release, domain.ReleaseStatusPushing, "push_tag", nil); err != nil {
			return err
		}
	case domain.ReleaseStatusPushing:
		return nil
	default:
		return fmt.Errorf("%w: %s -> released", ErrReleaseInvalidStatusTransition, release.Status)
	}

	return nil
}
