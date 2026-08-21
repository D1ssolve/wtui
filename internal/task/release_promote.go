package task

import (
	"context"
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
)

func (m *manager) PromoteRelease(ctx context.Context, releaseID string, statusCh chan<- string) (domain.Release, error) {
	release, err := m.GetRelease(ctx, releaseID)
	if err != nil {
		return domain.Release{}, err
	}
	if release.Status != domain.ReleaseStatusPrepared {
		return release, fmt.Errorf("%w: %s -> %s", ErrReleaseInvalidStatusTransition, release.Status, domain.ReleaseStatusAwaitingMasterMerge)
	}
	if IsLegacyManifest(release) {
		return release, fmt.Errorf("%w: reject release %s and prepare it again", ErrReleaseLegacyManifest, release.ID)
	}

	for i := range release.Services {
		svc := &release.Services[i]
		if svc.ProductionMR != nil && strings.EqualFold(svc.ProductionMR.State, "open") {
			svc.Status = domain.ReleaseStatusAwaitingMasterMerge
			sendStatus(statusCh, fmt.Sprintf("[%s][promote] production MR already open: %s", svc.Name, svc.ProductionMR.URL))
			continue
		}

		if err := m.promoteReleaseService(ctx, &release, svc, statusCh); err != nil {
			releaseErr := classifyReleaseError(err, svc)
			_ = m.failRelease(&release, releaseErr)
			return release, err
		}
	}

	if err := m.moveReleaseStatus(&release, domain.ReleaseStatusAwaitingMasterMerge, "awaiting_master_merge", nil); err != nil {
		return release, err
	}
	release, err = m.writeReleaseManifest(release)
	if err != nil {
		return release, err
	}
	sendStatus(statusCh, "[release][promote] awaiting production MR merge")
	return release, nil
}

func (m *manager) promoteReleaseService(ctx context.Context, release *domain.Release, svc *domain.ReleaseService, statusCh chan<- string) error {
	if m.flow == nil || strings.TrimSpace(m.flow.ProductionBranch) == "" {
		return fmt.Errorf("release promote: production branch is not configured")
	}

	remoteURL, err := m.git.RemoteURL(ctx, svc.RepoPath, "origin")
	if err != nil {
		return fmt.Errorf("release promote: resolve remote for service %s: %w", svc.Name, err)
	}
	repo := forge.ExtractRepoPath(remoteURL)
	if repo == "" {
		return fmt.Errorf("release promote: resolve repository path for %s: remote URL %q is not parseable", svc.Name, remoteURL)
	}
	client, err := m.forgeClientForService(ctx, domain.Service{Name: svc.Name, WorktreePath: svc.RepoPath})
	if err != nil {
		return err
	}

	sourceSHA, err := m.resolveReleaseRefSHA(ctx, svc.RepoPath, svc.ReleaseBranch)
	if err != nil {
		return err
	}
	worktreePath := svc.ReleaseWorktreePath
	if worktreePath == "" {
		worktreePath = svc.RepoPath
	}
	mrs, err := client.MRStatus(ctx, svc.ReleaseBranch, repo)
	if err != nil {
		return fmt.Errorf("release promote: inspect production MR for service %s: %w", svc.Name, err)
	}
	for _, mr := range mrs {
		open := strings.EqualFold(mr.State, "open") || strings.EqualFold(mr.State, "opened")
		wrongTarget := mr.TargetBranch != "" && !strings.EqualFold(mr.TargetBranch, m.flow.ProductionBranch)
		if !open || wrongTarget || mr.Number == 0 {
			continue
		}
		svc.ProductionMR = &domain.ProductionMRRef{Number: mr.Number, URL: mr.URL, SourceSHA: sourceSHA, State: "open"}
		svc.Status = domain.ReleaseStatusAwaitingMasterMerge
		if err := m.persistCheckpoint(release, "production_mr", nil); err != nil {
			return err
		}
		sendStatus(statusCh, fmt.Sprintf("[%s][promote] production MR already open: %s", svc.Name, mr.URL))
		return nil
	}
	sendStatus(statusCh, fmt.Sprintf("[%s][promote] creating %s -> %s MR", svc.Name, svc.ReleaseBranch, m.flow.ProductionBranch))
	mr, err := client.CreateMR(ctx, forge.CreateMRParams{
		WorktreePath: worktreePath,
		SourceBranch: svc.ReleaseBranch,
		TargetBranch: m.flow.ProductionBranch,
		Title:        fmt.Sprintf("Release %s (%s)", svc.Version, svc.Name),
		Description:  fmt.Sprintf("Release %s", release.ID),
		Repo:         repo,
	})
	if err != nil {
		return fmt.Errorf("release promote: create production MR for service %s: %w", svc.Name, err)
	}

	svc.ProductionMR = &domain.ProductionMRRef{Number: mr.Number, URL: mr.URL, SourceSHA: sourceSHA, State: "open"}
	svc.Status = domain.ReleaseStatusAwaitingMasterMerge
	if err := m.persistCheckpoint(release, "production_mr", nil); err != nil {
		return err
	}
	sendStatus(statusCh, fmt.Sprintf("[%s][promote] production MR open: %s", svc.Name, mr.URL))
	return nil
}
