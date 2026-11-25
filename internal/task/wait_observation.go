package task

import (
	"context"
	"errors"
	"slices"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

// WaitObserver is the read-only capability used by the TUI's background refresh.
// An empty key with no error means there is no confirmed next action yet.
type WaitObserver interface {
	ObserveReleaseWait(ctx context.Context, id, state string) (key string, err error)
	ObserveTaskWait(ctx context.Context, selected domain.Task, state string) (key string, err error)
}

type reviewObserver interface {
	ObserveMRByNumber(ctx context.Context, number int, repo, worktreePath string) (forge.MRReadiness, error)
}

var _ WaitObserver = (*manager)(nil)

var errWaitStateChanged = errors.New("workflow wait changed")

// ObservableReleaseWait returns the state ID only for an eligible release wait.
func ObservableReleaseWait(release domain.Release, flow *gitflow.ResolvedGitFlow) string {
	if release.Status == domain.ReleaseStatusFailed || isReleaseTerminalStatus(release.Status) || IsLegacyManifest(release) {
		return ""
	}
	machine := releaseMachineOf(flow)
	return observableWait(machine, releaseStateIDOf(machine, release))
}

// ObservableTaskWait returns the state ID only for an eligible task-machine wait.
func ObservableTaskWait(flow *gitflow.ResolvedGitFlow, phase, state string) string {
	if flow == nil {
		return ""
	}
	return observableWait(flow.BranchTypes[gitflow.BranchType(phase)].Machine, state)
}

func observableWait(machine *gitflow.Machine, state string) string {
	if machine == nil {
		return ""
	}
	st, ok := machine.State(state)
	if ok && st.Kind == gitflow.StateWait && (st.Wait == gitflow.WaitReviewMerged || st.Wait == gitflow.WaitCIGreen) {
		return state
	}
	return ""
}

func (m *manager) ObserveReleaseWait(ctx context.Context, id, state string) (string, error) {
	release, err := m.GetRelease(ctx, id)
	if err != nil {
		return "", err
	}
	if state == "" || ObservableReleaseWait(release, m.flow) != state {
		return "", errWaitStateChanged
	}
	if err := m.ensureReleaseFlowUnchanged(release); err != nil {
		return "", err
	}
	st, err := m.releaseState(release)
	if err != nil {
		return "", err
	}
	switch st.Wait {
	case gitflow.WaitCIGreen:
		key := waitRecheckKey(st)
		if key == "" || len(release.Services) == 0 {
			return "", nil
		}
		holds, err := m.releaseWaitHolds(ctx, release, st)
		if holds {
			return key, nil
		}
		return "", err
	case gitflow.WaitReviewMerged:
		if allReleaseServicesMerged(release.Services) {
			return waitRecheckKey(st), nil
		}
		if !slices.ContainsFunc(st.Events, func(ev gitflow.Event) bool {
			return ev.Key == "M" && ev.Action == gitflow.EventActionMergeReviews && ev.When == ""
		}) {
			return "", nil
		}
		for _, svc := range release.Services {
			if releaseServiceMerged(svc) {
				continue
			}
			if svc.ProductionMR == nil {
				return "", nil
			}
			client, repo, path, err := m.releaseForgeDetails(ctx, svc)
			if err != nil {
				return "", err
			}
			mr, err := observeNumberedReview(ctx, client, numberedReview{svc.ProductionMR.Number, repo, path})
			if err != nil {
				return "", err
			}
			if !mergedMRState(mr.State) || len(m.releaseReviewBlockers(svc, mr)) != 0 {
				return "", nil
			}
		}
		// M must still verify the delivered commit and persist the acceptance.
		return "M", nil
	default:
		return "", errWaitStateChanged
	}
}

func (m *manager) ObserveTaskWait(ctx context.Context, selected domain.Task, state string) (string, error) {
	ctx = git.WithoutLazyFetch(ctx)
	if state == "" || len(selected.Services) == 0 || ObservableTaskWait(m.flow, selected.Phase, state) != state {
		return "", errWaitStateChanged
	}
	tf, err := m.readTaskFlow(selected.ID, selected.Services)
	if err != nil {
		return "", err
	}
	if tf == nil || tf.state.State != state || tf.state.HookStopUnconfirmed {
		return "", errWaitStateChanged
	}
	st := tf.current()
	key := waitRecheckKey(st)
	if key == "" {
		return "", nil
	}
	for _, svc := range selected.Services {
		branch, err := m.git.GetWorktreeBranch(ctx, svc.WorktreePath)
		if err != nil {
			return "", err
		}
		if branch != svc.Branch {
			return "", errWaitStateChanged
		}
	}
	switch st.Wait {
	case gitflow.WaitCIGreen:
		holds, err := m.taskWaitHolds(ctx, tf, st)
		if holds {
			return key, nil
		}
		return "", err
	case gitflow.WaitReviewMerged:
		plans, _, _, err := m.workflowPlans(tf.services)
		if err != nil {
			return "", err
		}
		manualKey := key
		for _, plan := range plans {
			// Task review waits are only configured for review strategies.
			if plan.rule.CloseStrategy != gitflow.CloseStrategyReviewRequest || len(plan.rule.ReviewTargets) == 0 {
				return "", nil
			}
			target := plan.rule.ReviewTargets[0]
			tip, err := m.git.RemoteRefSHA(ctx, plan.svc.RepoPath, "refs/heads/"+target)
			if err != nil {
				return "", err
			}
			if tip == "" {
				return "", nil
			}
			// An uncached target cannot be checked for ancestry without fetching.
			merged, ancestryErr := m.git.IsAncestor(ctx, plan.svc.RepoPath, plan.svc.Branch, tip)
			if merged && ancestryErr == nil {
				continue
			}
			event := taskEventFor(st, gitflow.EventActionMergeReviews)
			if event == nil || event.When != "" {
				return "", ancestryErr
			}
			confirmed, err := m.observeTaskReview(ctx, plan.svc, remoteReviewTarget{branch: target, sha: tip})
			if err != nil || !confirmed {
				return "", err
			}
			manualKey = "M"
		}
		return manualKey, nil
	default:
		return "", errWaitStateChanged
	}
}

type remoteReviewTarget struct{ branch, sha string }

func (m *manager) observeTaskReview(ctx context.Context, svc domain.Service, target remoteReviewTarget) (bool, error) {
	client, repo, err := m.serviceForgeTarget(ctx, svc)
	if err != nil {
		return false, err
	}
	history, ok := client.(forge.HistoryClient)
	if !ok {
		return false, nil
	}
	rows, err := history.MRHistory(ctx, svc.Branch, repo)
	if err != nil {
		return false, err
	}
	active, _ := matchTargetMRs(rows, svc.Branch, target.branch)
	if len(active) != 1 || active[0].Number == 0 || !mergedMRState(active[0].State) {
		return false, nil
	}
	mr, err := observeNumberedReview(ctx, client, numberedReview{active[0].Number, repo, svc.WorktreePath})
	if err != nil {
		return false, err
	}
	head, err := m.git.ResolveRef(ctx, svc.RepoPath, svc.Branch)
	if err != nil {
		return false, err
	}
	// ponytail: when ancestry isn't available locally, only recognize a review
	// whose resulting commit is the live target tip; M owns deeper verification.
	return head != "" && mr.Number == active[0].Number && mr.MergedSHA == target.sha &&
		validateMergedReviewMRIdentity(mr, svc, target.branch, head) == nil, nil
}

type numberedReview struct {
	number     int
	repo, path string
}

func observeNumberedReview(ctx context.Context, client forge.ForgeClient, review numberedReview) (forge.MRReadiness, error) {
	if reader, ok := client.(reviewObserver); ok {
		return reader.ObserveMRByNumber(ctx, review.number, review.repo, review.path)
	}
	return client.MRReadinessByNumber(ctx, review.number, review.repo, review.path)
}

func waitRecheckKey(st *gitflow.State) string {
	for _, ev := range st.Events {
		if ev.Key == "F" && (ev.To != st.Next || ev.When != "" || ev.Action != "") {
			return ""
		}
	}
	return "F"
}
