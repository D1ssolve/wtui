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
	cause       error
}

type ReleaseMergeResult struct {
	ReleaseID string
	Merged    []string
	Skipped   []string
	Failed    []string
	Errs      map[string]error // complete causes; progress updates are best-effort
}

func (m *manager) InspectReleaseMerge(ctx context.Context, releaseID string) (ReleaseMergeInspection, error) {
	release, err := m.GetRelease(ctx, releaseID)
	if err != nil {
		return ReleaseMergeInspection{}, err
	}
	if st, err := m.releaseState(release); err != nil || st.Kind != gitflow.StateWait || st.Wait != gitflow.WaitReviewMerged || release.Status == domain.ReleaseStatusFailed {
		return ReleaseMergeInspection{}, fmt.Errorf("%w: release %s is not waiting for reviews to merge", ErrReleaseInvalidStatusTransition, release.ID)
	}
	return m.inspectReleaseReviews(ctx, release), nil
}

func (m *manager) inspectReleaseReviews(ctx context.Context, release domain.Release) ReleaseMergeInspection {
	inspection := ReleaseMergeInspection{ReleaseID: release.ID, Services: make([]ReleaseServiceMergeInspection, len(release.Services))}
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
				item.cause = errors.New("production MR is not recorded")
				item.Blockers = []string{item.cause.Error()}
				inspection.Services[i] = item
				return
			}

			client, repo, worktreePath, detailErr := m.releaseForgeDetails(ctx, svc)
			if detailErr != nil {
				item.Status = "failed"
				item.cause = detailErr
				item.Blockers = []string{detailErr.Error()}
				inspection.Services[i] = item
				return
			}

			item.MR, detailErr = client.MRReadinessByNumber(ctx, svc.ProductionMR.Number, repo, worktreePath)
			if detailErr == nil {
				_, detailErr = m.reconcileDirectMR(ctx, svc.RepoPath, m.reviewMethod(svc), &item.MR)
			}
			item.Blockers = append([]string(nil), item.MR.Blockers...)
			identity := m.releaseReviewBlockers(svc, item.MR)
			item.Blockers = append(item.Blockers, identity...)
			switch {
			case detailErr != nil:
				item.Status = "failed"
				item.cause = detailErr
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

	return inspection
}

// MergeReleaseMRs fires the merge_reviews event of the current review_merged wait.
func (m *manager) MergeReleaseMRs(ctx context.Context, releaseID string, statusCh chan<- string) (domain.Release, ReleaseMergeResult, error) {
	run := &releaseRun{statusCh: statusCh, mergeResult: ReleaseMergeResult{ReleaseID: releaseID}}
	release, fired, err := m.advanceRelease(ctx, releaseID, func(ev gitflow.Event) bool { return ev.Action == gitflow.EventActionMergeReviews }, "", run)
	if fired == nil && err == nil {
		err = fmt.Errorf("%w: release %s has no merge event in its current state", ErrReleaseInvalidStatusTransition, releaseID)
	}
	return release, run.mergeResult, err
}

func (m *manager) mergeReleaseReviews(ctx context.Context, release *domain.Release, statusCh chan<- string) (ReleaseMergeResult, error) {
	inspection := m.inspectReleaseReviews(ctx, *release)
	result := ReleaseMergeResult{ReleaseID: release.ID}
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
				serviceErr = m.persistMergedReleaseService(release, svc, authoritative)
			}
			if serviceErr != nil {
				result.fail(svc.Name, serviceErr, statusCh)
				if persistErr := m.markReleaseMergeServiceFailed(release, svc, serviceErr); persistErr != nil {
					return result, persistErr
				}
				continue
			}
			result.Skipped = append(result.Skipped, svc.Name)
			sendStatus(statusCh, fmt.Sprintf("[%s][merge] review into %s already merged", svc.Name, m.releaseReviewTarget(*svc)))
		case item.Status == "ready" && waitingDependency(*release, *svc, m.releaseDependencies()) != "":
			dep := waitingDependency(*release, *svc, m.releaseDependencies())
			result.Skipped = append(result.Skipped, svc.Name)
			sendStatus(statusCh, fmt.Sprintf("[%s][merge] waiting for %s, which it depends on, to merge first", svc.Name, dep))
		case item.Status == "ready":
			if err := m.mergeReleaseServiceMR(ctx, release, svc, item, statusCh); errors.Is(err, errMergeQueued) {
				result.Skipped = append(result.Skipped, svc.Name)
				continue
			} else if err != nil {
				result.fail(svc.Name, err, statusCh)
				if persistErr := m.markReleaseMergeServiceFailed(release, svc, err); persistErr != nil {
					return result, persistErr
				}
				continue
			}
			result.Merged = append(result.Merged, svc.Name)
		case item.Status == "failed":
			result.fail(svc.Name, item.cause, statusCh)
		default:
			result.Skipped = append(result.Skipped, svc.Name)
			sendStatus(statusCh, fmt.Sprintf("[%s][merge] %s: %s", svc.Name, item.Status, strings.Join(item.Blockers, "; ")))
		}
	}

	return result, nil
}

func (r *ReleaseMergeResult) fail(service string, cause error, statusCh chan<- string) {
	r.Failed = append(r.Failed, service)
	if r.Errs == nil {
		r.Errs = make(map[string]error)
	}
	r.Errs[service] = cause
	sendStatus(statusCh, fmt.Sprintf("[%s][merge] failed", service))
}
