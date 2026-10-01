package task

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

type ReleaseMergeInspection struct {
	ReleaseID string
	Services  []ReleaseServiceMergeInspection
}

type ReleaseServiceMergeInspection struct {
	ServiceName string
	Status      string
	Blockers    []string
	MR          forge.MRReadiness
}

type ReleaseMergeResult struct {
	ReleaseID string
	Merged    []string
	Skipped   []string
	Failed    []string
}

func (m *manager) InspectReleaseMerge(ctx context.Context, releaseID string) (ReleaseMergeInspection, error) {
	inspection, _, err := m.inspectReleaseMerge(ctx, releaseID)
	return inspection, err
}

func (m *manager) inspectReleaseMerge(ctx context.Context, releaseID string) (ReleaseMergeInspection, domain.Release, error) {
	release, err := m.GetRelease(ctx, releaseID)
	if err != nil {
		return ReleaseMergeInspection{}, domain.Release{}, err
	}
	if release.Status != domain.ReleaseStatusAwaitingMasterMerge {
		return ReleaseMergeInspection{}, release, fmt.Errorf("%w: %s -> %s", ErrReleaseInvalidStatusTransition, release.Status, domain.ReleaseStatusMasterMerged)
	}

	inspection := ReleaseMergeInspection{ReleaseID: releaseID, Services: make([]ReleaseServiceMergeInspection, len(release.Services))}
	sem := make(chan struct{}, m.concurrency())
	var wg sync.WaitGroup
	for i, svc := range release.Services {
		i, svc := i, svc
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			item := ReleaseServiceMergeInspection{ServiceName: svc.Name}
			if svc.ProductionMR == nil {
				item.Status = "failed"
				item.Blockers = []string{"production MR is not recorded"}
				inspection.Services[i] = item
				return
			}

			client, repo, worktreePath, detailErr := m.releaseForgeDetails(ctx, svc)
			if detailErr != nil {
				item.Status = "failed"
				item.Blockers = []string{detailErr.Error()}
				inspection.Services[i] = item
				return
			}

			item.MR, detailErr = client.MRReadinessByNumber(ctx, svc.ProductionMR.Number, repo, worktreePath)
			item.Blockers = append([]string(nil), item.MR.Blockers...)
			identity := releaseProductionMRBlockers(svc, m.productionBranch(), item.MR)
			item.Blockers = append(item.Blockers, identity...)
			switch {
			case detailErr != nil:
				item.Status = "failed"
				item.Blockers = []string{detailErr.Error()}
			case len(identity) > 0:
				item.Status = "blocked"
			case strings.EqualFold(item.MR.State, "merged"):
				item.Status = "merged"
			case item.MR.Ready:
				item.Status = "ready"
			default:
				item.Status = "blocked"
			}
			inspection.Services[i] = item
		}()
	}
	wg.Wait()

	return inspection, release, nil
}

