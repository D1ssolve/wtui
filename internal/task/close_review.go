package task

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

// Review-close reconciliation states returned by reconcileReviewClose.
const (
	// reviewCloseCreate: no active MR exists; the caller creates one and the
	// service waits for merge.
	reviewCloseCreate = "create"
	// reviewCloseWaiting: an MR is open; the caller must not recreate it and
	// must not run close post-actions.
	reviewCloseWaiting = "waiting"
	// reviewCloseVerified: the exact MR merged and its authoritative merge
	// SHA passed fresh target containment; the caller may run post-actions
	// against the accepted merge SHA.
	reviewCloseVerified = "verified"
)

type reviewSourcePublication struct {
	state    string
	openHead string
}

// sourceRemovedOnMerge reports an eligible review whose forge removes the source on merge:
// its MR history is classified before the source is published, so a merged review is
// verified read-only and its removed source is never recreated.
func (m *manager) sourceRemovedOnMerge(service, branch string) bool {
	rule := m.branchRule(service, branch)
	return rule.MR.RemoveSourceBranch && gitflow.SourceNeededAfterReview(rule) == ""
}

// publishReviewSource pushes the source after classification: a new review publishes it as
// before; an open review is only fast-forwarded in place, leased on the observed remote head,
// so a merge (and source removal) racing this push makes it fail instead of recreating the ref.
func (m *manager) publishReviewSource(ctx context.Context, svc domain.Service, publication reviewSourcePublication) (bool, error) {
	switch publication.state {
	case reviewCloseCreate:
		return true, m.pushBranch(ctx, svc.WorktreePath)
	case reviewCloseWaiting:
	default:
		return false, fmt.Errorf("cannot publish source for review state %q", publication.state)
	}
	ref := "refs/heads/" + svc.Branch
	remote, err := m.git.RemoteRefSHA(ctx, svc.RepoPath, ref)
	if err != nil {
		return false, withRecovery(err, "check remote source ref access; refresh before retrying")
	}
	if remote == "" {
		return false, withRecovery(fmt.Errorf("source %s is absent on the remote while its MR is open; not re-publishing it", svc.Branch), "inspect the MR in forge; press C again once it is merged")
	}
	if remote != publication.openHead {
		return false, withRecovery(fmt.Errorf("open MR head %.8s does not match published source %.8s", publication.openHead, remote), "compare the MR head with the remote source and refresh before retrying")
	}
	local, err := m.git.ResolveRef(ctx, svc.RepoPath, ref)
	if err != nil {
		return false, withRecovery(err, "inspect the local source branch; restore its ref")
	}
	if local == remote {
		return false, nil
	}
	forward, err := m.git.IsAncestor(ctx, svc.RepoPath, remote, local)
	if err != nil {
		return false, withRecovery(err, "inspect source history; repair verification")
	}
	if !forward {
		return false, withRecovery(fmt.Errorf("remote source %s (%.8s) is not a fast-forward of local %.8s", svc.Branch, remote, local), "compare local and remote source; reconcile intended changes")
	}
	pushURL, err := m.verifiedPushURL(ctx, svc)
	if err != nil {
		return false, err
	}
	return true, m.git.PushRefWithLease(ctx, svc.RepoPath, pushURL, ref, local, ref, remote)
}

