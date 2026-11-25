package task

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/shell"
)

type releaseRun struct {
	statusCh      chan<- string
	taskMergePlan *ReleaseTaskMergePlan
	retry         bool
	mergeResult   ReleaseMergeResult
}

// ReleaseEvent is an event the user can fire in the release's current state.
type ReleaseEvent struct {
	Name   string
	Key    string
	Label  string
	Action string
	To     string
}

type releaseServiceError struct {
	svc *domain.ReleaseService
	err error
}

func (e *releaseServiceError) Error() string { return e.err.Error() }
func (e *releaseServiceError) Unwrap() error { return e.err }

func serviceFailure(svc *domain.ReleaseService, err error) error {
	if err == nil {
		return nil
	}
	return &releaseServiceError{svc: svc, err: err}
}

func (m *manager) releaseMachine() *gitflow.Machine {
	return releaseMachineOf(m.flow)
}

func releaseMachineOf(flow *gitflow.ResolvedGitFlow) *gitflow.Machine {
	production, integration := "master", "develop"
	if flow != nil {
		production, integration = flow.ProductionBranch, flow.IntegrationBranch
	}
	if flow != nil && flow.Release != nil {
		if flow.Release.Machine != nil {
			return flow.Release.Machine
		}
		return gitflow.ReleaseMachineFromSteps(flow.Release.Steps, production)
	}
	return gitflow.DefaultReleaseMachine(production, integration)
}

// continueAfterPreparation resumes the machine after task-MR preparation completed the initial state.
func (m *manager) continueAfterPreparation(ctx context.Context, release domain.Release) (domain.Release, error) {
	mach := m.releaseMachine()
	initial, _ := mach.State(mach.Initial)
	release.State = mach.Initial
	if err := m.enterReleaseState(&release, initial.Next, ""); err != nil {
		return release, err
	}
	return m.runReleaseMachine(ctx, release, &releaseRun{})
}

// releaseStateID maps a manifest to its machine state; manifests written before
// release machines carry only a status, whose name the legacy states reuse.
func (m *manager) releaseStateID(r domain.Release) string {
	return releaseStateIDOf(m.releaseMachine(), r)
}

func releaseStateIDOf(mach *gitflow.Machine, r domain.Release) string {
	// A status naming a state wins: older binaries move only the status and keep the state field.
	if _, ok := mach.State(string(r.Status)); ok {
		return string(r.Status)
	}
	if r.State != "" {
		return r.State
	}
	stateWith := func(kind string) string {
		for _, s := range mach.States {
			for _, step := range s.Run {
				if step.Kind == kind {
					return s.ID
				}
			}
		}
		return mach.Initial
	}
	switch r.Status {
	case domain.ReleaseStatusPrepared, domain.ReleaseStatusAwaitingMasterMerge, domain.ReleaseStatusMasterMerged:
		if _, ok := mach.State(string(r.Status)); ok {
			return string(r.Status)
		}
	case domain.ReleaseStatusReleased:
		for _, s := range mach.States {
			if s.Kind == gitflow.StateFinal && s.Outcome == gitflow.OutcomeReleased {
				return s.ID
			}
		}
	case domain.ReleaseStatusSyncingDevelop, domain.ReleaseStatusTagging:
		return stateWith(gitflow.StepTag)
	case domain.ReleaseStatusFailed, domain.ReleaseStatusPushing:
		switch {
		case r.PreparedAt == nil:
			return mach.Initial
		case releaseHasAcceptedMerges(r):
			return stateWith(gitflow.StepTag)
		}
		if _, ok := mach.State(gitflow.StatePromoting); ok {
			return gitflow.StatePromoting
		}
		return stateWith(gitflow.StepTag)
	}
	return mach.Initial
}

