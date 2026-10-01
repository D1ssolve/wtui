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
		item.Status = "failed"
		item.Blockers = []string{clientErr.Error()}
		return item
	}
	repo := forge.ExtractRepoPath(svc.RemoteURL)
	if repo == "" {
		item.Status = "failed"
		item.Blockers = []string{fmt.Sprintf("resolve repository path for %s: remote URL %q is not parseable", svc.Name, svc.RemoteURL)}
		return item
	}
	var readErr error
	item.MR, readErr = client.MRReadiness(ctx, svc.Branch, repo, svc.WorktreePath)
	item.Blockers = append([]string(nil), item.MR.Blockers...)
	switch {
	case readErr != nil:
		item.Status = "failed"
		item.Blockers = []string{readErr.Error()}
	case item.MR.Number == 0:
		item = m.reconcileMergedReviewMR(ctx, svc, client, repo, reviewTarget, item)
	default:
		if drift := reviewMRDriftBlocker(item.MR, svc.Branch, reviewTarget); drift != "" {
			item.Status = "blocked"
			item.Blockers = append(item.Blockers, drift)
			break
		}
		switch {
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
		item.Status = "failed"
		item.Blockers = []string{err.Error()}
		return item
	}
	history, ok := client.(forge.HistoryClient)
	if !ok {
		item.Status = "no_mr"
		return item
	}
	rows, err := history.MRHistory(ctx, svc.Branch, repo)
	if err != nil {
		return fail(fmt.Errorf("read MR history for %s: %w", svc.Branch, err))
	}
	active, _ := matchTargetMRs(rows, svc.Branch, reviewTarget)
	switch {
	case len(active) == 0:
		item.Status = "no_mr"
		return item
	case len(active) > 1:
		return fail(fmt.Errorf("ambiguous MR history for %s -> %s: %d active MRs", svc.Branch, reviewTarget, len(active)))
	}
	mr, err := client.MRReadinessByNumber(ctx, active[0].Number, repo, svc.WorktreePath)
	if err != nil {
		return fail(err)
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
		return fmt.Errorf("MR !%d state is %q, not merged", mr.Number, mr.State)
	}
	if mr.SourceBranch != svc.Branch {
		return fmt.Errorf("merged MR !%d source is %s, want %s", mr.Number, mr.SourceBranch, svc.Branch)
	}
	if reviewTarget == "" || mr.TargetBranch != reviewTarget {
		return fmt.Errorf("merged MR !%d targets %s, want %s", mr.Number, mr.TargetBranch, reviewTarget)
	}
	if mr.HeadSHA == "" {
		return fmt.Errorf("merged MR !%d has no head SHA", mr.Number)
	}
	if mr.HeadSHA != currentSHA {
		return fmt.Errorf("merged MR !%d head %s does not match current source %s", mr.Number, mr.HeadSHA, currentSHA)
	}
	if strings.TrimSpace(mr.MergedSHA) == "" {
		return fmt.Errorf("merged MR !%d has no merged SHA", mr.Number)
	}
	return nil
}