// reconcileReviewClose returns the state and its evidence SHA: the published
// head for a removal-enabled open review, or the contained merge for a verified review.
func (m *manager) reconcileReviewClose(ctx context.Context, svc domain.Service, target, repo string, client forge.ForgeClient) (string, string, error) {
	history, ok := client.(forge.HistoryClient)
	if !ok {
		return "", "", errors.New("forge does not support MR history")
	}
	rows, err := history.MRHistory(ctx, svc.Branch, repo)
	if err != nil {
		return "", "", err
	}
	removal := m.sourceRemovedOnMerge(svc.Name, svc.Branch)
	const inspect = "inspect MR source, target and state in the forge; refresh before retrying"
	if removal || m.hasServiceRule(svc) {
		if svc.Branch == "" || target == "" {
			return "", "", withRecovery(errors.New("review source or target branch is missing"), inspect)
		}
		// This listing is source-scoped. A row we cannot classify must not
		// disappear into the same bucket as genuinely empty history.
		seen := make(map[int]struct{}, len(rows))
		for _, row := range rows {
			if row.Number <= 0 || row.SourceBranch != svc.Branch || row.TargetBranch != target {
				return "", "", withRecovery(fmt.Errorf("MR history identity is incomplete or unexpected: !%d %q → %q, expected %q → %q", row.Number, row.SourceBranch, row.TargetBranch, svc.Branch, target), inspect)
			}
			if _, duplicate := seen[row.Number]; duplicate {
				return "", "", withRecovery(fmt.Errorf("ambiguous MR history: duplicate !%d", row.Number), inspect)
			}
			seen[row.Number] = struct{}{}
			switch strings.ToLower(strings.TrimSpace(row.State)) {
			case "open", "opened", "merged", "closed":
			default:
				return "", "", withRecovery(fmt.Errorf("MR history !%d has unknown state %q", row.Number, row.State), inspect)
			}
		}
	}
	active, _ := matchTargetMRs(rows, svc.Branch, target)
	if len(active) > 1 {
		return "", "", fmt.Errorf("ambiguous MR history for %s → %s", svc.Branch, target)
	}
	if len(active) == 0 {
		return reviewCloseCreate, "", nil
	}
	want := active[0]
	fresh, err := client.MRReadinessByNumber(ctx, want.Number, repo, svc.WorktreePath)
	if err != nil {
		return "", "", err
	}
	if removal || m.hasServiceRule(svc) {
		if fresh.Number != want.Number || fresh.SourceBranch != svc.Branch || fresh.TargetBranch != target {
			return "", "", withRecovery(fmt.Errorf("MR !%d identity changed or is incomplete: got !%d %q → %q, expected %q → %q", want.Number, fresh.Number, fresh.SourceBranch, fresh.TargetBranch, svc.Branch, target), inspect)
		}
		if strings.TrimSpace(fresh.HeadSHA) == "" {
			return "", "", withRecovery(fmt.Errorf("MR !%d has no head SHA", want.Number), inspect)
		}
		if mergedMRState(want.State) && !mergedMRState(fresh.State) {
			return "", "", withRecovery(fmt.Errorf("MR !%d is listed as merged but numbered detail is %q", want.Number, fresh.State), inspect)
		}
	}
	if _, err := m.reconcileDirectMR(ctx, svc.RepoPath, m.mergeMethodForBranch(svc.Name, svc.Branch), &fresh); err != nil {
		return "", "", err
	}
	switch strings.ToLower(strings.TrimSpace(fresh.State)) {
	case "open", "opened":
		return reviewCloseWaiting, fresh.HeadSHA, nil
	case "merged":
	default:
		return "", "", fmt.Errorf("MR !%d is %s", want.Number, fresh.State)
	}
	// Accept the merged MR only against the authoritative source: a fresh
	// fetch plus an exact local==remote source equality, so a source that
	// advanced on the remote (or never left this machine) can never
	// authorize post-actions on stale local code.
	// A forge asked to remove the source on merge leaves no remote ref; only then is the
	// local head accepted, and only as far as it equals the merged MR head checked below.
	head, err := m.freshSourceSHA(ctx, svc, removal)
	if err != nil {
		return "", "", fmt.Errorf("resolve current source %s: %w", svc.Branch, err)
	}
	if err := validateHotfixMRIdentity(svc, head, want, fresh); err != nil {
		return "", "", fmt.Errorf("merged MR !%d: %w", want.Number, err)
	}
	mergeSHA, err := m.verifyHotfixMergeSHA(ctx, svc, target, fresh)
	if err != nil {
		return "", "", fmt.Errorf("merged MR !%d: %w", want.Number, err)
	}
	return reviewCloseVerified, mergeSHA, nil
}
