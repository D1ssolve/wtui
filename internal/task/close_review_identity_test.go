package task

import (
	"context"
	"testing"

	"github.com/D1ssolve/wtui/internal/forge"
)

func TestCloseTask_RemovalReviewRejectsUnprovenIdentity(t *testing.T) {
	const taskID = "IN-REVIEW-ID"
	const branch = "feature/" + taskID
	open := forge.MRInfo{Number: 7, State: "opened", SourceBranch: branch, TargetBranch: "develop"}
	merged := forge.MRInfo{Number: 7, State: "merged", SourceBranch: branch, TargetBranch: "develop"}
	detail := forge.MRReadiness{Number: 7, State: "open", SourceBranch: branch, TargetBranch: "develop", HeadSHA: "published"}
	for _, tc := range []struct {
		name         string
		rows         []forge.MRInfo
		detail       forge.MRReadiness
		pushDisabled bool
	}{
		{name: "wrong historical target", rows: []forge.MRInfo{{Number: 7, State: "merged", SourceBranch: branch, TargetBranch: "master"}}},
		{name: "missing historical target", rows: []forge.MRInfo{{Number: 7, State: "merged", SourceBranch: branch}}},
		{name: "wrong historical target without publishing", rows: []forge.MRInfo{{Number: 7, State: "merged", SourceBranch: branch, TargetBranch: "master"}}, pushDisabled: true},
		{name: "valid row cannot hide incomplete evidence", rows: []forge.MRInfo{open, {Number: 8, State: "merged"}}, detail: detail},
		{name: "incomplete closed row cannot authorize creation", rows: []forge.MRInfo{{State: "closed", SourceBranch: branch, TargetBranch: "develop"}}},
		{name: "unknown historical state", rows: []forge.MRInfo{{Number: 7, State: "unknown", SourceBranch: branch, TargetBranch: "develop"}}, detail: detail},
		{name: "retargeted open review", rows: []forge.MRInfo{open}, detail: forge.MRReadiness{Number: 7, State: "open", SourceBranch: branch, TargetBranch: "master", HeadSHA: "published"}},
		{name: "wrong numbered open review", rows: []forge.MRInfo{open}, detail: forge.MRReadiness{Number: 8, State: "open", SourceBranch: branch, TargetBranch: "develop", HeadSHA: "published"}},
		{name: "wrong numbered source", rows: []forge.MRInfo{open}, detail: forge.MRReadiness{Number: 7, State: "open", SourceBranch: "feature/unrelated", TargetBranch: "develop", HeadSHA: "published"}},
		{name: "missing open head", rows: []forge.MRInfo{open}, detail: forge.MRReadiness{Number: 7, State: "open", SourceBranch: branch, TargetBranch: "develop"}},
		{name: "open head contradicts published source", rows: []forge.MRInfo{open}, detail: forge.MRReadiness{Number: 7, State: "open", SourceBranch: branch, TargetBranch: "develop", HeadSHA: "different"}},
		{name: "merged list contradicts open detail", rows: []forge.MRInfo{merged}, detail: detail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, git, client := reviewCloseEnv(t, taskID, func(g *mockGitClient) {
				g.remoteRefSHAFn = func(_, _ string) (string, error) { return "published", nil }
			})
			m.cfg.Close.PushSourceBeforeReview = !tc.pushDisabled
			rule := m.flow.BranchTypes["feature"]
			rule.TagOnClose, rule.TriggerPipelineOnClose, rule.MR.RemoveSourceBranch = false, false, true
			m.flow.BranchTypes["feature"] = rule
			client.mrHistoryFn = func(context.Context, string, string) ([]forge.MRInfo, error) { return tc.rows, nil }
			client.mrReadinessByNumberFn = func(context.Context, int, string, string) (forge.MRReadiness, error) {
				return tc.detail, nil
			}

			result, err := m.CloseTask(t.Context(), CloseTaskParams{TaskID: taskID})

			if err == nil || result.Success {
				t.Errorf("expected rejected close, got %+v, error %v", result, err)
			}
			if len(git.pushCalls) != 0 || len(git.pushRefWithLeaseCalls) != 0 || *client.createdCount != 0 {
				t.Errorf("rejected review mutated source or created MR: pushes=%d leases=%d creates=%d",
					len(git.pushCalls), len(git.pushRefWithLeaseCalls), *client.createdCount)
			}
		})
	}
}