func (m *manager) MergeReleaseMRs(ctx context.Context, releaseID string, statusCh chan<- string) (domain.Release, ReleaseMergeResult, error) {
	inspection, release, err := m.inspectReleaseMerge(ctx, releaseID)
	if err != nil {
		return release, ReleaseMergeResult{ReleaseID: releaseID}, err
	}

	result := ReleaseMergeResult{ReleaseID: releaseID}
	for i, item := range inspection.Services {
		svc := &release.Services[i]
		// A merged MR whose head no longer matches the recorded source SHA is
		// reconciled, not skipped, so the mismatch fails closed as a retryable
		// service failure instead of stalling the release silently.
		reconcileMerged := item.Status == "merged" ||
			(item.Status == "blocked" && strings.EqualFold(strings.TrimSpace(item.MR.State), "merged") &&
				len(releaseProductionMRHeadBlockers(*svc, item.MR)) > 0)
		switch {
		case reconcileMerged:
			var serviceErr error
			mergeSHA := strings.TrimSpace(svc.AcceptedMergeSHA)
			if mergeSHA == "" {
				mergeSHA, serviceErr = m.recoverMissingReleaseMergeSHA(ctx, svc)
			}
			var authoritative string
			if serviceErr == nil {
				authoritative, serviceErr = m.verifyMergedReleaseService(ctx, svc, mergeSHA)
			}
			if serviceErr == nil {
				serviceErr = m.persistMergedReleaseService(&release, svc, authoritative)
			}
			if serviceErr != nil {
				if persistErr := m.markReleaseMergeServiceFailed(&release, svc, serviceErr); persistErr != nil {
					return release, result, persistErr
				}
				result.Failed = append(result.Failed, svc.Name)
				sendStatus(statusCh, fmt.Sprintf("[%s][merge] failed: %v", svc.Name, serviceErr))
				continue
			}
			result.Skipped = append(result.Skipped, svc.Name)
			sendStatus(statusCh, fmt.Sprintf("[%s][merge] production MR already merged", svc.Name))
		case item.Status == "ready":
			if err := m.mergeReleaseServiceMR(ctx, &release, svc, item, statusCh); err != nil {
				if persistErr := m.markReleaseMergeServiceFailed(&release, svc, err); persistErr != nil {
					return release, result, persistErr
				}
				result.Failed = append(result.Failed, svc.Name)
				sendStatus(statusCh, fmt.Sprintf("[%s][merge] failed: %v", svc.Name, err))
				continue
			}
			result.Merged = append(result.Merged, svc.Name)
		default:
			if item.Status == "failed" {
				result.Failed = append(result.Failed, svc.Name)
			} else {
				result.Skipped = append(result.Skipped, svc.Name)
			}
			sendStatus(statusCh, fmt.Sprintf("[%s][merge] %s: %s", svc.Name, item.Status, strings.Join(item.Blockers, "; ")))
		}
	}

	if allReleaseServicesMerged(release.Services) {
		if err := m.moveReleaseStatus(&release, domain.ReleaseStatusMasterMerged, "master_merged", nil); err != nil {
			return release, result, err
		}
		release, err = m.writeReleaseManifest(release)
		if err != nil {
			return release, result, err
		}
		sendStatus(statusCh, "[release][merge] all production MRs merged")
	}

	return release, result, nil
}

func (m *manager) markReleaseMergeServiceFailed(release *domain.Release, svc *domain.ReleaseService, err error) error {
	svc.Status = domain.ReleaseStatusFailed
	svc.AcceptedMergeSHA = ""
	svc.Error = &domain.ReleaseError{
		Code:        "ERR_RELEASE_MERGE_FAILED",
		Message:     "production MR merge failed",
		Stage:       "production_mr_merge",
		ServiceName: svc.Name,
		Recoverable: true,
		Cause:       err.Error(),
	}
	return m.persistCheckpoint(release, "production_mr_merge", nil)
}

func (m *manager) mergeReleaseServiceMR(ctx context.Context, release *domain.Release, svc *domain.ReleaseService, item ReleaseServiceMergeInspection, statusCh chan<- string) error {
	client, repo, worktreePath, err := m.releaseForgeDetails(ctx, *svc)
	if err != nil {
		return err
	}
	fresh, err := client.MRReadinessByNumber(ctx, svc.ProductionMR.Number, repo, worktreePath)
	if err != nil {
		return fmt.Errorf("recheck production MR !%d: %w", svc.ProductionMR.Number, err)
	}
	if err := validateReleaseMRForMerge(*svc, m.productionBranch(), fresh); err != nil {
		return err
	}
	if err := m.git.Fetch(ctx, svc.RepoPath); err != nil {
		return fmt.Errorf("release merge: fetch service %s: %w", svc.Name, err)
	}
	targetSHA, err := m.resolveReleaseRefSHA(ctx, svc.RepoPath, "origin/"+fresh.TargetBranch)
	if err != nil {
		return fmt.Errorf("release merge: resolve production target for MR %d: %w", svc.ProductionMR.Number, err)
	}
	params := forge.MergeMRParams{
		WorktreePath:         worktreePath,
		Repo:                 repo,
		Number:               svc.ProductionMR.Number,
		Method:               m.releaseMergeMethod(),
		ExpectedHeadSHA:      svc.ProductionMR.SourceSHA,
		ExpectedTargetBranch: fresh.TargetBranch,
		ExpectedTargetSHA:    targetSHA,
	}

	sendStatus(statusCh, fmt.Sprintf("[%s][merge] merging production MR %d", svc.Name, params.Number))
	merged, err := client.MergeMR(ctx, params)
	if err != nil {
		return err
	}
	if !merged.Merged {
		return errors.New("forge did not report merge success")
	}
	mergeSHA := strings.TrimSpace(merged.MergeCommitSHA)
	if mergeSHA == "" {
		mergeSHA, err = m.recoverMissingReleaseMergeSHAWithClient(ctx, svc, client, repo, worktreePath)
		if err != nil {
			return err
		}
	}
	authoritative, err := m.verifyAcceptedMergeSHA(ctx, svc, client, repo, worktreePath, mergeSHA)
	if err != nil {
		return err
	}
	if err := m.persistMergedReleaseService(release, svc, authoritative); err != nil {
		return err
	}
	sendStatus(statusCh, fmt.Sprintf("[%s][merge] production MR merged", svc.Name))
	return nil
}

