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

// WaitObservation separates positive evidence from the availability of a key.
type WaitObservation struct {
	Ready bool
	Key   string
}

// WaitObserver is the read-only capability used by the TUI's background refresh.
type WaitObserver interface {
	ObserveReleaseWait(ctx context.Context, id, state string) (WaitObservation, error)
	ObserveTaskWait(ctx context.Context, selected domain.Task, state string) (WaitObservation, error)
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

func (m *manager) ObserveReleaseWait(ctx context.Context, id, state string) (WaitObservation, error) {
	release, err := m.GetRelease(ctx, id)
	if err != nil {
		return WaitObservation{}, err
	}
	if state == "" || ObservableReleaseWait(release, m.flow) != state {
		return WaitObservation{}, errWaitStateChanged
	}
	if err := m.ensureReleaseFlowUnchanged(release); err != nil {
		return WaitObservation{}, err
	}
	st, err := m.releaseState(release)
	if err != nil {
		return WaitObservation{}, err
	}
	switch st.Wait {
	case gitflow.WaitCIGreen:
		if len(release.Services) == 0 {
			return WaitObservation{}, nil
		}
		holds, err := m.releaseWaitHolds(ctx, release, st)
		if holds {
			return WaitObservation{Ready: true, Key: waitRecheckKey(st, gitflow.WaitCIGreen)}, nil
		}
		return WaitObservation{}, err
	case gitflow.WaitReviewMerged:
		if allReleaseServicesMerged(release.Services) {
			return WaitObservation{Ready: true, Key: waitRecheckKey(st, "")}, nil
		}
		for _, svc := range release.Services {
			if releaseServiceMerged(svc) {
				continue
			}
			if svc.ProductionMR == nil {
				return WaitObservation{}, nil
			}
			client, repo, path, err := m.releaseForgeDetails(ctx, svc)
			if err != nil {
				return WaitObservation{}, err
			}
			mr, err := observeNumberedReview(ctx, client, numberedReview{svc.ProductionMR.Number, repo, path})
			if err != nil {
				return WaitObservation{}, err
			}
			if !mergedMRState(mr.State) || len(m.releaseReviewBlockers(svc, mr)) != 0 {
				return WaitObservation{}, nil
			}
		}
		observation := WaitObservation{Ready: true}
		if slices.ContainsFunc(st.Events, func(ev gitflow.Event) bool {
			return ev.Key == "M" && ev.Action == gitflow.EventActionMergeReviews && ev.When == ""
		}) {
			// M must still verify the delivered commit and persist the acceptance.
			observation.Key = "M"
		}
		return observation, nil
	default:
		return WaitObservation{}, errWaitStateChanged
	}
}

func (m *manager) ObserveTaskWait(ctx context.Context, selected domain.Task, state string) (WaitObservation, error) {
	ctx = git.WithoutLazyFetch(ctx)
	if state == "" || len(selected.Services) == 0 || ObservableTaskWait(m.flow, selected.Phase, state) != state {
		return WaitObservation{}, errWaitStateChanged
	}
	tf, err := m.readTaskFlow(selected.ID, selected.Services)
	if err != nil {
		return WaitObservation{}, err
	}
	if tf == nil || tf.state.State != state || tf.state.HookStopUnconfirmed {
		return WaitObservation{}, errWaitStateChanged
	}
	st := tf.current()
	for _, svc := range selected.Services {
		branch, err := m.git.GetWorktreeBranch(ctx, svc.WorktreePath)
		if err != nil {
			return WaitObservation{}, err
		}
		if branch != svc.Branch {
			return WaitObservation{}, errWaitStateChanged
		}
	}
	switch st.Wait {
	case gitflow.WaitCIGreen:
		holds, err := m.taskWaitHolds(ctx, tf, st)
		if holds {
			guard := ""
			if st.WaitOn == gitflow.TaskBranchRef {
				guard = gitflow.WaitCIGreen
			}
			return WaitObservation{Ready: true, Key: waitRecheckKey(st, guard)}, nil
		}
		return WaitObservation{}, err
	case gitflow.WaitReviewMerged:
		holds, err := m.taskReviewWaitHolds(ctx, tf)
		if holds {
			return WaitObservation{Ready: true, Key: waitRecheckKey(st, "")}, nil
		}
		return WaitObservation{}, err
	default:
		return WaitObservation{}, errWaitStateChanged
	}
}

type taskReviewRefs struct{ sourceSHA, targetBranch, targetSHA string }

// Both observation and manual re-check prove delivery against exact live refs.
func (m *manager) taskReviewWaitHolds(ctx context.Context, tf *taskFlow) (bool, error) {
	plans, _, _, err := m.workflowPlans(tf.services)
	if err != nil {
		return false, err
	}
	pins, err := m.loadTaskTrainPins(tf.taskID)
	if err != nil {
		return false, err
	}
	for _, plan := range plans {
		if plan.rule.CloseStrategy != gitflow.CloseStrategyReviewRequest || len(plan.rule.ReviewTargets) == 0 {
			return false, nil
		}
		source := "refs/heads/" + plan.svc.Branch
		local, err := m.git.ResolveRef(ctx, plan.svc.RepoPath, source)
		if err != nil {
			return false, err
		}
		published, err := m.git.RemoteRefSHA(ctx, plan.svc.RepoPath, source)
		if err != nil || published == "" || published != local {
			return false, err
		}
		if recorded := takeTaskTrainPins(pins, plan.svc.Name); len(recorded) > 0 {
			if len(recorded) != len(m.taskTrainTargets(plan.svc)) {
				return false, nil
			}
			for _, pin := range recorded {
				if pin.Head != published {
					return false, nil
				}
				if ok, err := m.pinnedTrainAtLiveTarget(ctx, plan.svc, pin); err != nil || !ok {
					return false, err
				}
			}
			continue
		}
		for _, target := range plan.rule.ReviewTargets {
			tip, err := m.git.RemoteRefSHA(ctx, plan.svc.RepoPath, "refs/heads/"+target)
			if err != nil || tip == "" {
				return false, err
			}
			// An uncached target or a squash may not prove source ancestry.
			merged, ancestryErr := m.git.IsAncestor(ctx, plan.svc.RepoPath, published, tip)
			if merged && ancestryErr == nil {
				continue
			}
			refs := taskReviewRefs{sourceSHA: published, targetBranch: target, targetSHA: tip}
			confirmed, err := m.taskReviewAtTarget(ctx, plan.svc, refs)
			if err != nil || !confirmed {
				return false, err
			}
		}
	}
	// Leftover pins belong to a service or target no longer in this flow.
	err = ctx.Err()
	return len(plans) > 0 && len(pins) == 0 && err == nil, err
}

func (m *manager) taskReviewAtTarget(ctx context.Context, svc domain.Service, refs taskReviewRefs) (bool, error) {
	client, repo, err := m.serviceForgeTarget(ctx, svc)
	if errors.Is(err, forge.ErrForgeUnavailable) {
		return false, nil
	}
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
	active, _ := matchTargetMRs(rows, svc.Branch, refs.targetBranch)
	if len(active) != 1 || active[0].Number == 0 || !mergedMRState(active[0].State) {
		return false, nil
	}
	mr, err := observeNumberedReview(ctx, client, numberedReview{active[0].Number, repo, svc.WorktreePath})
	if err != nil {
		return false, err
	}
	// Without source ancestry, only accept a review whose exact merged SHA
	// is the live target tip. No local fetch is required to establish equality.
	return mr.Number == active[0].Number && mr.MergedSHA == refs.targetSHA &&
		validateMergedReviewMRIdentity(mr, svc, refs.targetBranch, refs.sourceSHA) == nil, nil
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

func waitRecheckKey(st *gitflow.State, satisfiedGuard string) string {
	for _, ev := range st.Events {
		// A named F takes its own transition; only a matching guard rechecks the proof.
		if ev.Key == "F" && (ev.To != st.Next || ev.When == "" || ev.When != satisfiedGuard || ev.Action != "") {
			return ""
		}
	}
	return "F"
}
