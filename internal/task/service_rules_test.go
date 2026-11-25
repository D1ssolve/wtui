package task

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func TestServiceRuleRejectsWrongMRTarget(t *testing.T) {
	mgr, _ := newWorkflowTestManager(t, map[string]forge.MRReadiness{"feature/a": {Number: 1, Ready: true, State: "open", SourceBranch: "feature/a", TargetBranch: "develop"}, "feature/b": {Number: 2, Ready: true, State: "open", SourceBranch: "feature/b", TargetBranch: "develop"}}, false, true)
	m := mgr.(*manager)
	flow, err := gitflow.ParseWorkflowYAML(`workflow: {extends: git-flow, services: {a: {task_types: {feature: {close: [{review: {into: main}}], mr: {draft: true}}}}}}`)
	if err != nil {
		t.Fatal(err)
	}
	m.flow = flow
	plan, err := m.PlanCloseTask(t.Context(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range plan.Services {
		target := "develop"
		if s.ServiceName == "a" {
			target = "main"
		}
		if s.ForgePlan.TargetBranch != target || s.MR.Draft != (s.ServiceName == "a") {
			t.Fatalf("unexpected review plan: %+v", s)
		}
	}
	rows, err := m.InspectTaskMerge(t.Context(), "TASK-1")
	if err != nil || len(rows.Services) != 2 {
		t.Fatalf("inspect: %+v %v", rows, err)
	}
	for _, r := range rows.Services {
		want := "ready"
		if r.ServiceName == "a" {
			want = "blocked"
		}
		if r.Status != want {
			t.Fatalf("%s: %s, want %s", r.ServiceName, r.Status, want)
		}
	}
}

func TestServiceRuleHotfixConfirmationIgnoresUnrelatedOverrides(t *testing.T) {
	m, _, f := hotfixManager(t)
	load := func(service, target string) {
		t.Helper()
		flow, err := gitflow.ParseWorkflowYAML(fmt.Sprintf(`workflow:
  extends: git-flow
  task_types:
    hotfix: {prefixes: [hotfix/], from: production, close: [{review: {into: [master, develop]}}]}
  services:
    %s: {task_types: {hotfix: {close: [{review: {into: [master, %s]}}], mr: {labels: [urgent]}}}}
`, service, target))
		if err != nil {
			t.Fatal(err)
		}
		m.flow = flow
	}
	load("other", "stable")
	p, err := m.PlanCloseTask(t.Context(), "H")
	if err != nil {
		t.Fatal(err)
	}
	load("other", "next")
	if _, err := m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H", DryRun: true, Fingerprint: p.Fingerprint}); err != nil {
		t.Fatal(err)
	}
	load("svc", "stable")
	if _, err := m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H", Fingerprint: p.Fingerprint}); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("stale approval: %v", err)
	}
	if len(f.created) > 0 {
		t.Fatalf("unexpected writes: %v", f.created)
	}
	if _, err := m.CloseTask(t.Context(), CloseTaskParams{TaskID: "H"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.created, []string{"stable"}) || !slices.Equal(f.createdParams[0].Labels, []string{"urgent"}) {
		t.Fatalf("created: %+v", f.createdParams)
	}
}

func TestServiceRuleTaskMachineResumeChecksOnlyParticipants(t *testing.T) {
	const workflow = `workflow:
  extends: git-flow
  task_types:
    feature:
      prefixes: [feature/]
      from: integration
      close: [{review: {into: integration}}]
      machine:
        states:
          coding: {wait: manual, on: {close: {key: C, action: close, to: done}}}
          done: {final: released}
  services:
    a: {task_types: {feature: {from: %s}}}
    unused: {task_types: {feature: {from: %s}}}
`
	load := func(a, other string) *gitflow.ResolvedGitFlow {
		t.Helper()
		f, err := gitflow.ParseWorkflowYAML(fmt.Sprintf(workflow, a, other))
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	flow := load("main", "staging")
	m := newRuleWorkflowTestManager(t, flow.BranchTypes[gitflow.BranchTypeFeature], false).(*manager)
	m.flow = flow
	tf, err := m.loadTaskFlow(t.Context(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.saveTaskFlow(tf); err != nil {
		t.Fatal(err)
	}
	m.flow = load("main", "staging-2")
	summary, err := m.TaskWorkflow(t.Context(), "TASK-1")
	if err != nil || summary.Current != "coding" {
		t.Fatalf("unrelated service invalidated resume: %+v %v", summary, err)
	}
	m.flow = load("recovery", "staging-2")
	if _, err := m.CloseTask(t.Context(), CloseTaskParams{TaskID: "TASK-1"}); !errors.Is(err, ErrTaskFlowStep) {
		t.Fatalf("changed service rule did not block close: %v", err)
	}
}