func (m *manager) releaseState(r domain.Release) (*gitflow.State, error) {
	id := m.releaseStateID(r)
	st, ok := m.releaseMachine().State(id)
	if !ok {
		return nil, fmt.Errorf("%w: release %s is in state %q, which the configured workflow no longer has", ErrReleaseFlowChanged, r.ID, id)
	}
	return st, nil
}

// ReleaseEvents lists the events available in the release's current state.
func (m *manager) ReleaseEvents(r domain.Release) []ReleaseEvent {
	if r.Status == domain.ReleaseStatusFailed || isReleaseTerminalStatus(r.Status) {
		return nil
	}
	st, err := m.releaseState(r)
	if err != nil || st.Kind != gitflow.StateWait {
		return nil
	}
	events := make([]ReleaseEvent, 0, len(st.Events)+1)
	hasF := false
	for _, ev := range st.Events {
		events = append(events, ReleaseEvent{Name: ev.Name, Key: ev.Key, Label: ev.Label, Action: ev.Action, To: ev.To})
		hasF = hasF || ev.Key == "F"
	}
	if st.Wait != gitflow.WaitManual && !hasF {
		// An unnamed F event re-checks the wait condition.
		events = append(events, ReleaseEvent{Key: "F", Label: "re-check " + strings.ReplaceAll(st.Wait, "_", " "), To: st.Next})
	}
	return events
}

func (m *manager) enterReleaseState(r *domain.Release, to, event string) error {
	from := r.State
	if from == "" {
		from = m.releaseStateID(*r)
	}
	r.History = append(r.History, domain.ReleaseTransition{From: from, To: to, Event: event, At: defaultReleaseNow().UTC()})
	for i := range r.Services {
		// Entering a state anew runs its hooks again; a retry re-runs the state without entering it.
		r.Services[i].HooksDone = slices.DeleteFunc(r.Services[i].HooksDone, func(h string) bool { return strings.HasPrefix(h, to+"/") })
	}
	r.State = to
	r.UpdatedAt = defaultReleaseNow().UTC()
	written, err := m.writeReleaseManifest(*r)
	if err != nil {
		return err
	}
	*r = written
	return nil
}

// AdvanceRelease fires a named event in the current wait state, or re-checks the
// wait condition when event is empty, then runs the machine until it waits again.
func (m *manager) AdvanceRelease(ctx context.Context, releaseID, event string, statusCh chan<- string) (domain.Release, error) {
	release, _, err := m.advanceRelease(ctx, releaseID, func(gitflow.Event) bool { return false }, event, &releaseRun{statusCh: statusCh})
	return release, err
}

func (m *manager) advanceByKey(ctx context.Context, releaseID, key string, run *releaseRun) (domain.Release, error) {
	release, _, err := m.advanceRelease(ctx, releaseID, func(ev gitflow.Event) bool { return ev.Key == key }, "", run)
	return release, err
}

