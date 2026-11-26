package task

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

var retryCheckpoints = map[string]struct{}{
	"failed":               {},
	"fetch":                {},
	"integration_worktree": {},
	"service_prepared":     {},
	"prepared":             {},
	"cleanup_prepare":      {},
	"finish_fetch":         {},
	"merge":                {},
	"push_integration":     {},
	"branch":               {},
	"tag":                  {},
	"push_branch":          {},
	"push_tag":             {},
	"publish_release":      {},
	"service_done":         {},
	"cleanup":              {},
	"final":                {},
}

func (m *manager) RetryRelease(ctx context.Context, releaseID string) (domain.Release, error) {
	unlock, err := m.lockRelease(ctx, releaseID)
	if err != nil {
		return domain.Release{}, err
	}
	defer unlock()
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
	if err := m.ensureReleaseFlowUnchanged(release); err != nil {
		return domain.Release{}, err
	}

	if hasTaskMR, _ := awaitingTaskMergeMetadataState(release); release.PreparedAt == nil && hasTaskMR {
		return m.retryAcceptedPreparation(ctx, release)
	}

	if !IsLegacyManifest(release) {
		st, err := m.releaseState(release)
		if err != nil {
			return domain.Release{}, err
		}
		if st.Kind != gitflow.StateRun {
			return domain.Release{}, fmt.Errorf("%w: failed release %s is not in a run state", ErrReleaseInvalidStatusTransition, release.ID)
		}
		if release.PreparedAt == nil {
			if err := m.validateRetrySafety(ctx, release); err != nil {
				return domain.Release{}, err
			}
		}
		release.State = st.ID
		release.Error = nil
		release.CompletedAt = nil
		for i := range release.Services {
			release.Services[i].Error = nil
		}
		return m.runReleaseMachine(ctx, release, &releaseRun{retry: true})
	}

	if err := m.validateRetrySafety(ctx, release); err != nil {
		return domain.Release{}, err
	}

	if release.PreparedAt != nil {
		return m.retryFinishRelease(ctx, release)
	}
	return m.retryPrepareRelease(ctx, release)
}

func releaseHasAcceptedMerges(release domain.Release) bool {
	if len(release.Services) == 0 {
		return false
	}
	for _, svc := range release.Services {
		if svc.AcceptedMergeSHA == "" {
			return false
		}
	}
	return true
}

func (m *manager) retryPrepareRelease(ctx context.Context, release domain.Release) (domain.Release, error) {
	if err := m.moveReleaseStatus(&release, domain.ReleaseStatusValidating, "validating", nil); err != nil {
		return domain.Release{}, err
	}
	if release.StartedAt == nil {
		startedAt := defaultReleaseNow().UTC()
		release.StartedAt = &startedAt
	}
	release.CompletedAt = nil
	release, err := m.writeReleaseManifest(release)
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
		if servicePrepareRetryCompleted(*svc) {
			svc.Status = domain.ReleaseStatusPrepared
			if err := m.persistCheckpoint(&release, "service_prepared", nil); err != nil {
				return domain.Release{}, err
			}
			continue
		}

		if execErr := m.retryPrepareService(ctx, &release, svc); execErr != nil {
			releaseErr := classifyReleaseError(execErr, svc)
			_ = m.failRelease(&release, releaseErr)
			return release, execErr
		}
	}

	if err := m.ensureReleaseReadyForPrepared(&release); err != nil {
		return release, err
	}
	preparedAt := defaultReleaseNow().UTC()
	release.PreparedAt = &preparedAt
	release.Error = nil

	release, err = m.writeReleaseManifest(release)
	if err != nil {
		return domain.Release{}, err
	}

	return release, nil
}

