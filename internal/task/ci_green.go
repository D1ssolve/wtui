package task

import (
	"context"
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/forge"
)

// ciGreenOnHead reports whether every pipeline run for the branch's current remote head
// succeeded; runs for older commits never count.
func (m *manager) ciGreenOnHead(ctx context.Context, client forge.ForgeClient, repo, repoPath, service, branch string) (bool, string, error) {
	head, err := m.git.RemoteRefSHA(ctx, repoPath, "refs/heads/"+branch)
	if err != nil {
		return false, "", fmt.Errorf("resolve remote head of %s for %s: %w", branch, service, err)
	}
	pipelines, err := client.PipelineStatus(ctx, branch, repo)
	if err != nil {
		return false, "", fmt.Errorf("pipeline status for %s on %s: %w", service, branch, err)
	}
	short := head[:min(8, len(head))]
	found := false
	for _, p := range pipelines {
		if p.HeadSHA != head {
			continue
		}
		found = true
		if !strings.EqualFold(p.Conclusion, "success") && !strings.EqualFold(p.Status, "success") {
			return false, fmt.Sprintf("%s: %s on %s (%s) is %s", service, p.WorkflowName, branch, short, strings.TrimSpace(p.Status+" "+p.Conclusion)), nil
		}
	}
	if !found {
		return false, fmt.Sprintf("%s: no pipeline for the current head of %s (%s) yet", service, branch, short), nil
	}
	return true, "", nil
}