func (m *manager) advanceRelease(ctx context.Context, releaseID string, match func(gitflow.Event) bool, event string, run *releaseRun) (domain.Release, *gitflow.Event, error) {
	unlock, err := m.lockRelease(ctx, releaseID)
	if err != nil {
		return domain.Release{}, nil, err
	}
	defer unlock()
	release, err := m.GetRelease(ctx, releaseID)
	if err != nil {
		return domain.Release{}, nil, err
	}
	if IsLegacyManifest(release) {
		return release, nil, fmt.Errorf("%w: reject release %s and prepare it again", ErrReleaseLegacyManifest, release.ID)
	}
	if err := m.ensureReleaseFlowUnchanged(release); err != nil {
		return release, nil, err
	}
	st, err := m.releaseState(release)
	if err != nil {
		return release, nil, err
	}
	if release.Status == domain.ReleaseStatusFailed || isReleaseTerminalStatus(release.Status) || st.Kind != gitflow.StateWait {
		return release, nil, fmt.Errorf("%w: release %s is %s in state %s; nothing waits for input", ErrReleaseInvalidStatusTransition, release.ID, release.Status, st.ID)
	}
	release.State = st.ID

	var fired *gitflow.Event
	for i := range st.Events {
		if st.Events[i].Name == event || match(st.Events[i]) {
			fired = &st.Events[i]
			break
		}
	}
	if fired == nil && (event != "" || st.Wait == gitflow.WaitManual) {
		return release, nil, fmt.Errorf("%w: state %s of release %s has no such event", ErrReleaseInvalidStatusTransition, st.ID, release.ID)
	}

	next := ""
	if fired != nil {
		release.Error = nil
		if fired.When == gitflow.WaitCIGreen {
			branch := st.WaitOn
			if branch == "" {
				branch = gitflow.ReleaseBranchRef
			}
			green, detail, err := m.releaseCIGreen(ctx, release, branch)
			if err != nil {
				return release, fired, err
			}
			if !green {
				return release, fired, fmt.Errorf("%w: %s requires green CI: %s", ErrReleaseInvalidStatusTransition, fired.Name, detail)
			}
		}
		if fired.Action == gitflow.EventActionMergeReviews {
			result, err := m.mergeReleaseReviews(ctx, &release, run.statusCh)
			run.mergeResult = result
			if err != nil {
				return release, fired, err
			}
		}
		next = fired.To
	}
	if next == "" && st.Wait != gitflow.WaitManual {
		holds, err := m.releaseWaitHolds(ctx, release, st)
		if err != nil {
			return release, fired, err
		}
		if holds {
			next = st.Next
		}
	}
	if next == "" {
		release, err = m.writeReleaseManifest(release)
		return release, fired, err
	}
	name := event
	if fired != nil {
		name = fired.Name
	}
	if err := m.enterReleaseState(&release, next, name); err != nil {
		return release, fired, err
	}
	release, err = m.runReleaseMachine(ctx, release, run)
	return release, fired, err
}

// runReleaseMachine executes run states until the release waits, ends, or fails.
// A failed run state stays current, so RetryRelease re-runs its idempotent actions.
func (m *manager) runReleaseMachine(ctx context.Context, release domain.Release, run *releaseRun) (domain.Release, error) {
	mach := m.releaseMachine()
	for {
		st, ok := mach.State(release.State)
		if !ok {
			return release, fmt.Errorf("%w: unknown state %q", ErrReleaseFlowChanged, release.State)
		}
		switch st.Kind {
		case gitflow.StateFinal:
			status := domain.ReleaseStatusReleased
			if st.Outcome == gitflow.OutcomeRejected {
				status = domain.ReleaseStatusRejected
			}
			release.Status = status
			release.Checkpoint = "final"
			release.Error = nil
			completedAt := defaultReleaseNow().UTC()
			release.CompletedAt = &completedAt
			return m.writeReleaseManifest(release)

		case gitflow.StateWait:
			// An error that routed here through on_error stays visible until the next event.
			release.Status = domain.ReleaseStatus(st.ID)
			release.Checkpoint = st.ID
			if st.Wait != gitflow.WaitManual {
				holds, err := m.releaseWaitHolds(ctx, release, st)
				if err != nil {
					if workflowInterrupted(err) {
						// The transition was already persisted, but the wait check did not finish.
						stored, saveErr := m.writeReleaseManifest(release)
						return stored, errors.Join(err, saveErr)
					}
					sendStatus(run.statusCh, fmt.Sprintf("[release][%s] %v", st.ID, err))
				} else if holds {
					if err := m.enterReleaseState(&release, st.Next, st.Wait); err != nil {
						return release, err
					}
					continue
				}
			}
			return m.writeReleaseManifest(release)

		case gitflow.StateRun:
			err := m.runReleaseState(ctx, &release, st, run)
			run.retry = false
			if err != nil {
				if workflowInterrupted(err) {
					return m.saveReleaseInterruption(release, err)
				}
				var svcErr *releaseServiceError
				var svc *domain.ReleaseService
				if errors.As(err, &svcErr) {
					svc = svcErr.svc
				}
				releaseErr := classifyReleaseError(err, svc)
				safety := errors.Is(err, ErrReleaseMasterMoved) || errors.Is(err, ErrReleaseRetryUnsafe) || errors.Is(err, ErrReleaseFlowChanged)
				if st.OnError != "" && !safety {
					sendStatus(run.statusCh, fmt.Sprintf("[release][%s] %v; continuing with %s", st.ID, err, st.OnError))
					release.Error = releaseErr
					if err := m.enterReleaseState(&release, st.OnError, "error"); err != nil {
						return release, err
					}
					continue
				}
				if release.Status == domain.ReleaseStatusTaskMergeBlocked || release.Status == domain.ReleaseStatusTaskMergePartial {
					return release, err // waits for a task-merge retry, not a failure retry
				}
				_ = m.failRelease(&release, releaseErr)
				return release, err
			}
			if st.ID == mach.Initial && release.PreparedAt == nil {
				preparedAt := defaultReleaseNow().UTC()
				release.PreparedAt = &preparedAt
			}
			if err := m.enterReleaseState(&release, st.Next, ""); err != nil {
				return release, err
			}
		}
	}
}

