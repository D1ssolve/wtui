package task

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
)

func (m *manager) CreateRelease(ctx context.Context, params CreateReleaseParams) (domain.Release, error) {
	plan, err := m.buildReleasePlan(ctx, params)
	if err != nil {
		return domain.Release{}, err
	}

	releaseVersion := params.SharedVersion
	if strings.TrimSpace(releaseVersion) == "" && len(plan.Services) > 0 {
		releaseVersion = plan.Services[0].Version
	}

	releaseID, err := generateReleaseID(m.cfg.Release.IDFormat, releaseVersion, nil)
	if err != nil {
		return domain.Release{}, fmt.Errorf("%w: %v", ErrReleaseManifestInvalid, err)
	}

	now := defaultReleaseNow().UTC()
	release := domain.Release{
		ID:         releaseID,
		Status:     domain.ReleaseStatusDraft,
		Checkpoint: "draft",
		Version:    strings.TrimSpace(releaseVersion),
		TaskIDs:    append([]string(nil), plan.TaskIDs...),
		Tasks:      append([]domain.ReleaseTaskRef(nil), plan.Tasks...),
		Services:   append([]domain.ReleaseService(nil), plan.Services...),
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	release.Tag = sharedTagOrEmpty(release.Services)

	if _, err := m.ensureReleaseDir(release.ID, false); err != nil {
		return domain.Release{}, err
	}

	release, err = m.writeReleaseManifest(release)
	if err != nil {
		return domain.Release{}, err
	}

	if !params.StartImmediately {
		return release, nil
	}

	if err := m.moveReleaseStatus(&release, domain.ReleaseStatusValidating, "validating", nil); err != nil {
		return release, err
	}
	startedAt := defaultReleaseNow().UTC()
	release.StartedAt = &startedAt
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
		if execErr := m.executeReleaseService(ctx, &release, svc, params.StatusCh); execErr != nil {
			releaseErr := classifyReleaseError(execErr, svc)
			_ = m.failRelease(&release, releaseErr)
			return release, execErr
		}
	}

	if err := m.ensureReleaseReadyForReleased(&release); err != nil {
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

func (m *manager) executeReleaseService(ctx context.Context, release *domain.Release, svc *domain.ReleaseService, statusCh chan<- string) (err error) {
	svc.Status = domain.ReleaseStatusMerging
	if err := m.persistCheckpoint(release, "fetch", nil); err != nil {
		return err
	}
	sendStatus(statusCh, fmt.Sprintf("[%s][fetch] fetching %s", svc.Name, svc.RepoPath))
	if err := m.git.Fetch(ctx, svc.RepoPath); err != nil {
		return fmt.Errorf("%w: service=%s: %v", ErrReleaseOperationInProgress, svc.Name, err)
	}

	integrationPath := filepath.Join(release.Dir, ".work", svc.Name+"-integration")
	if err := os.MkdirAll(filepath.Dir(integrationPath), 0o755); err != nil {
		return fmt.Errorf("%w: %v", ErrReleaseManifestInvalid, err)
	}
	sendStatus(statusCh, fmt.Sprintf("[%s][worktree] preparing integration worktree", svc.Name))
	if err := m.git.AddWorktree(ctx, svc.RepoPath, integrationPath, svc.IntegrationBranch, false, ""); err != nil {
		return err
	}
	svc.IntegrationWorktreePath = integrationPath
	svc.PreIntegrationRef = svc.IntegrationBranch
	preIntegrationSHA, err := resolveReleaseRefSHA(ctx, svc.RepoPath, svc.IntegrationBranch)
	if err != nil {
		return err
	}
	svc.PostIntegrationSHA = preIntegrationSHA
	if err := m.persistCheckpoint(release, "integration_worktree", nil); err != nil {
		return err
	}

	keepIntegration := m.cfg.Release != nil && m.cfg.Release.KeepIntegrationWorktrees != nil && *m.cfg.Release.KeepIntegrationWorktrees
	mergeConflict := false
	cleanupIntegration := func() {
		commonDir, err := m.git.CommonDir(ctx, integrationPath)
		if err == nil {
			_ = m.git.RemoveWorktree(ctx, commonDir, integrationPath, true)
		}
		_ = os.RemoveAll(integrationPath)
		svc.IntegrationWorktreePath = ""
	}
	defer func() {
		if svc.IntegrationWorktreePath == "" {
			return
		}
		if err == nil {
			if !keepIntegration {
				cleanupIntegration()
				_ = m.persistCheckpoint(release, "cleanup", nil)
			}
			return
		}
		if keepIntegration || mergeConflict {
			return
		}
		cleanupIntegration()
		_ = m.persistCheckpoint(release, "cleanup", nil)
	}()

	sendStatus(statusCh, fmt.Sprintf("[%s][merge] merging feature branches", svc.Name))
	for fbIdx := range svc.FeatureBranches {
		fb := &svc.FeatureBranches[fbIdx]
		sendStatus(statusCh, fmt.Sprintf("[%s][merge] %s", svc.Name, fb.Branch))
		if err := m.git.Merge(ctx, integrationPath, fb.Branch); err != nil {
			states, stateErr := m.git.OperationState(ctx, integrationPath)
			if stateErr == nil && containsMergeConflictState(states) {
				mergeConflict = true
				_ = m.git.MergeAbort(ctx, integrationPath)
				return fmt.Errorf("%w: service=%s branch=%s", ErrReleaseMergeConflict, svc.Name, fb.Branch)
			}
			return err
		}
		fb.Merged = true
		mergeSHA, resolveErr := resolveReleaseRefSHA(ctx, svc.RepoPath, fb.Branch)
		if resolveErr != nil {
			return resolveErr
		}
		fb.MergeRef = mergeSHA
		if err := m.persistCheckpoint(release, "merge", nil); err != nil {
			return err
		}
	}

	svc.PostIntegrationRef = svc.IntegrationBranch
	postIntegrationSHA, err := resolveReleaseRefSHA(ctx, integrationPath, "HEAD")
	if err != nil {
		return err
	}
	svc.PostIntegrationSHA = postIntegrationSHA

	if m.cfg.Release != nil && m.cfg.Release.PushIntegration != nil && *m.cfg.Release.PushIntegration {
		sendStatus(statusCh, fmt.Sprintf("[%s][push] pushing integration branch %s", svc.Name, svc.IntegrationBranch))
		if err := m.git.PushBranchExplicit(ctx, integrationPath, svc.IntegrationBranch); err != nil {
			return fmt.Errorf("%w: service=%s integration=%s: %v", ErrReleaseOperationInProgress, svc.Name, svc.IntegrationBranch, err)
		}
		svc.PushedIntegration = true
		if err := m.persistCheckpoint(release, "push_integration", nil); err != nil {
			return err
		}
	}

	svc.Status = domain.ReleaseStatusBranching
	branchExists, err := m.git.BranchExists(ctx, svc.RepoPath, svc.ReleaseBranch)
	if err != nil {
		return err
	}
	if branchExists {
		return fmt.Errorf("%w: service=%s branch=%s", ErrReleaseBranchExists, svc.Name, svc.ReleaseBranch)
	}

	sendStatus(statusCh, fmt.Sprintf("[%s][branch] creating %s", svc.Name, svc.ReleaseBranch))
	if err := m.git.CreateBranchFromBranch(ctx, svc.RepoPath, svc.ReleaseBranch, svc.IntegrationBranch); err != nil {
		return err
	}
	svc.ReleaseRef = svc.ReleaseBranch
	releaseSHA, err := resolveReleaseRefSHA(ctx, svc.RepoPath, svc.ReleaseBranch)
	if err != nil {
		return err
	}
	svc.ReleaseSHA = releaseSHA
	if err := m.persistCheckpoint(release, "branch", nil); err != nil {
		return err
	}

	pushPath := integrationPath
	if m.cfg.Release != nil && m.cfg.Release.CreateReleaseWorktrees != nil && *m.cfg.Release.CreateReleaseWorktrees {
		releaseWorktreePath := filepath.Join(release.Dir, "services", svc.Name)
		if err := os.MkdirAll(filepath.Dir(releaseWorktreePath), 0o755); err != nil {
			return fmt.Errorf("%w: %v", ErrReleaseManifestInvalid, err)
		}
		sendStatus(statusCh, fmt.Sprintf("[%s][worktree] creating release worktree", svc.Name))
		if err := m.git.AddWorktree(ctx, svc.RepoPath, releaseWorktreePath, svc.ReleaseBranch, false, ""); err != nil {
			return err
		}
		svc.ReleaseWorktreePath = releaseWorktreePath
		pushPath = releaseWorktreePath
	}

	svc.Status = domain.ReleaseStatusTagging
	tagExists, err := m.git.TagExists(ctx, svc.RepoPath, svc.Tag)
	if err != nil {
		return err
	}
	if tagExists {
		return fmt.Errorf("%w: service=%s tag=%s", ErrReleaseTagExists, svc.Name, svc.Tag)
	}

	sendStatus(statusCh, fmt.Sprintf("[%s][tag] creating %s", svc.Name, svc.Tag))
	if err := m.git.CreateTag(ctx, svc.RepoPath, svc.Tag, svc.ReleaseBranch, "wtui release "+release.ID); err != nil {
		return err
	}
	svc.TagRef = svc.Tag
	tagSHA, err := resolveReleaseRefSHA(ctx, svc.RepoPath, svc.Tag+"^{}")
	if err != nil {
		return err
	}
	svc.TagSHA = tagSHA
	if err := m.persistCheckpoint(release, "tag", nil); err != nil {
		return err
	}

	svc.Status = domain.ReleaseStatusPushing
	if m.cfg.Release != nil && m.cfg.Release.PushReleaseBranches != nil && *m.cfg.Release.PushReleaseBranches {
		sendStatus(statusCh, fmt.Sprintf("[%s][push] pushing release branch %s", svc.Name, svc.ReleaseBranch))
		if err := m.git.PushBranchExplicit(ctx, pushPath, svc.ReleaseBranch); err != nil {
			return fmt.Errorf("%w: service=%s branch=%s: %v", ErrReleaseOperationInProgress, svc.Name, svc.ReleaseBranch, err)
		}
		svc.PushedReleaseBranch = true
		if err := m.persistCheckpoint(release, "push_branch", nil); err != nil {
			return err
		}
	}

	if m.cfg.Release != nil && m.cfg.Release.PushTags != nil && *m.cfg.Release.PushTags {
		sendStatus(statusCh, fmt.Sprintf("[%s][push] pushing tag %s", svc.Name, svc.Tag))
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

	sendStatus(statusCh, fmt.Sprintf("[%s][done] released", svc.Name))
	return nil
}

func (m *manager) ensureReleaseReadyForReleased(release *domain.Release) error {
	switch release.Status {
	case domain.ReleaseStatusMerging:
		if err := m.moveReleaseStatus(release, domain.ReleaseStatusBranching, "branch", nil); err != nil {
			return err
		}
		if _, err := m.writeReleaseManifest(*release); err != nil {
			return err
		}
		fallthrough
	case domain.ReleaseStatusBranching:
		if err := m.moveReleaseStatus(release, domain.ReleaseStatusTagging, "tag", nil); err != nil {
			return err
		}
		if _, err := m.writeReleaseManifest(*release); err != nil {
			return err
		}
		fallthrough
	case domain.ReleaseStatusTagging:
		if err := m.moveReleaseStatus(release, domain.ReleaseStatusPushing, "push_tag", nil); err != nil {
			return err
		}
		if _, err := m.writeReleaseManifest(*release); err != nil {
			return err
		}
	case domain.ReleaseStatusPushing:
		return nil
	default:
		return fmt.Errorf("%w: %s -> released", ErrReleaseInvalidStatusTransition, release.Status)
	}

	return nil
}

var releaseResolveRefSHA = func(ctx context.Context, repoPath, ref string) (string, error) {
	out, runErr := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", ref).Output()
	if runErr != nil {
		return "", runErr
	}
	return strings.TrimSpace(string(out)), nil
}

func resolveReleaseRefSHA(ctx context.Context, repoPath, ref string) (string, error) {
	sha, err := releaseResolveRefSHA(ctx, repoPath, ref)
	if err != nil {
		return "", fmt.Errorf("release: resolve ref sha repo=%s ref=%s: %w", repoPath, ref, err)
	}
	if strings.TrimSpace(sha) == "" {
		return "", fmt.Errorf("release: resolve ref sha repo=%s ref=%s: empty result", repoPath, ref)
	}
	return sha, nil
}

func (m *manager) moveReleaseStatus(release *domain.Release, status domain.ReleaseStatus, checkpoint string, releaseErr *domain.ReleaseError) error {
	if release.Status != status {
		if err := validateReleaseStatusTransition(release.Status, status); err != nil {
			return err
		}
		release.Status = status
	}
	release.Checkpoint = checkpoint
	release.Error = releaseErr
	release.UpdatedAt = defaultReleaseNow().UTC()
	return nil
}

func (m *manager) persistCheckpoint(release *domain.Release, checkpoint string, releaseErr *domain.ReleaseError) error {
	release.Checkpoint = checkpoint
	release.Error = releaseErr
	release.UpdatedAt = defaultReleaseNow().UTC()
	_, err := m.writeReleaseManifest(*release)
	if err != nil {
		return err
	}
	return nil
}

func (m *manager) failRelease(release *domain.Release, releaseErr *domain.ReleaseError) error {
	release.Status = domain.ReleaseStatusFailed
	release.Checkpoint = "failed"
	release.Error = releaseErr
	now := defaultReleaseNow().UTC()
	release.UpdatedAt = now
	release.CompletedAt = &now
	_, err := m.writeReleaseManifest(*release)
	return err
}

func classifyReleaseError(err error, svc *domain.ReleaseService) *domain.ReleaseError {
	if errors.Is(err, ErrReleaseMergeConflict) {
		msg := ErrReleaseMergeConflict.Error()
		if svc != nil {
			svc.Status = domain.ReleaseStatusFailed
			svc.Error = &domain.ReleaseError{
				Code:        "ERR_RELEASE_MERGE_CONFLICT",
				Message:     msg,
				Stage:       "merge",
				ServiceName: svc.Name,
				Recoverable: true,
				Cause:       err.Error(),
			}
			return svc.Error
		}
	}

	if errors.Is(err, ErrReleaseBranchExists) {
		return &domain.ReleaseError{Code: "ERR_RELEASE_BRANCH_EXISTS", Message: ErrReleaseBranchExists.Error(), Stage: "branch", Recoverable: true, Cause: err.Error()}
	}
	if errors.Is(err, ErrReleaseTagExists) {
		return &domain.ReleaseError{Code: "ERR_RELEASE_TAG_EXISTS", Message: ErrReleaseTagExists.Error(), Stage: "tag", Recoverable: true, Cause: err.Error()}
	}

	if svc != nil {
		svc.Status = domain.ReleaseStatusFailed
		svc.Error = &domain.ReleaseError{Code: "ERR_RELEASE_PUSH_FAILED", Message: "release step failed", Stage: "push", ServiceName: svc.Name, Recoverable: true, Cause: err.Error()}
		return svc.Error
	}

	return &domain.ReleaseError{Code: "ERR_RELEASE_PUSH_FAILED", Message: "release step failed", Stage: "push", Recoverable: true, Cause: err.Error()}
}

func containsMergeConflictState(states []domain.RepoState) bool {
	for _, state := range states {
		if state == domain.RepoStateMerging || state == domain.RepoStateConflicted {
			return true
		}
	}
	return false
}

func sharedTagOrEmpty(services []domain.ReleaseService) string {
	if len(services) == 0 {
		return ""
	}
	shared := services[0].Tag
	for i := 1; i < len(services); i++ {
		if services[i].Tag != shared {
			return ""
		}
	}
	return shared
}
