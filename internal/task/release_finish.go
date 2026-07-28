package task

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/D1ssolve/wtui/internal/domain"
)

func (m *manager) FinalizeRelease(ctx context.Context, params FinishReleaseParams) (domain.Release, error) {
	release, err := m.GetRelease(ctx, params.ReleaseID)
	if err != nil {
		return domain.Release{}, err
	}
	if IsLegacyManifest(release) {
		return release, fmt.Errorf("%w: reject release %s and prepare it again", ErrReleaseLegacyManifest, release.ID)
	}
	if release.Status != domain.ReleaseStatusMasterMerged {
		return release, fmt.Errorf("%w: %s -> %s", ErrReleaseInvalidStatusTransition, release.Status, domain.ReleaseStatusSyncingDevelop)
	}

	for i := range release.Services {
		svc := &release.Services[i]
		sendStatus(params.StatusCh, fmt.Sprintf("[%s][finalize_fetch] fetching %s", svc.Name, svc.RepoPath))
		if err := m.git.Fetch(ctx, svc.RepoPath); err != nil {
			return m.failFinalization(&release, svc, fmt.Errorf("release finalize: fetch service=%s: %w", svc.Name, err))
		}
		masterSHA, err := m.resolveReleaseRefSHA(ctx, svc.RepoPath, "origin/"+m.flow.ProductionBranch)
		if err != nil {
			return m.failFinalization(&release, svc, err)
		}
		if masterSHA != svc.AcceptedMergeSHA {
			return m.failFinalization(&release, svc, fmt.Errorf("%w: service=%s expected=%s actual=%s", ErrReleaseMasterMoved, svc.Name, svc.AcceptedMergeSHA, masterSHA))
		}
		if err := m.validateFinalizeTag(ctx, *svc); err != nil {
			return m.failFinalization(&release, svc, err)
		}
	}

	if err := m.moveReleaseStatus(&release, domain.ReleaseStatusSyncingDevelop, "syncing_develop", nil); err != nil {
		return release, err
	}
	if release, err = m.writeReleaseManifest(release); err != nil {
		return release, err
	}
	for i := range release.Services {
		if err := m.syncFinalizeService(ctx, &release, &release.Services[i], params.StatusCh); err != nil {
			return m.failFinalization(&release, &release.Services[i], err)
		}
	}

	if err := m.moveReleaseStatus(&release, domain.ReleaseStatusTagging, "tagging", nil); err != nil {
		return release, err
	}
	if release, err = m.writeReleaseManifest(release); err != nil {
		return release, err
	}
	for i := range release.Services {
		if err := m.tagFinalizeService(ctx, &release, &release.Services[i], params.StatusCh); err != nil {
			return m.failFinalization(&release, &release.Services[i], err)
		}
	}

	if err := m.moveReleaseStatus(&release, domain.ReleaseStatusPushing, "pushing", nil); err != nil {
		return release, err
	}
	if release, err = m.writeReleaseManifest(release); err != nil {
		return release, err
	}
	for i := range release.Services {
		svc := &release.Services[i]
		if m.cfg.Release != nil && m.cfg.Release.PushTags != nil && *m.cfg.Release.PushTags {
			sendStatus(params.StatusCh, fmt.Sprintf("[%s][push] pushing tag %s", svc.Name, svc.Tag))
			if err := m.git.PushTag(ctx, svc.RepoPath, svc.Tag); err != nil {
				return m.failFinalization(&release, svc, fmt.Errorf("%w: service=%s tag=%s: %v", ErrReleaseTagPushFailed, svc.Name, svc.Tag, err))
			}
			svc.PushedTag = true
		}
		svc.Status = domain.ReleaseStatusReleased
		svc.Error = nil
		if err := m.persistCheckpoint(&release, "push_tag", nil); err != nil {
			return release, err
		}
	}

	if err := m.moveReleaseStatus(&release, domain.ReleaseStatusReleased, "final", nil); err != nil {
		return release, err
	}
	completedAt := defaultReleaseNow().UTC()
	release.CompletedAt = &completedAt
	release.Error = nil
	return m.writeReleaseManifest(release)
}