func (m *manager) productionBranch() string {
	if m.flow == nil {
		return ""
	}
	return m.flow.ProductionBranch
}

// releaseProductionMRHeadBlockers validates the exact nonempty merged-head
// identity: the forge head must equal the persisted source SHA in every MR
// state. The squash/rebase merge result may differ from the recorded source
// head, so only the head is pinned here, never MergedSHA.
func releaseProductionMRHeadBlockers(svc domain.ReleaseService, mr forge.MRReadiness) []string {
	if svc.ProductionMR == nil {
		return nil
	}
	if strings.TrimSpace(mr.HeadSHA) == "" {
		return []string{"production MR head SHA missing"}
	}
	if mr.HeadSHA != svc.ProductionMR.SourceSHA {
		return []string{fmt.Sprintf("production MR head drifted: recorded %s current %s", svc.ProductionMR.SourceSHA, mr.HeadSHA)}
	}
	return nil
}

// releaseProductionMRBlockers validates the exact recorded production MR
// identity against numbered forge detail: number, source and target branches,
// and the exact nonempty head, which must equal the persisted source SHA in
// every state (open or merged) because squash/rebase merge results may differ
// from the recorded source head. Shared by merge inspection, the pre-merge
// gate, post-merge acceptance, and merge-SHA recovery so a retargeted or
// drifted MR can never authorize a production merge or its acceptance.
func releaseProductionMRBlockers(svc domain.ReleaseService, productionBranch string, mr forge.MRReadiness) []string {
	if svc.ProductionMR == nil {
		return []string{"production MR is not recorded"}
	}
	var blockers []string
	if svc.ProductionMR.Number == 0 {
		blockers = append(blockers, "production MR number missing")
	}
	if strings.TrimSpace(svc.ProductionMR.SourceSHA) == "" {
		blockers = append(blockers, "production MR source SHA missing")
	}
	blockers = append(blockers, releaseProductionMRHeadBlockers(svc, mr)...)
	if mr.Number != svc.ProductionMR.Number {
		blockers = append(blockers, fmt.Sprintf("production MR number changed: got !%d, want !%d", mr.Number, svc.ProductionMR.Number))
	}
	if mr.SourceBranch != svc.ReleaseBranch {
		blockers = append(blockers, fmt.Sprintf("production MR source is %s, want %s", mr.SourceBranch, svc.ReleaseBranch))
	}
	if productionBranch == "" {
		blockers = append(blockers, "production branch not configured")
	} else if mr.TargetBranch != productionBranch {
		blockers = append(blockers, fmt.Sprintf("production MR targets %s, want %s", mr.TargetBranch, productionBranch))
	}
	return blockers
}

// validateReleaseMRForMerge is the strict gate immediately before a production
// merge: exact recorded identity, undrifted head, open ready state, and a
// forge capable of enforcing the requested server-side head pin. Any failure
// is a blocker; production merges never run unpinned.
func validateReleaseMRForMerge(svc domain.ReleaseService, productionBranch string, mr forge.MRReadiness) error {
	if blockers := releaseProductionMRBlockers(svc, productionBranch, mr); len(blockers) > 0 {
		return errors.New(strings.Join(blockers, "; "))
	}
	state := strings.ToLower(strings.TrimSpace(mr.State))
	if state != "open" && state != "opened" {
		return fmt.Errorf("production MR !%d state is %q, want open", mr.Number, mr.State)
	}
	if len(mr.Blockers) > 0 {
		return errors.New(strings.Join(mr.Blockers, "; "))
	}
	if !mr.SupportsSHAPin {
		return fmt.Errorf("production MR !%d requires a SHA-pinned merge, forge cannot enforce it", mr.Number)
	}
	if !mr.SupportsTargetBinding {
		return fmt.Errorf("production MR !%d requires a target-bound merge, forge cannot enforce it", mr.Number)
	}
	return nil
}

// verifyMergedReleaseService re-verifies an already-merged production MR from
// authoritative forge detail plus the local production clone before success is
// (re)persisted.
func (m *manager) verifyMergedReleaseService(ctx context.Context, svc *domain.ReleaseService, mergeSHA string) (string, error) {
	client, repo, worktreePath, err := m.releaseForgeDetails(ctx, *svc)
	if err != nil {
		return "", err
	}
	return m.verifyAcceptedMergeSHA(ctx, svc, client, repo, worktreePath, mergeSHA)
}

