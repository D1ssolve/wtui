package task

import (
	"slices"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func TestR4Gate_B2_SummaryWaitsForEveryServiceReviewTarget(t *testing.T) {
	const id = "TASK-1"
	flow, err := gitflow.ParseWorkflowYAML(`workflow: {extends: git-flow, services: {a: {task_types: {feature: {close: [{review: {into: [main, stable]}}]}}}}}`)
	if err != nil {
		t.Fatal(err)
	}
	main := forge.MRReadiness{Number: 7, SourceBranch: "feature/a", TargetBranch: "main", HeadSHA: "head", State: "opened", Ready: true}
	stable := main
	stable.Number, stable.TargetBranch = 8, "stable"
	develop := forge.MRReadiness{Number: 9, SourceBranch: "feature/b", TargetBranch: "develop", HeadSHA: "head", State: "merged", MergedSHA: "delivered"}
	queue := &queueForgeClient{mergeForgeClient: mergeForgeClient{
		readinessByNumber: map[int]forge.MRReadiness{7: main, 9: develop},
		history: map[string][]forge.MRInfo{
			"feature/a": {{Number: 7, SourceBranch: "feature/a", TargetBranch: "main", State: "opened"}},
			"feature/b": {{Number: 9, SourceBranch: "feature/b", TargetBranch: "develop", State: "merged"}},
		},
	}}
	m, g := newMRMergeTestManager(t, map[string]string{"a": "feature/a", "b": "feature/b"}, "git@gitlab.com:group/repo.git", queue)
	m.flow, m.cfg.Forge.MergeQueue = flow, true
	g.resolveRefRes, g.remoteBranchExistsRes = "head", true
	g.remoteRefSHAFn = func(_, ref string) (string, error) {
		if strings.HasPrefix(ref, "refs/heads/feature/") {
			return "head", nil
		}
		return "delivered", nil
	}
	g.isAncestorFn = func(_, from, _ string) (bool, error) { return from == "delivered", nil }
	services, err := m.ListServices(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	enrollAndDeliver := func(mr forge.MRReadiness) {
		t.Helper()
		r, err := m.MergeServiceMR(t.Context(), id, "a", MRSelection{Number: mr.Number, TargetBranch: mr.TargetBranch, HeadSHA: mr.HeadSHA})
		if err != nil || !slices.Equal(r.Queued, []string{"a"}) {
			t.Fatalf("enroll %s: %+v, %v", mr.TargetBranch, r, err)
		}
		mr.State, mr.MergedSHA = "merged", "delivered"
		queue.readinessByNumber[mr.Number] = mr
	}
	check := func(stableStatus string, phase domain.WorkflowPhase, ready bool) {
		t.Helper()
		ins, err := m.InspectTaskMerge(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(ins.Services, func(row ServiceMergeInspection) bool {
			return row.ServiceName == "a" && row.MR.TargetBranch == "stable" && row.Status == stableStatus
		}) {
			t.Fatalf("stable: want %s in %+v", stableStatus, ins.Services)
		}
		gotWait, err := m.taskReviewWaitHolds(t.Context(), &taskFlow{taskID: id, services: services})
		if err != nil || gotWait != ready {
			t.Fatalf("wait=%v, %v; want %v", gotWait, err, ready)
		}
		summary, err := m.TaskWorkflow(t.Context(), id)
		if err != nil || summary.Current != phase {
			t.Errorf("stable %s: summary phase=%q next=%q, err=%v; want %q", stableStatus, summary.Current, summary.NextAction, err, phase)
		}
	}
	enrollAndDeliver(main)
	check("no_mr", domain.TaskWorkflowMR, false)
	queue.history["feature/a"] = append(queue.history["feature/a"], forge.MRInfo{Number: 8, SourceBranch: "feature/a", TargetBranch: "stable", State: "opened"})
	queue.readinessByNumber[8] = stable
	enrollAndDeliver(stable)
	check("merged", domain.TaskWorkflowReleaseEligible, true)

	// B4: once the rule stops reviewing into main, its recorded pin is stale evidence, as M reports.
	if m.flow, err = gitflow.ParseWorkflowYAML(`workflow: {extends: git-flow, services: {a: {task_types: {feature: {close: [{review: {into: [stable, next]}}]}}}}}`); err != nil {
		t.Fatal(err)
	}
	// M refuses to enroll beside a stale pin, so next is delivered as an ordinary merged MR.
	next := stable
	next.Number, next.TargetBranch, next.State, next.MergedSHA = 10, "next", "merged", "delivered"
	queue.history["feature/a"] = append(queue.history["feature/a"], forge.MRInfo{Number: 10, SourceBranch: "feature/a", TargetBranch: "next", State: "merged"})
	queue.readinessByNumber[10] = next
	if summary, err := m.TaskWorkflow(t.Context(), id); err != nil || summary.Current == domain.TaskWorkflowReleaseEligible {
		t.Fatalf("stale main pin: summary phase=%q next=%q err=%v; want not release-eligible", summary.Current, summary.NextAction, err)
	}
}
