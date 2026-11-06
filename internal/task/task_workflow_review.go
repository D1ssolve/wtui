package task

import (
	"context"
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
)

// inspectWorkflowReviewMR mirrors the readiness classification in mr_merge.go,
// limited to review_request services so workflow guidance never touches the
// forge for direct_merge or none strategies. MRReadiness selects by source
// branch only, so a reported MR whose source or target drifts from the
// configured review target is blocked instead of merge-ready. When no open MR
// exists, exact MR history is consulted so an externally merged MR
// (squash/rebase included) is recognized as merged instead of missing.
func (m *manager) inspectWorkflowReviewMR(ctx context.Context, svc domain.Service, reviewTarget string) ServiceMergeInspection {
	item := ServiceMergeInspection{ServiceName: svc.Name}
	client, clientErr := m.forgeClientForService(ctx, svc)
	if clientErr != nil {
		return failedWorkflowInspection(item, clientErr, "inspect origin URL and forge provider configuration")
	}
	repo := forge.ExtractRepoPath(svc.RemoteURL)
	if repo == "" {
		return failedWorkflowInspection(item, fmt.Errorf("resolve repository path for %s: remote URL %q is not parseable", svc.Name, svc.RemoteURL), "correct the service remote URL; refresh workflow")
	}
	var readErr error
	item.MR, readErr = client.MRReadiness(ctx, svc.Branch, repo, svc.WorktreePath)
	item.Blockers = append([]string(nil), item.MR.Blockers...)
	switch {
	case readErr != nil:
		return failedWorkflowInspection(item, readErr, "inspect MR readiness in forge and CLI diagnostics; refresh")
	case item.MR.Number == 0:
		item = m.reconcileMergedReviewMR(ctx, svc, client, repo, reviewTarget, item)
	default:
		if drift := reviewMRDriftBlocker(item.MR, svc.Branch, reviewTarget); drift != "" {
			item.Status = "blocked"
			item.Blockers = append(item.Blockers, drift)
			item.NextAction = "inspect MR source/target in forge; select the correct MR"
			if item.MR.SourceBranch == "" || item.MR.SourceBranch == svc.Branch {
				item.NextAction = "correct MR target to " + reviewTarget + " in forge; refresh"
			}
			break
		}
		if _, err := m.reconcileDirectMR(ctx, svc.RepoPath, m.mergeMethodForBranch(svc.Branch), &item.MR); err != nil {
			return failedWorkflowInspection(item, err, "inspect saved merge proof and origin push destination")
		}
		item.Blockers = append([]string(nil), item.MR.Blockers...)
		item.NextAction = item.MR.NextAction
		switch {
		case mergedMRState(item.MR.State):
			item.Status = "merged"
			item.NextAction = ""
		case item.MR.Ready:
			item.Status = "ready"
		case waitingBlockers(item.Blockers):
			item.Status = "waiting"
		default:
			item.Status = "blocked"
		}
	}
	return item
}

