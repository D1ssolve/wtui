package task

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
			switch {
			case detailErr != nil:
				item.Status = "failed"
				item.Blockers = []string{detailErr.Error()}
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
		switch item.Status {
		case "merged":
			var serviceErr error
			mergeSHA := strings.TrimSpace(svc.AcceptedMergeSHA)
			if mergeSHA == "" {
				mergeSHA, serviceErr = m.recoverMissingReleaseMergeSHA(ctx, svc)
			}
			if serviceErr == nil {
				serviceErr = m.persistMergedReleaseService(&release, svc, mergeSHA)
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
		case "ready":
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
	params := forge.MergeMRParams{WorktreePath: worktreePath, Repo: repo, Number: svc.ProductionMR.Number, Method: m.releaseMergeMethod()}
	if item.MR.SupportsSHAPin {
		params.ExpectedHeadSHA = svc.ProductionMR.SourceSHA
	} else {
		if item.MR.HeadSHA != svc.ProductionMR.SourceSHA {
			return fmt.Errorf("release merge: head SHA drift service=%s expected=%s actual=%s", svc.Name, svc.ProductionMR.SourceSHA, item.MR.HeadSHA)
		}
		if m.logger != nil {
			m.logger.WarnContext(ctx, "forge does not support SHA-pinned merge", slog.String("service", svc.Name))
		}
	}

	sendStatus(statusCh, fmt.Sprintf("[%s][merge] merging production MR %d", svc.Name, params.Number))
	merged, err := client.MergeMR(ctx, params)
	if err != nil {
		return err
	}
	if !merged.Merged {
		return errors.New("forge did not report merge success")
	}
	mergeSHA := merged.MergeCommitSHA
	if strings.TrimSpace(mergeSHA) == "" {
		mergeSHA, err = m.recoverMissingReleaseMergeSHAWithClient(ctx, svc, client, repo, worktreePath)
		if err != nil {
			return err
		}
	}
	if err := m.persistMergedReleaseService(release, svc, mergeSHA); err != nil {
		return err
	}
	sendStatus(statusCh, fmt.Sprintf("[%s][merge] production MR merged", svc.Name))
	return nil
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
	return client, repo, worktreePath, nil
}

func allReleaseServicesMerged(services []domain.ReleaseService) bool {
	for _, svc := range services {
		if svc.ProductionMR == nil || !strings.EqualFold(svc.ProductionMR.State, "merged") || strings.TrimSpace(svc.AcceptedMergeSHA) == "" {
			return false
		}
	}
	return len(services) > 0
}
