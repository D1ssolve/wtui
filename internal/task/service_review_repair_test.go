package task

import (
	"context"
	"slices"
	"testing"

	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func TestCloseTask_HeterogeneousReviewTargets(t *testing.T) {
	m0, _ := newWorkflowTestManager(t, nil, false, true)
	m := m0.(*manager)
	ctx := t.Context()
	const id = "TASK-1"
	plain, err := gitflow.ParseWorkflowYAML("workflow: {extends: git-flow}")
	if err != nil {
		t.Fatal(err)
	}
	m.flow = plain
	old, err := m.PlanCloseTask(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	mixed, err := gitflow.ParseWorkflowYAML("workflow: {extends: git-flow, services: {a: {task_types: {feature: {close: [{review: {into: [main, stable]}}]}}}}}")
	if err != nil {
		t.Fatal(err)
	}
	m.flow = mixed
	wrongTarget := true
	var created []string
	client := &mockForgeClient{mrHistoryFn: func(_ context.Context, branch, _ string) ([]forge.MRInfo, error) {
		if wrongTarget && branch == "feature/b" {
			return []forge.MRInfo{{Number: 7, State: "opened", SourceBranch: branch, TargetBranch: "other"}}, nil
		}
		return nil, nil
	}, createMRFn: func(_ context.Context, p forge.CreateMRParams) (forge.MRInfo, error) {
		created = append(created, p.SourceBranch+":"+p.TargetBranch)
		return forge.MRInfo{}, nil
	}}
	m.forgeClients[forge.ForgeProviderGitLab] = client
	m.cfg.Close.PushSourceBeforeReview = true
	g := m.git.(*mockGitClient)
	current, err := m.PlanCloseTask(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	assertNoWrites := func() {
		t.Helper()
		if len(created) != 0 || len(g.pushCalls) != 0 || len(g.pushRefWithLeaseCalls) != 0 {
			t.Fatalf("refused close wrote MRs %v or source pushes %v/%v", created, g.pushCalls, g.pushRefWithLeaseCalls)
		}
	}
	if _, err = m.CloseTask(ctx, CloseTaskParams{TaskID: id, Fingerprint: current.Fingerprint}); err == nil {
		t.Fatal("wrong-target MR accepted")
	}
	assertNoWrites()
	wrongTarget = false
	if _, err = m.CloseTask(ctx, CloseTaskParams{TaskID: id, Fingerprint: old.Fingerprint}); err == nil {
		t.Fatal("pre-override approval accepted")
	}
	assertNoWrites()
	result, err := m.CloseTask(ctx, CloseTaskParams{TaskID: id, Fingerprint: current.Fingerprint})
	if err != nil || !result.Waiting {
		t.Fatalf("mixed close: %+v, %v", result, err)
	}
	slices.Sort(created)
	if !slices.Equal(created, []string{"feature/a:main", "feature/a:stable", "feature/b:develop"}) {
		t.Fatalf("created MRs %v", created)
	}
}
