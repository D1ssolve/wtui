package task

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
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
	head, err := m.resolveFreshSourceSHA(ctx, svc)
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