// verifyAcceptedMergeSHA is the single acceptance gate for a production merge
// result: re-read the exact MR identity, reconcile the candidate SHA with the
// authoritative merged SHA, ensure the object exists, and prove containment in
// the fresh configured production tip. Production may have advanced beyond the
// merge commit; ancestry, not equality, is the proof.
func (m *manager) verifyAcceptedMergeSHA(ctx context.Context, svc *domain.ReleaseService, client forge.ForgeClient, repo, worktreePath, mergeSHA string) (string, error) {
	fresh, err := client.MRReadinessByNumber(ctx, svc.ProductionMR.Number, repo, worktreePath)
	if err != nil {
		return "", fmt.Errorf("release merge: verify merged MR %d: %w", svc.ProductionMR.Number, err)
	}
	if blockers := releaseProductionMRBlockers(*svc, m.productionBranch(), fresh); len(blockers) > 0 {
		return "", fmt.Errorf("release merge: merged MR %d identity rejected: %s", svc.ProductionMR.Number, strings.Join(blockers, "; "))
	}
	if !strings.EqualFold(strings.TrimSpace(fresh.State), "merged") {
		return "", fmt.Errorf("release merge: MR %d state is %q, want merged", svc.ProductionMR.Number, fresh.State)
	}
	authoritative := strings.TrimSpace(fresh.MergedSHA)
	if authoritative == "" {
		authoritative, err = m.recoverMissingReleaseMergeSHAWithClient(ctx, svc, client, repo, worktreePath)
		if err != nil {
			return "", err
		}
	}
	if mergeSHA != authoritative {
		return "", fmt.Errorf("release merge: MR %d returned SHA %s does not match authoritative %s", svc.ProductionMR.Number, mergeSHA, authoritative)
	}
	if err := m.git.EnsureCommit(ctx, svc.RepoPath, authoritative); err != nil {
		return "", fmt.Errorf("release merge: merged object %s unavailable: %w", authoritative, err)
	}
	if err := m.git.Fetch(ctx, svc.RepoPath); err != nil {
		return "", fmt.Errorf("release merge: fetch for production verification: %w", err)
	}
	production := m.productionBranch()
	tip, err := m.resolveReleaseRefSHA(ctx, svc.RepoPath, "origin/"+production)
	if err != nil {
		return "", err
	}
	contained, err := m.git.IsAncestor(ctx, svc.RepoPath, authoritative, tip)
	if err != nil {
		return "", err
	}
	if !contained {
		return "", fmt.Errorf("release merge: merged SHA %s is not contained in %s", authoritative, production)
	}
	return authoritative, nil
}

func (m *manager) recoverMissingReleaseMergeSHA(ctx context.Context, svc *domain.ReleaseService) (string, error) {
	client, repo, worktreePath, err := m.releaseForgeDetails(ctx, *svc)
	if err != nil {
		return "", err
	}
	return m.recoverMissingReleaseMergeSHAWithClient(ctx, svc, client, repo, worktreePath)
}

func (m *manager) recoverMissingReleaseMergeSHAWithClient(ctx context.Context, svc *domain.ReleaseService, client forge.ForgeClient, repo, worktreePath string) (string, error) {
	if err := m.git.Fetch(ctx, svc.RepoPath); err != nil {
		return "", fmt.Errorf("release merge: fetch service %s: %w", svc.Name, err)
	}
	fresh, err := client.MRReadinessByNumber(ctx, svc.ProductionMR.Number, repo, worktreePath)
	if err != nil {
		return "", fmt.Errorf("release merge: inspect merged MR %d: %w", svc.ProductionMR.Number, err)
	}
	if !strings.EqualFold(fresh.State, "merged") {
		return "", fmt.Errorf("release merge: merge commit SHA unavailable for MR %d (state=%s)", svc.ProductionMR.Number, fresh.State)
	}
	if blockers := releaseProductionMRBlockers(*svc, m.productionBranch(), fresh); len(blockers) > 0 {
		return "", fmt.Errorf("release merge: merged MR %d identity rejected: %s", svc.ProductionMR.Number, strings.Join(blockers, "; "))
	}
	if strings.TrimSpace(fresh.MergedSHA) != "" {
		return fresh.MergedSHA, nil
	}
	if strings.TrimSpace(fresh.HeadSHA) != "" {
		targetBranch := fresh.TargetBranch
		if targetBranch == "" && m.flow != nil {
			targetBranch = m.flow.ProductionBranch
		}
		if targetBranch != "" {
			targetSHA, resolveErr := m.resolveReleaseRefSHA(ctx, svc.RepoPath, "origin/"+targetBranch)
			if resolveErr != nil {
				return "", fmt.Errorf("release merge: resolve target branch for MR %d: %w", svc.ProductionMR.Number, resolveErr)
			}
			if targetSHA == fresh.HeadSHA {
				return fresh.HeadSHA, nil
			}
		}
	}
	return "", fmt.Errorf("release merge: merge commit SHA unavailable for MR %d (state=%s)", svc.ProductionMR.Number, fresh.State)
}