func (m *manager) retryFinishRelease(ctx context.Context, release domain.Release) (domain.Release, error) {
	release.CompletedAt = nil
	if err := m.moveReleaseStatus(&release, domain.ReleaseStatusPrepared, "prepared", nil); err != nil {
		return domain.Release{}, err
	}
	release, err := m.writeReleaseManifest(release)
	if err != nil {
		return domain.Release{}, err
	}

	if err := m.moveReleaseStatus(&release, domain.ReleaseStatusTagging, "tag", nil); err != nil {
		return release, err
	}
	release, err = m.writeReleaseManifest(release)
	if err != nil {
		return domain.Release{}, err
	}

	for i := range release.Services {
		svc := &release.Services[i]
		if serviceFinishRetryCompleted(*svc) {
			svc.Status = domain.ReleaseStatusReleased
			if err := m.persistCheckpoint(&release, "service_done", nil); err != nil {
				return domain.Release{}, err
			}
			continue
		}

		if execErr := m.runFinishService(ctx, &release, svc, nil); execErr != nil {
			releaseErr := classifyReleaseError(execErr, svc)
			_ = m.failRelease(&release, releaseErr)
			return release, execErr
		}
	}
	// This retry tags outside the machine, so it runs the flow's publish steps itself.
	for _, step := range m.releaseMachine().Steps() {
		if step.Kind != gitflow.StepPublish {
			continue
		}
		if execErr := m.publishReleases(ctx, &release, step, nil); execErr != nil {
			var svcErr *releaseServiceError
			var svc *domain.ReleaseService
			if errors.As(execErr, &svcErr) {
				svc = svcErr.svc
			}
			_ = m.failRelease(&release, classifyReleaseError(execErr, svc))
			return release, execErr
		}
	}

	if err := m.ensureReleaseReadyForReleased(&release); err != nil {
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

func servicePrepareRetryCompleted(svc domain.ReleaseService) bool {
	return svc.Status == domain.ReleaseStatusPrepared || svc.PushedReleaseBranch
}

func serviceFinishRetryCompleted(svc domain.ReleaseService) bool {
	return svc.Status == domain.ReleaseStatusReleased || svc.PushedTag
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

// storedIntegrationRef reads a checkpointed Pre/PostIntegrationRef unambiguously. Older
// manifests saved the short "<remote>/<branch>" tracking name, which a same-named local
// branch shadows; a full ref, a commit SHA or a slash-free name is used as is.
func storedIntegrationRef(ref string) string {
	if ref == "" || strings.HasPrefix(ref, "refs/") || !strings.Contains(ref, "/") {
		return ref
	}
	return "refs/remotes/" + ref
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
			ref := storedIntegrationRef(svc.PostIntegrationRef)
			if ref == "" {
				ref = svc.IntegrationBranch
			}
			currentSHA, resolveErr := m.resolveReleaseRefSHA(ctx, svc.RepoPath, ref)
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
			currentSHA, resolveErr := m.resolveReleaseRefSHA(ctx, svc.RepoPath, svc.ReleaseBranch)
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
			currentSHA, resolveErr := m.resolveReleaseRefSHA(ctx, svc.RepoPath, svc.Tag+"^{}")
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
			ok, err := m.git.IsAncestor(ctx, svc.RepoPath, storedIntegrationRef(svc.PostIntegrationRef), svc.ReleaseRef)
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

func (m *manager) retryPrepareService(ctx context.Context, release *domain.Release, svc *domain.ReleaseService) error {
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
		if err := m.git.AddDetachedWorktree(ctx, svc.RepoPath, integrationPath, m.cfg.ServiceRemote().Tracking(svc.IntegrationBranch)); err != nil {
			return err
		}
		svc.IntegrationWorktreePath = integrationPath
		if err := m.persistCheckpoint(release, "integration_worktree", nil); err != nil {
			return err
		}
	}

	keepIntegration := m.cfg.Release != nil && m.cfg.Release.KeepIntegrationWorktrees != nil && *m.cfg.Release.KeepIntegrationWorktrees
	cleanupIntegration := func() error {
		if svc.IntegrationWorktreePath == "" {
			return nil
		}
		// A validation failure retains IntegrationWorktreePath so the
		// manifest stays truthful and the next retry reuses the worktree.
		if err := m.removeOwnedIntegrationWorktree(ctx, release, svc, svc.IntegrationWorktreePath, svc.PostIntegrationSHA); err != nil {
			return err
		}
		svc.IntegrationWorktreePath = ""
		return nil
	}

	if err := m.git.MergeFFOnly(ctx, integrationPath, m.cfg.ServiceRemote().Tracking(svc.IntegrationBranch)); err != nil {
		return fmt.Errorf("release prepare retry: fast-forward service=%s integration=%s: %w", svc.Name, svc.IntegrationBranch, err)
	}

	for fbIdx := range svc.FeatureBranches {
		fb := &svc.FeatureBranches[fbIdx]
		merged, verifyErr := m.branchMergedInto(ctx, svc.RepoPath, fb.Branch, m.cfg.ServiceRemote().Tracking(svc.IntegrationBranch))
		if verifyErr != nil {
			return verifyErr
		}
		if !merged {
			return fmt.Errorf("%w: service=%s branch=%s integration="+m.cfg.ServiceRemote().String()+"/%s", ErrReleaseTaskNotMerged, svc.Name, fb.Branch, svc.IntegrationBranch)
		}
		fb.Merged = true
		mergeSHA, resolveErr := m.resolveReleaseRefSHA(ctx, svc.RepoPath, fb.Branch)
		if resolveErr != nil {
			return resolveErr
		}
		fb.MergeRef = mergeSHA
		if err := m.persistCheckpoint(release, "merge", nil); err != nil {
			return err
		}
	}

	if svc.PreIntegrationRef == "" {
		svc.PreIntegrationRef = m.cfg.ServiceRemote().Tracking(svc.IntegrationBranch)
	}
	if svc.PostIntegrationRef == "" {
		svc.PostIntegrationRef = m.cfg.ServiceRemote().Tracking(svc.IntegrationBranch)
	}
	if svc.PostIntegrationSHA == "" {
		postIntegrationSHA, resolveErr := m.resolveReleaseRefSHA(ctx, integrationPath, "HEAD")
		if resolveErr != nil {
			return resolveErr
		}
		svc.PostIntegrationSHA = postIntegrationSHA
	}

	if m.cfg.Release != nil && m.cfg.Release.PushIntegration != nil && *m.cfg.Release.PushIntegration && !svc.PushedIntegration {
		if err := m.git.PushRef(ctx, integrationPath, "HEAD", svc.IntegrationBranch); err != nil {
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
		if err := m.git.CreateBranchFromBranch(ctx, svc.RepoPath, svc.ReleaseBranch, svc.PostIntegrationSHA); err != nil {
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
		releaseSHA, resolveErr := m.resolveReleaseRefSHA(ctx, svc.RepoPath, svc.ReleaseBranch)
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

	if err := m.moveReleaseStatus(release, domain.ReleaseStatusPushing, "push_branch", nil); err != nil {
		return err
	}
	if m.cfg.Release != nil && m.cfg.Release.PushReleaseBranches != nil && *m.cfg.Release.PushReleaseBranches && !svc.PushedReleaseBranch {
		if err := m.git.PushBranchExplicit(ctx, pushPath, svc.ReleaseBranch); err != nil {
			return fmt.Errorf("%w: service=%s branch=%s: %v", ErrReleaseOperationInProgress, svc.Name, svc.ReleaseBranch, err)
		}
		svc.PushedReleaseBranch = true
		if err := m.persistCheckpoint(release, "push_branch", nil); err != nil {
			return err
		}
	}

	if !keepIntegration {
		if err := cleanupIntegration(); err != nil {
			return err
		}
		if err := m.persistCheckpoint(release, "cleanup_prepare", nil); err != nil {
			return err
		}
	}

	svc.Status = domain.ReleaseStatusPrepared
	if err := m.persistCheckpoint(release, "service_prepared", nil); err != nil {
		return err
	}

	return nil
}
