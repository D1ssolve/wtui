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

// reconcileReviewClose inspects the exact numbered MR for
// sourceBranch→target using the forge MR history and classifies the close.
// Post-actions may only follow the verified state: a created or open MR means
// the service waits, a merged MR is re-validated against the current source
// head and fresh origin/<target> containment before its merge SHA is
// accepted. A merged MR is never recreated.
// sourceRemovedOnMerge reports an eligible review whose forge removes the source on merge:
// its MR history is classified before the source is published, so a merged review is
// verified read-only and its removed source is never recreated.
func (m *manager) sourceRemovedOnMerge(branch string) bool {
	rule := m.branchRule(branch)
	return rule.MR.RemoveSourceBranch && gitflow.SourceNeededAfterReview(rule) == ""
}

// publishReviewSource pushes the source after classification: a new review publishes it as
// before; an open review is only fast-forwarded in place, leased on the observed remote head,
// so a merge (and source removal) racing this push makes it fail instead of recreating the ref.
func (m *manager) publishReviewSource(ctx context.Context, svc domain.Service, state string) (bool, error) {
	if state == reviewCloseCreate {
		return true, m.pushBranch(ctx, svc.WorktreePath)
	}
	ref := "refs/heads/" + svc.Branch
	remote, err := m.git.RemoteRefSHA(ctx, svc.RepoPath, ref)
	if err != nil {
		return false, withRecovery(err, "check remote source ref access; refresh before retrying")
	}
	if remote == "" {
		return false, withRecovery(fmt.Errorf("source %s is absent on the remote while its MR is open; not re-publishing it", svc.Branch), "inspect the MR in forge; press C again once it is merged")
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

func (m *manager) reconcileReviewClose(ctx context.Context, svc domain.Service, target, repo string, client forge.ForgeClient) (string, string, error) {
	history, ok := client.(forge.HistoryClient)
	if !ok {
		return "", "", errors.New("forge does not support MR history")
	}
	rows, err := history.MRHistory(ctx, svc.Branch, repo)
	if err != nil {
		return "", "", err
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
	if _, err := m.reconcileDirectMR(ctx, svc.RepoPath, m.mergeMethodForBranch(svc.Branch), &fresh); err != nil {
		return "", "", err
	}
	switch strings.ToLower(strings.TrimSpace(fresh.State)) {
	case "open", "opened":
		return reviewCloseWaiting, "", nil
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
	head, err := m.freshSourceSHA(ctx, svc, m.sourceRemovedOnMerge(svc.Branch))
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