func (m *manager) syncFinalizeService(ctx context.Context, release *domain.Release, svc *domain.ReleaseService, statusCh chan<- string) (err error) {
	integrationPath := filepath.Join(release.Dir, ".work", svc.Name+"-finalize-integration")
	if err := os.MkdirAll(filepath.Dir(integrationPath), 0o755); err != nil {
		return fmt.Errorf("%w: %v", ErrReleaseManifestInvalid, err)
	}
	if err := m.git.AddWorktree(ctx, svc.RepoPath, integrationPath, svc.IntegrationBranch, false, ""); err != nil {
		return err
	}
	svc.IntegrationWorktreePath = integrationPath
	svc.Status = domain.ReleaseStatusSyncingDevelop
	keep := m.cfg.Release != nil && m.cfg.Release.KeepIntegrationWorktrees != nil && *m.cfg.Release.KeepIntegrationWorktrees
	defer func() {
		if keep {
			return
		}
		commonDir, commonErr := m.git.CommonDir(ctx, integrationPath)
		if commonErr == nil {
			_ = m.git.RemoveWorktree(ctx, commonDir, integrationPath, true)
		}
		_ = os.RemoveAll(integrationPath)
		svc.IntegrationWorktreePath = ""
	}()
	if err := m.persistCheckpoint(release, "integration_worktree", nil); err != nil {
		return err
	}

	if err := m.git.MergeFFOnly(ctx, integrationPath, "origin/"+svc.IntegrationBranch); err != nil {
		return fmt.Errorf("release finalize: fast-forward service=%s integration=%s: %w", svc.Name, svc.IntegrationBranch, err)
	}
	alreadyMerged, err := m.git.IsAncestor(ctx, svc.RepoPath, svc.ReleaseBranch, svc.IntegrationBranch)
	if err != nil {
		return err
	}
	if alreadyMerged {
		sendStatus(statusCh, fmt.Sprintf("[%s][sync] %s already contains %s", svc.Name, svc.IntegrationBranch, svc.ReleaseBranch))
	} else {
		sendStatus(statusCh, fmt.Sprintf("[%s][sync] merging %s into %s", svc.Name, svc.ReleaseBranch, svc.IntegrationBranch))
		if err := m.git.Merge(ctx, integrationPath, svc.ReleaseBranch); err != nil {
			states, stateErr := m.git.OperationState(ctx, integrationPath)
			if stateErr == nil && containsMergeConflictState(states) {
				_ = m.git.MergeAbort(ctx, integrationPath)
				return fmt.Errorf("%w: service=%s branch=%s", ErrReleaseMergeConflict, svc.Name, svc.ReleaseBranch)
			}
			return err
		}
	}

	if m.cfg.Release != nil && m.cfg.Release.PushIntegration != nil && *m.cfg.Release.PushIntegration {
		if err := m.git.PushBranchExplicit(ctx, integrationPath, svc.IntegrationBranch); err != nil {
			return fmt.Errorf("release finalize: push service=%s integration=%s: %w", svc.Name, svc.IntegrationBranch, err)
		}
		svc.PushedIntegration = true
	}
	svc.PostIntegrationRef = svc.IntegrationBranch
	sha, err := m.resolveReleaseRefSHA(ctx, integrationPath, "HEAD")
	if err != nil {
		return err
	}
	svc.PostIntegrationSHA = sha
	return m.persistCheckpoint(release, "sync_develop", nil)
}

func (m *manager) validateFinalizeTag(ctx context.Context, svc domain.ReleaseService) error {
	exists, err := m.git.TagExists(ctx, svc.RepoPath, svc.Tag)
	if err != nil || !exists {
		return err
	}
	sha, err := m.resolveReleaseRefSHA(ctx, svc.RepoPath, svc.Tag+"^{}")
	if err != nil {
		return err
	}
	if sha != svc.AcceptedMergeSHA {
		return fmt.Errorf("%w: service=%s tag=%s expected=%s actual=%s", ErrReleaseRetryUnsafe, svc.Name, svc.Tag, svc.AcceptedMergeSHA, sha)
	}
	return nil
}

func (m *manager) tagFinalizeService(ctx context.Context, release *domain.Release, svc *domain.ReleaseService, statusCh chan<- string) error {
	exists, err := m.git.TagExists(ctx, svc.RepoPath, svc.Tag)
	if err != nil {
		return err
	}
	if !exists {
		sendStatus(statusCh, fmt.Sprintf("[%s][tag] creating %s", svc.Name, svc.Tag))
		if err := m.git.CreateTag(ctx, svc.RepoPath, svc.Tag, svc.AcceptedMergeSHA, "wtui release "+release.ID); err != nil {
			return fmt.Errorf("%w: service=%s tag=%s: %v", ErrReleaseTagCreateFailed, svc.Name, svc.Tag, err)
		}
	}
	svc.TagRef = svc.Tag
	svc.TagSHA = svc.AcceptedMergeSHA
	svc.Status = domain.ReleaseStatusTagging
	return m.persistCheckpoint(release, "tag", nil)
}

func (m *manager) failFinalization(release *domain.Release, svc *domain.ReleaseService, err error) (domain.Release, error) {
	releaseErr := classifyReleaseError(err, svc)
	_ = m.failRelease(release, releaseErr)
	return *release, err
}

// retryFinishRelease uses this for unfinished service checkpoints.
func (m *manager) runFinishService(ctx context.Context, release *domain.Release, svc *domain.ReleaseService, _ chan<- string) error {
	if svc.PushedTag {
		return nil
	}
	if svc.AcceptedMergeSHA == "" {
		return fmt.Errorf("%w: service=%s accepted merge SHA missing", ErrReleaseLegacyManifest, svc.Name)
	}
	if err := m.git.CreateTag(ctx, svc.RepoPath, svc.Tag, svc.AcceptedMergeSHA, "wtui release "+release.ID); err != nil {
		return fmt.Errorf("%w: %v", ErrReleaseTagCreateFailed, err)
	}
	svc.TagRef, svc.TagSHA = svc.Tag, svc.AcceptedMergeSHA
	if m.cfg.Release != nil && m.cfg.Release.PushTags != nil && *m.cfg.Release.PushTags {
		if err := m.git.PushTag(ctx, svc.RepoPath, svc.Tag); err != nil {
			return fmt.Errorf("%w: %v", ErrReleaseTagPushFailed, err)
		}
		svc.PushedTag = true
	}
	svc.Status = domain.ReleaseStatusReleased
	return nil
}

func (m *manager) validateFinishSafety(context.Context, domain.ReleaseService) *domain.ReleaseError {
	return nil
}
