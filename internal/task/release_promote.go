package task

import (
	"context"
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

// PromoteRelease fires the release's F event; in git-flow it opens the production reviews.
func (m *manager) PromoteRelease(ctx context.Context, releaseID string, statusCh chan<- string) (domain.Release, error) {
	return m.advanceByKey(ctx, releaseID, "F", &releaseRun{statusCh: statusCh})
}

func (m *manager) promoteReleaseService(ctx context.Context, release *domain.Release, svc *domain.ReleaseService, source, target, method string, statusCh chan<- string) error {

	remoteURL, err := m.git.RemoteURL(ctx, svc.RepoPath, m.cfg.ServiceRemote().String())
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

	sourceRef := source
	if source != svc.ReleaseBranch {
		if err := m.git.Fetch(ctx, svc.RepoPath); err != nil {
			return fmt.Errorf("release review: fetch service %s: %w", svc.Name, err)
		}
		sourceRef = m.cfg.ServiceRemote().Tracking(source)
	}
	sourceSHA, err := m.resolveReleaseRefSHA(ctx, svc.RepoPath, sourceRef)
	if err != nil {
		return err
	}
	worktreePath := svc.ReleaseWorktreePath
	if worktreePath == "" {
		worktreePath = svc.RepoPath
	}
	mrs, err := client.MRStatus(ctx, source, repo)
	if err != nil {
		return fmt.Errorf("release promote: inspect production MR for service %s: %w", svc.Name, err)
	}
	for _, mr := range mrs {
		open := strings.EqualFold(mr.State, "open") || strings.EqualFold(mr.State, "opened")
		wrongTarget := mr.TargetBranch != "" && !strings.EqualFold(mr.TargetBranch, target)
		if !open || wrongTarget || mr.Number == 0 {
			continue
		}
		svc.ProductionMR = newReviewRef(mr.Number, mr.URL, sourceSHA, remoteURL, source, target, method, svc.ReleaseBranch, m.productionBranch())
		svc.Status = domain.ReleaseStatusAwaitingMasterMerge
		if err := m.persistCheckpoint(release, "production_mr", nil); err != nil {
			return err
		}
		sendStatus(statusCh, fmt.Sprintf("[%s][review] %s -> %s already open: %s", svc.Name, source, target, mr.URL))
		return nil
	}
	sendStatus(statusCh, fmt.Sprintf("[%s][review] creating %s -> %s MR", svc.Name, source, target))
	title, description := release.Title, release.Description
	if strings.TrimSpace(title) == "" {
		title = fmt.Sprintf("Release %s (%s)", svc.Version, svc.Name)
	}
	if strings.TrimSpace(description) == "" {
		description = fmt.Sprintf("Release %s", release.ID)
	}
	var policy gitflow.MRPolicy
	if m.flow != nil && m.flow.Release != nil {
		policy = m.flow.Release.MR
	}
	mr, err := client.CreateMR(ctx, withMRPolicy(forge.CreateMRParams{
		WorktreePath: worktreePath,
		SourceBranch: source,
		TargetBranch: target,
		Title:        title,
		Description:  description,
		Repo:         repo,
	}, policy))
	if err != nil {
		return fmt.Errorf("release review: create %s -> %s MR for service %s: %w", source, target, svc.Name, err)
	}

	svc.ProductionMR = newReviewRef(mr.Number, mr.URL, sourceSHA, remoteURL, source, target, method, svc.ReleaseBranch, m.productionBranch())
	svc.Status = domain.ReleaseStatusAwaitingMasterMerge
	if err := m.persistCheckpoint(release, "production_mr", nil); err != nil {
		return err
	}
	sendStatus(statusCh, fmt.Sprintf("[%s][review] %s -> %s open: %s", svc.Name, source, target, mr.URL))
	return nil
}

// newProductionMRRef binds a production MR proof to the repository identity
// it was created against, so a later configured remote retarget can never reuse the MR
// number/SHA pair against a different repository.
func newProductionMRRef(number int, url, sourceSHA, remoteURL string) *domain.ProductionMRRef {
	return &domain.ProductionMRRef{
		Number:       number,
		URL:          url,
		SourceSHA:    sourceSHA,
		State:        "open",
		Repo:         forge.ExtractRepoPath(remoteURL),
		ProviderHost: forge.RemoteHost(remoteURL),
	}
}

// newReviewRef records source, target and method only where they differ from the git-flow
// release review, so manifests of that flow keep their previous shape.
func newReviewRef(number int, url, sourceSHA, remoteURL, source, target, method, releaseBranch, production string) *domain.ProductionMRRef {
	ref := newProductionMRRef(number, url, sourceSHA, remoteURL)
	if source != releaseBranch {
		ref.Source = source
	}
	if target != production {
		ref.Target = target
	}
	ref.Method = method
	return ref
}
