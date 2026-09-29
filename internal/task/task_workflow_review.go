package task

import (
	"context"
	"fmt"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
)

// inspectWorkflowReviewMR mirrors the readiness classification in mr_merge.go,
// limited to review_request services so workflow guidance never touches the
// forge for direct_merge or none strategies. MRReadiness selects by source
// branch only, so a reported MR whose source or target drifts from the
// configured review target is blocked instead of merge-ready.
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
		item.Status = "no_mr"
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