func (m *manager) runReleaseState(ctx context.Context, release *domain.Release, st *gitflow.State, run *releaseRun) error {
	// Every tag of the state is verified before any action mutates a repository.
	for _, step := range st.Run {
		if step.Kind == gitflow.StepTag {
			if err := m.acceptReleaseTagTargets(ctx, release, step, run.statusCh); err != nil {
				return err
			}
		}
	}
	for i, step := range st.Run {
		var err error
		switch step.Kind {
		case gitflow.StepIntegrateTasks:
			err = m.integrateReleaseTasksAction(ctx, release, run)
		case gitflow.StepCutBranch:
			err = m.cutReleaseBranches(ctx, release, run)
		case gitflow.StepReview:
			err = m.openReleaseReviews(ctx, release, step, run.statusCh)
		case gitflow.StepBackMerge:
			err = m.backMergeRelease(ctx, release, step, run.statusCh)
		case gitflow.StepTag:
			err = m.tagRelease(ctx, release, run.statusCh)
		case gitflow.StepHook:
			err = m.runReleaseHook(ctx, release, st.ID, fmt.Sprintf("%s/%d/%s", st.ID, i, step.Name), step, run.statusCh)
		case gitflow.StepPipeline:
			err = m.triggerReleasePipelines(ctx, release, step, run.statusCh)
		default:
			err = fmt.Errorf("release action %s is not supported", step.Kind)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *manager) integrateReleaseTasksAction(ctx context.Context, release *domain.Release, run *releaseRun) error {
	if run.retry {
		return nil // merged task MRs are retried by RetryReleaseTaskMerges; a failure retry re-runs preparation only
	}
	if run.taskMergePlan == nil {
		return fmt.Errorf("%w: task MR integration needs a confirmed plan; use ctrl+r to review it", ErrReleaseRetryUnsafe)
	}
	return m.integrateReleaseTaskMRs(ctx, release, run.taskMergePlan, run.statusCh)
}

func (m *manager) cutReleaseBranches(ctx context.Context, release *domain.Release, run *releaseRun) error {
	if release.Status == domain.ReleaseStatusValidating || release.Status == domain.ReleaseStatusFailed {
		if err := m.moveReleaseStatus(release, domain.ReleaseStatusMerging, "merging", nil); err != nil {
			return err
		}
		written, err := m.writeReleaseManifest(*release)
		if err != nil {
			return err
		}
		*release = written
	}
	for i := range release.Services {
		svc := &release.Services[i]
		var err error
		switch {
		case run.retry && servicePrepareRetryCompleted(*svc):
			svc.Status = domain.ReleaseStatusPrepared
			err = m.persistCheckpoint(release, "service_prepared", nil)
		case run.retry:
			err = m.retryPrepareService(ctx, release, svc)
		default:
			err = m.executePrepareService(ctx, release, svc, run.statusCh)
		}
		if err != nil {
			return serviceFailure(svc, err)
		}
	}
	return nil
}

func (m *manager) reviewSource(svc domain.ReleaseService) string {
	if svc.ProductionMR != nil && svc.ProductionMR.Source != "" {
		return svc.ProductionMR.Source
	}
	return svc.ReleaseBranch
}

func (m *manager) releaseReviewTarget(svc domain.ReleaseService) string {
	if svc.ProductionMR != nil && svc.ProductionMR.Target != "" {
		return svc.ProductionMR.Target
	}
	return m.productionBranch()
}

func (m *manager) reviewMethod(svc domain.ReleaseService) string {
	if svc.ProductionMR != nil && svc.ProductionMR.Method != "" {
		return svc.ProductionMR.Method
	}
	return m.releaseMergeMethod()
}

func (m *manager) openReleaseReviews(ctx context.Context, release *domain.Release, step gitflow.Step, statusCh chan<- string) error {
	target := step.Into[0]
	for i := range release.Services {
		svc := &release.Services[i]
		source := step.From
		if source == gitflow.ReleaseBranchRef {
			source = svc.ReleaseBranch
		}
		if pr := svc.ProductionMR; pr != nil && m.releaseReviewTarget(*svc) == target && m.reviewSource(*svc) == source &&
			(strings.EqualFold(pr.State, "open") || strings.EqualFold(pr.State, "merged")) {
			sendStatus(statusCh, fmt.Sprintf("[%s][review] %s -> %s already %s: %s", svc.Name, source, target, pr.State, pr.URL))
			continue
		}
		if err := m.promoteReleaseService(ctx, release, svc, source, target, forgeMergeMethod(step.Merge), statusCh); err != nil {
			return serviceFailure(svc, err)
		}
	}
	return nil
}

func (m *manager) backMergeRelease(ctx context.Context, release *domain.Release, step gitflow.Step, statusCh chan<- string) error {
	if err := m.moveReleaseStatus(release, domain.ReleaseStatusSyncingDevelop, "syncing_develop", nil); err != nil {
		return err
	}
	written, err := m.writeReleaseManifest(*release)
	if err != nil {
		return err
	}
	*release = written
	for i := range release.Services {
		svc := &release.Services[i]
		from := step.From
		if from == gitflow.ReleaseBranchRef {
			from = svc.ReleaseBranch
		} else {
			from = "origin/" + from
		}
		if err := m.syncFinalizeService(ctx, release, svc, from, step.Into[0], statusCh); err != nil {
			return serviceFailure(svc, err)
		}
	}
	return nil
}

// acceptReleaseTagTargets fixes the commit every service tag will point to and
// proves it is still current before the state mutates anything.
func (m *manager) acceptReleaseTagTargets(ctx context.Context, release *domain.Release, step gitflow.Step, statusCh chan<- string) error {
	for i := range release.Services {
		svc := &release.Services[i]
		sendStatus(statusCh, fmt.Sprintf("[%s][finalize_fetch] fetching %s", svc.Name, svc.RepoPath))
		if err := m.git.Fetch(ctx, svc.RepoPath); err != nil {
			return serviceFailure(svc, fmt.Errorf("release finalize: fetch service=%s: %w", svc.Name, err))
		}
		var err error
		if step.On == gitflow.ReleaseBranchRef {
			err = m.acceptReleaseBranchTip(ctx, svc)
		} else {
			err = m.verifyAcceptedReviewMerge(ctx, svc, step.On)
		}
		if err == nil {
			err = m.validateFinalizeTag(ctx, *svc)
		}
		if err != nil {
			return serviceFailure(svc, err)
		}
	}
	return nil
}

func (m *manager) verifyAcceptedReviewMerge(ctx context.Context, svc *domain.ReleaseService, branch string) error {
	expected := svc.Accepted[branch]
	if expected == "" && m.releaseReviewTarget(*svc) == branch {
		expected = svc.AcceptedMergeSHA
	}
	if expected == "" {
		return fmt.Errorf("%w: service=%s has no accepted merge into %s", ErrReleaseRetryUnsafe, svc.Name, branch)
	}
	tip, err := m.resolveReleaseRefSHA(ctx, svc.RepoPath, "origin/"+branch)
	if err != nil {
		return err
	}
	if tip != expected {
		return fmt.Errorf("%w: service=%s branch=%s expected=%s actual=%s", ErrReleaseMasterMoved, svc.Name, branch, expected, tip)
	}
	svc.AcceptedMergeSHA = expected
	return nil
}

func (m *manager) tagRelease(ctx context.Context, release *domain.Release, statusCh chan<- string) error {
	if err := m.moveReleaseStatus(release, domain.ReleaseStatusTagging, "tagging", nil); err != nil {
		return err
	}
	written, err := m.writeReleaseManifest(*release)
	if err != nil {
		return err
	}
	*release = written
	for i := range release.Services {
		if err := m.tagFinalizeService(ctx, release, &release.Services[i], statusCh); err != nil {
			return serviceFailure(&release.Services[i], err)
		}
	}
	if err := m.moveReleaseStatus(release, domain.ReleaseStatusPushing, "pushing", nil); err != nil {
		return err
	}
	if written, err = m.writeReleaseManifest(*release); err != nil {
		return err
	}
	*release = written
	for i := range release.Services {
		svc := &release.Services[i]
		if m.cfg.Release != nil && m.cfg.Release.PushTags != nil && *m.cfg.Release.PushTags {
			sendStatus(statusCh, fmt.Sprintf("[%s][push] pushing tag %s", svc.Name, svc.Tag))
			// Prove the push destination before publishing: a tag pushed to a
			// fetch-vs-push mismatched remote ships nothing the release
			// identity covers. The captured URL is the push target, never
			// the mutable remote name.
			pushURL, err := m.verifiedRepoPushURL(ctx, svc.Name, svc.RepoPath)
			if err != nil {
				return serviceFailure(svc, fmt.Errorf("release finalize: verify push destination for service=%s: %w", svc.Name, err))
			}
			tagObjectSHA, err := m.resolveTagObjectSHA(ctx, svc.RepoPath, svc.Tag, svc.AcceptedMergeSHA)
			if err != nil {
				return serviceFailure(svc, err)
			}
			if err := m.git.PushTag(ctx, svc.RepoPath, pushURL, svc.Tag, tagObjectSHA); err != nil {
				return serviceFailure(svc, fmt.Errorf("%w: service=%s tag=%s: %v", ErrReleaseTagPushFailed, svc.Name, svc.Tag, err))
			}
			svc.PushedTag = true
		}
		svc.Status = domain.ReleaseStatusReleased
		svc.Error = nil
		if err := m.persistCheckpoint(release, "push_tag", nil); err != nil {
			return err
		}
	}
	return nil
}

const hookOutputLines = 20

// runReleaseHook runs a configured shell command once per service; a service whose
// hook already succeeded in this state is skipped on retry.
func (m *manager) runReleaseHook(ctx context.Context, release *domain.Release, stateID, done string, step gitflow.Step, statusCh chan<- string) error {
	timeout, err := time.ParseDuration(step.Timeout)
	if err != nil {
		return err
	}
	for i := range release.Services {
		svc := &release.Services[i]
		if slices.Contains(svc.HooksDone, done) {
			continue
		}
		dir := svc.ReleaseWorktreePath
		if dir == "" {
			dir = svc.RepoPath
		}
		sendStatus(statusCh, fmt.Sprintf("[%s][hook] %s: %s", svc.Name, step.Name, step.Command))
		hookCtx, cancel := context.WithTimeout(ctx, timeout)
		cmd := shell.Command(hookCtx, step.Command)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"WTUI_RELEASE_ID="+release.ID,
			"WTUI_STATE="+stateID,
			"WTUI_SERVICE="+svc.Name,
			"WTUI_REPO="+svc.RepoPath,
			"WTUI_VERSION="+svc.Version,
			"WTUI_TAG="+svc.Tag,
			"WTUI_RELEASE_BRANCH="+svc.ReleaseBranch,
		)
		out, runErr := shell.HookOutput(hookCtx, cmd)
		cancel()
		lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
		if len(lines) > hookOutputLines {
			lines = lines[len(lines)-hookOutputLines:]
		}
		for _, line := range lines {
			if line != "" {
				sendStatus(statusCh, fmt.Sprintf("[%s][hook %s] %s", svc.Name, step.Name, line))
			}
		}
		if runErr != nil {
			if errors.Is(runErr, context.DeadlineExceeded) && ctx.Err() == nil {
				runErr = fmt.Errorf("timed out after %s", timeout)
			}
			return serviceFailure(svc, fmt.Errorf("hook %s failed for %s: %w", step.Name, svc.Name, runErr))
		}
		svc.HooksDone = append(svc.HooksDone, done)
		if err := m.persistCheckpoint(release, "hook", nil); err != nil {
			return err
		}
	}
	return nil
}

func (m *manager) releaseBranchFor(svc domain.ReleaseService, ref string) string {
	if ref == gitflow.ReleaseBranchRef {
		return svc.ReleaseBranch
	}
	return ref
}

func (m *manager) triggerReleasePipelines(ctx context.Context, release *domain.Release, step gitflow.Step, statusCh chan<- string) error {
	for i := range release.Services {
		svc := &release.Services[i]
		client, repo, worktreePath, err := m.releaseForgeTarget(ctx, *svc)
		if err != nil {
			return serviceFailure(svc, err)
		}
		branch := m.releaseBranchFor(*svc, step.On)
		sendStatus(statusCh, fmt.Sprintf("[%s][pipeline] triggering on %s", svc.Name, branch))
		if err := client.TriggerPipeline(ctx, forge.TriggerPipelineParams{WorktreePath: worktreePath, Branch: branch, Repo: repo}); err != nil {
			return serviceFailure(svc, fmt.Errorf("trigger pipeline on %s: %w", branch, err))
		}
	}
	return nil
}

func (m *manager) releaseForgeTarget(ctx context.Context, svc domain.ReleaseService) (forge.ForgeClient, string, string, error) {
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
		return nil, "", "", fmt.Errorf("resolve remote for service %s: %w", svc.Name, err)
	}
	repo := forge.ExtractRepoPath(remoteURL)
	if repo == "" {
		return nil, "", "", fmt.Errorf("resolve repository path for %s: remote URL %q is not parseable", svc.Name, remoteURL)
	}
	return client, repo, worktreePath, nil
}

func (m *manager) releaseWaitHolds(ctx context.Context, release domain.Release, st *gitflow.State) (bool, error) {
	switch st.Wait {
	case gitflow.WaitReviewMerged:
		return allReleaseServicesMerged(release.Services), nil
	case gitflow.WaitCIGreen:
		green, _, err := m.releaseCIGreen(ctx, release, st.WaitOn)
		return green, err
	}
	return false, nil
}

func (m *manager) releaseCIGreen(ctx context.Context, release domain.Release, ref string) (bool, string, error) {
	for _, svc := range release.Services {
		client, repo, _, err := m.releaseForgeTarget(ctx, svc)
		if err != nil {
			return false, "", err
		}
		if green, detail, err := m.ciGreenOnHead(ctx, client, repo, svc.RepoPath, svc.Name, m.releaseBranchFor(svc, ref)); err != nil || !green {
			return false, detail, err
		}
	}
	return true, "", nil
}