func (m *manager) persistMergedReleaseService(release *domain.Release, svc *domain.ReleaseService, mergeSHA string) error {
	if strings.TrimSpace(mergeSHA) == "" {
		return errors.New("release merge: merge commit SHA is unavailable")
	}

	svc.ProductionMR.State = "merged"
	svc.AcceptedMergeSHA = mergeSHA
	svc.Status = domain.ReleaseStatusMasterMerged
	svc.Error = nil
	return m.persistCheckpoint(release, "production_mr_merge", nil)
}

func (m *manager) releaseMergeMethod() string {
	if m.flow == nil {
		return ""
	}
	rule, ok := m.flow.BranchTypes[gitflow.BranchTypeRelease]
	if !ok {
		return ""
	}
	return forgeMergeMethod(rule.MergeStrategy)
}

func (m *manager) releaseForgeDetails(ctx context.Context, svc domain.ReleaseService) (forge.ForgeClient, string, string, error) {
	worktreePath := svc.ReleaseWorktreePath
	if worktreePath == "" {
		worktreePath = svc.RepoPath
	}
	client, err := m.forgeClientForService(ctx, domain.Service{Name: svc.Name, WorktreePath: worktreePath})
	if err != nil {
		return nil, "", "", err
	}
	remoteURL, err := m.git.RemoteURL(ctx, svc.RepoPath, "origin")
	if err != nil {
		return nil, "", "", fmt.Errorf("release merge: resolve remote for service %s: %w", svc.Name, err)
	}
	repo := forge.ExtractRepoPath(remoteURL)
	if repo == "" {
		return nil, "", "", fmt.Errorf("release merge: resolve repository path for %s: remote URL %q is not parseable", svc.Name, remoteURL)
	}
	if blockers := productionMRRepositoryBlockers(svc, remoteURL); len(blockers) > 0 {
		return nil, "", "", errors.New(strings.Join(blockers, "; "))
	}
	return client, repo, worktreePath, nil
}

// productionMRRepositoryBlockers binds the persisted production MR proof to
// the repository identity it was recorded against: the canonical repo path
// must equal the service's current origin, and the provider host must match
// when it was recorded. A legacy record without identity proves nothing and
// fails closed, so an origin retarget to a repository that happens to serve
// an MR with the same number and SHAs can never authorize inspection, merge,
// or reconciliation.
func productionMRRepositoryBlockers(svc domain.ReleaseService, remoteURL string) []string {
	if svc.ProductionMR == nil {
		return nil
	}
	if strings.TrimSpace(svc.ProductionMR.Repo) == "" {
		return []string{"production MR repository identity missing (legacy record); reject the release and prepare it again"}
	}
	repo := forge.ExtractRepoPath(remoteURL)
	if repo == "" {
		return []string{fmt.Sprintf("resolve repository path for service %s: remote URL %q is not parseable", svc.Name, remoteURL)}
	}
	if !strings.EqualFold(repo, svc.ProductionMR.Repo) {
		return []string{fmt.Sprintf("production MR repository %s does not match current origin repository %s", svc.ProductionMR.Repo, repo)}
	}
	if host := strings.TrimSpace(svc.ProductionMR.ProviderHost); host != "" && !strings.EqualFold(host, forge.RemoteHost(remoteURL)) {
		return []string{fmt.Sprintf("production MR provider host %s does not match current origin host %s", host, forge.RemoteHost(remoteURL))}
	}
	return nil
}

func allReleaseServicesMerged(services []domain.ReleaseService) bool {
	for _, svc := range services {
		if svc.ProductionMR == nil || !strings.EqualFold(svc.ProductionMR.State, "merged") || strings.TrimSpace(svc.AcceptedMergeSHA) == "" {
			return false
		}
	}
	return len(services) > 0
}