// reconcileMergedReviewMR recovers an inspection whose open-MR lookup found
// nothing by consulting exact MR history: at most one active MR for
// branch→reviewTarget may exist, and its authoritative numbered detail must
// prove an external merge. Fails closed: ambiguity, unmerged state, identity
// drift, a stale head, or an unproven merged SHA fails the service; a forge
// without history support or no matching MR keeps the honest no_mr.
func (m *manager) reconcileMergedReviewMR(ctx context.Context, svc domain.Service, client forge.ForgeClient, repo, reviewTarget string, item ServiceMergeInspection) ServiceMergeInspection {
	fail := func(err error) ServiceMergeInspection {
		return failedWorkflowInspection(item, err, "inspect numbered MR in forge and repository; refresh")
	}
	history, ok := client.(forge.HistoryClient)
	if !ok {
		item.Status = "no_mr"
		item.NextAction = "inspect forge history; press C only if an MR is needed"
		return item
	}
	rows, err := history.MRHistory(ctx, svc.Branch, repo)
	if err != nil {
		return fail(withRecovery(fmt.Errorf("read MR history for %s: %w", svc.Branch, err), "inspect MR history in forge; restore CLI access"))
	}
	active, _ := matchTargetMRs(rows, svc.Branch, reviewTarget)
	switch {
	case len(active) == 0:
		item.Status = "no_mr"
		return item
	case len(active) > 1:
		return fail(withRecovery(fmt.Errorf("ambiguous MR history for %s -> %s: %d active MRs", svc.Branch, reviewTarget, len(active)), "resolve duplicate active MRs in forge; refresh"))
	}
	item.MR = forge.MRReadiness{Number: active[0].Number, URL: active[0].URL, SourceBranch: active[0].SourceBranch, TargetBranch: active[0].TargetBranch}
	mr, err := client.MRReadinessByNumber(ctx, active[0].Number, repo, svc.WorktreePath)
	if err != nil {
		return fail(withRecovery(err, "inspect numbered MR in forge; restore detail access"))
	}
	item.MR = mr
	if item.MR.Number == 0 {
		item.MR.Number = active[0].Number
	}
	if item.MR.URL == "" {
		item.MR.URL = active[0].URL
	}
	if _, err := m.reconcileDirectMR(ctx, svc.RepoPath, m.mergeMethodForBranch(svc.Branch), &mr); err != nil {
		return fail(withRecovery(err, "inspect saved merge proof and origin push destination"))
	}
	currentSHA, err := m.resolveFreshSourceSHA(ctx, svc)
	if err != nil {
		return fail(err)
	}
	if err := validateMergedReviewMRIdentity(mr, svc, reviewTarget, currentSHA); err != nil {
		return fail(err)
	}
	if err := m.verifyMergedTaskMRContained(ctx, svc, mr.MergedSHA, mr.TargetBranch); err != nil {
		return fail(err)
	}
	item.MR = mr
	if item.MR.URL == "" {
		item.MR.URL = active[0].URL
	}
	item.Blockers = nil
	item.Status = "merged"
	return item
}

// validateMergedReviewMRIdentity fails closed unless the authoritative merged
// MR matches the service branch, the review target, and the fresh
// current-source tip exactly. An external squash/rebase merge rewrites
// history, so source ancestry proves nothing here: only the exact head that
// was merged, still the current source, guarantees no successor commit is
// left unmerged.
func validateMergedReviewMRIdentity(mr forge.MRReadiness, svc domain.Service, reviewTarget, currentSHA string) error {
	if !strings.EqualFold(strings.TrimSpace(mr.State), "merged") {
		return withRecovery(fmt.Errorf("MR !%d state is %q, not merged", mr.Number, mr.State), "inspect MR state in forge; merge intended work if still open")
	}
	if mr.SourceBranch != svc.Branch {
		return withRecovery(fmt.Errorf("merged MR !%d source is %s, want %s", mr.Number, mr.SourceBranch, svc.Branch), "inspect MR source in forge; select the correct MR")
	}
	if reviewTarget == "" || mr.TargetBranch != reviewTarget {
		if reviewTarget == "" {
			return withRecovery(fmt.Errorf("merged MR !%d targets %s, want %s", mr.Number, mr.TargetBranch, reviewTarget), "configure the review target; inspect MR delivery in forge")
		}
		return withRecovery(fmt.Errorf("merged MR !%d targets %s, want %s", mr.Number, mr.TargetBranch, reviewTarget), "check configured target "+reviewTarget+" and MR delivery in forge")
	}
	if mr.HeadSHA == "" {
		return withRecovery(fmt.Errorf("merged MR !%d has no head SHA", mr.Number), "inspect MR head metadata in forge; refresh numbered detail")
	}
	if mr.HeadSHA != currentSHA {
		return &recoveryError{
			err:         fmt.Errorf("merged MR !%d head %s does not match current source %s", mr.Number, mr.HeadSHA, currentSHA),
			explanation: fmt.Sprintf("MR !%d covers a different source commit (merged %.8s, current %.8s)", mr.Number, mr.HeadSHA, currentSHA),
			action:      "compare " + svc.Branch + " with " + reviewTarget + "; create and merge a follow-up MR for intended remaining changes, then press r",
		}
	}
	if strings.TrimSpace(mr.MergedSHA) == "" {
		return withRecovery(fmt.Errorf("merged MR !%d has no merged SHA", mr.Number), "inspect merge SHA in forge; restore missing evidence")
	}
	return nil
}
