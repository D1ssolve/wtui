package task

import (
	"context"
	"fmt"
	"os"

	"github.com/D1ssolve/wtui/internal/domain"
)

func (m *manager) FinishRelease(ctx context.Context, params FinishReleaseParams) (domain.Release, error) {
	release, err := m.GetRelease(ctx, params.ReleaseID)
	if err != nil {
		return domain.Release{}, err
	}

	if release.Status != domain.ReleaseStatusPrepared {
		return domain.Release{}, fmt.Errorf("%w: %s -> tagging", ErrReleaseInvalidStatusTransition, release.Status)
	}

	statusCh := params.StatusCh

	// Preflight: validate every service is in a safe state before any irreversible tag creation.
	for i := range release.Services {
		svc := &release.Services[i]
		if svc.Status == domain.ReleaseStatusReleased || svc.PushedTag {
			continue
		}
		sendStatus(statusCh, fmt.Sprintf("[%s][finish_fetch] fetching %s", svc.Name, svc.RepoPath))
		if err := m.git.Fetch(ctx, svc.RepoPath); err != nil {
			releaseErr := &domain.ReleaseError{
				Code:        "ERR_RELEASE_FETCH",
				Message:     fmt.Sprintf("fetch failed for %s: %v", svc.Name, err),
				Stage:       "finish_fetch",
				ServiceName: svc.Name,
				Recoverable: true,
				Cause:       err.Error(),
			}
			_ = m.failRelease(&release, releaseErr)
			loaded, _ := m.loadReleaseManifest(release.ID)
			if loaded.ID != "" {
				release = loaded
			}
			return release, fmt.Errorf("%w: service=%s stage=finish_fetch: %w", ErrReleaseOperationInProgress, svc.Name, err)
		}
		if safetyErr := m.validateFinishSafety(ctx, *svc); safetyErr != nil {
			_ = m.failRelease(&release, safetyErr)
			return release, fmt.Errorf("%w: %s", ErrReleaseRetryUnsafe, safetyErr.Message)
		}
	}

	if err := m.moveReleaseStatus(&release, domain.ReleaseStatusTagging, "tagging", nil); err != nil {
		return release, err
	}
	release, err = m.writeReleaseManifest(release)
	if err != nil {
		return release, err
	}

	for i := range release.Services {
		svc := &release.Services[i]
		if execErr := m.runFinishService(ctx, &release, svc, statusCh); execErr != nil {
			releaseErr := classifyReleaseError(execErr, svc)
			_ = m.failRelease(&release, releaseErr)
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

func (m *manager) runFinishService(ctx context.Context, release *domain.Release, svc *domain.ReleaseService, statusCh chan<- string) error {
	if svc.Status == domain.ReleaseStatusReleased || svc.PushedTag {
		sendStatus(statusCh, fmt.Sprintf("[%s][done] already released", svc.Name))
		return nil
	}

	pushPath := svc.RepoPath
	if svc.ReleaseWorktreePath != "" {
		if _, err := os.Stat(svc.ReleaseWorktreePath); err == nil {
			pushPath = svc.ReleaseWorktreePath
		}
	}

	svc.Status = domain.ReleaseStatusTagging
	tagAlreadyCreated := false
	if svc.TagRef != "" && svc.TagSHA != "" {
		exists, err := m.git.TagExists(ctx, svc.RepoPath, svc.Tag)
		if err != nil {
			return err
		}
		if exists {
			currentTagSHA, err := m.resolveReleaseRefSHA(ctx, svc.RepoPath, svc.Tag+"^{}")
			if err != nil {
				return err
			}
			if currentTagSHA != svc.TagSHA {
				return fmt.Errorf("%w: service=%s tag=%s expected=%s actual=%s", ErrReleaseRetryUnsafe, svc.Name, svc.Tag, svc.TagSHA, currentTagSHA)
			}
			tagAlreadyCreated = true
		}
	}

	if !tagAlreadyCreated {
		sendStatus(statusCh, fmt.Sprintf("[%s][tag] creating %s", svc.Name, svc.Tag))
		if err := m.git.CreateTag(ctx, svc.RepoPath, svc.Tag, svc.ReleaseBranch, "wtui release "+release.ID); err != nil {
			return fmt.Errorf("%w: service=%s tag=%s: %v", ErrReleaseTagCreateFailed, svc.Name, svc.Tag, err)
		}
		svc.TagRef = svc.Tag
		tagSHA, err := m.resolveReleaseRefSHA(ctx, svc.RepoPath, svc.Tag+"^{}")
		if err != nil {
			return err
		}
		svc.TagSHA = tagSHA
	}
	if err := m.persistCheckpoint(release, "tag", nil); err != nil {
		return err
	}

	if m.cfg.Release != nil && m.cfg.Release.PushTags != nil && *m.cfg.Release.PushTags {
		svc.Status = domain.ReleaseStatusPushing
		if err := m.moveReleaseStatus(release, domain.ReleaseStatusPushing, "pushing", nil); err != nil {
			return err
		}
		if _, err := m.writeReleaseManifest(*release); err != nil {
			return err
		}
		sendStatus(statusCh, fmt.Sprintf("[%s][push] pushing tag %s", svc.Name, svc.Tag))
		if err := m.git.PushTag(ctx, pushPath, svc.Tag); err != nil {
			return fmt.Errorf("%w: service=%s tag=%s: %v", ErrReleaseTagPushFailed, svc.Name, svc.Tag, err)
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

	sendStatus(statusCh, fmt.Sprintf("[%s][done] released", svc.Name))
	return nil
}

func (m *manager) validateFinishSafety(ctx context.Context, svc domain.ReleaseService) *domain.ReleaseError {
	exists, err := m.git.BranchExists(ctx, svc.RepoPath, svc.ReleaseBranch)
	if err != nil {
		return &domain.ReleaseError{
			Code:        "ERR_RELEASE_BRANCH_MISSING",
			Message:     fmt.Sprintf("release branch check failed: %v", err),
			Stage:       "finish_validate",
			ServiceName: svc.Name,
			Recoverable: false,
			Cause:       err.Error(),
		}
	}
	if !exists {
		return &domain.ReleaseError{
			Code:        "ERR_RELEASE_BRANCH_MISSING",
			Message:     fmt.Sprintf("release branch %s missing", svc.ReleaseBranch),
			Stage:       "finish_validate",
			ServiceName: svc.Name,
			Recoverable: false,
			Cause:       "branch does not exist",
		}
	}

	if svc.ReleaseSHA != "" {
		currentSHA, resolveErr := m.resolveReleaseRefSHA(ctx, svc.RepoPath, svc.ReleaseBranch)
		if resolveErr != nil {
			return &domain.ReleaseError{
				Code:        "ERR_RELEASE_SHA_MISMATCH",
				Message:     fmt.Sprintf("release branch sha resolve failed: %v", resolveErr),
				Stage:       "finish_validate",
				ServiceName: svc.Name,
				Recoverable: false,
				Cause:       resolveErr.Error(),
			}
		}
		if currentSHA != svc.ReleaseSHA {
			return &domain.ReleaseError{
				Code:        "ERR_RELEASE_SHA_MISMATCH",
				Message:     fmt.Sprintf("release branch sha mismatch expected=%s actual=%s", svc.ReleaseSHA, currentSHA),
				Stage:       "finish_validate",
				ServiceName: svc.Name,
				Recoverable: false,
				Cause:       fmt.Sprintf("expected=%s actual=%s", svc.ReleaseSHA, currentSHA),
			}
		}
	}

	if !svc.PushedTag && svc.TagRef == "" {
		tagExists, err := m.git.TagExists(ctx, svc.RepoPath, svc.Tag)
		if err != nil {
			return &domain.ReleaseError{
				Code:        "ERR_RELEASE_TAG_CONFLICT",
				Message:     fmt.Sprintf("tag check failed: %v", err),
				Stage:       "finish_validate",
				ServiceName: svc.Name,
				Recoverable: false,
				Cause:       err.Error(),
			}
		}
		if tagExists {
			return &domain.ReleaseError{
				Code:        "ERR_RELEASE_TAG_CONFLICT",
				Message:     fmt.Sprintf("unexpected existing tag %s", svc.Tag),
				Stage:       "finish_validate",
				ServiceName: svc.Name,
				Recoverable: false,
				Cause:       "tag already exists",
			}
		}
	}

	return nil
}
