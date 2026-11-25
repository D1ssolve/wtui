package task

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"gopkg.in/yaml.v3"
)

func taskMachineRule(t *testing.T, logFile string) gitflow.BranchTypeRule {
	t.Helper()
	var cfg config.Config
	src := `
workflow:
  extends: git-flow
  task_types:
    feature:
      prefixes: [feature/]
      from: integration
      close:
        - review: {into: integration}
      machine:
        states:
          coding: {wait: manual, on: {close: {key: C, action: close, to: stand}}}
          stand:
            run: [{hook: {name: deploy, command: 'echo "$WTUI_TASK_ID:$WTUI_SERVICE:$WTUI_STATE" >> ` + logFile + `'}}]
            next: qa
          qa: {wait: manual, on: {accept: {key: A, to: review}, reject: {key: Z, to: coding}}}
          review: {wait: review_merged, next: done, on: {merge: {key: M, action: merge_reviews}}}
          done: {final: released}
`
	if err := yaml.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatal(err)
	}
	flow, err := gitflow.Resolve(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	return flow.BranchTypes[gitflow.BranchTypeFeature]
}

func TestTaskMachine_GatesCloseAndMergeAndRunsStandHooks(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "deploy.log")
	mgr := newRuleWorkflowTestManager(t, taskMachineRule(t, logFile), false).(*manager)
	ctx := t.Context()

	summary, err := mgr.TaskWorkflow(ctx, "TASK-1")
	if err != nil || !summary.Gated || summary.Current != "coding" || len(summary.Events) != 1 || summary.Events[0].Action != gitflow.EventActionClose {
		t.Fatalf("summary = %+v, err = %v", summary, err)
	}
	if _, err := mgr.MergeTaskMRs(ctx, "TASK-1"); !errors.Is(err, ErrTaskFlowStep) || !strings.Contains(err.Error(), "press C to close") {
		t.Fatalf("merge in coding: %v", err)
	}
	if err := mgr.AdvanceTask(ctx, "TASK-1", "close", nil); err == nil || !strings.Contains(err.Error(), "runs through C") {
		t.Fatalf("close must go through the confirmed C flow: %v", err)
	}

	tf, err := mgr.gateTaskAction(ctx, "TASK-1", gitflow.EventActionClose)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.afterTaskAction(ctx, tf, gitflow.EventActionClose, nil); err != nil {
		t.Fatal(err)
	}
	if summary, _ = mgr.TaskWorkflow(ctx, "TASK-1"); summary.Current != "qa" {
		t.Fatalf("after close: %s", summary.Current)
	}
	if err := mgr.AdvanceTask(ctx, "TASK-1", "reject", nil); err != nil {
		t.Fatal(err)
	}
	tf, _ = mgr.gateTaskAction(ctx, "TASK-1", gitflow.EventActionClose)
	if err := mgr.afterTaskAction(ctx, tf, gitflow.EventActionClose, nil); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AdvanceTask(ctx, "TASK-1", "accept", nil); err != nil {
		t.Fatal(err)
	}
	if summary, _ = mgr.TaskWorkflow(ctx, "TASK-1"); summary.Current != "review" || !slices.ContainsFunc(summary.Events, func(e domain.WorkflowEvent) bool { return e.Key == "M" }) {
		t.Fatalf("review: %+v", summary)
	}
	log, _ := os.ReadFile(logFile)
	lines := strings.Fields(string(log))
	slices.Sort(lines)
	want := []string{"TASK-1:a:stand", "TASK-1:a:stand", "TASK-1:b:stand", "TASK-1:b:stand"}
	if !slices.Equal(lines, want) {
		t.Fatalf("hook runs = %v, want each service deployed on both visits to stand", lines)
	}
}

func TestTaskMachine_ReviewMergedWaitAdvancesAndConfigChangeBlocks(t *testing.T) {
	rule := taskMachineRule(t, filepath.Join(t.TempDir(), "log"))
	mgr := newRuleWorkflowTestManager(t, rule, true).(*manager)
	ctx := t.Context()
	state := `{"type":"feature","fingerprint":"` + rule.Machine.Fingerprint() + `","state":"review"}`
	if err := os.WriteFile(filepath.Join(mgr.taskDir("TASK-1"), taskFlowFileName), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AdvanceTask(ctx, "TASK-1", "", nil); err != nil {
		t.Fatal(err)
	}
	if summary, _ := mgr.TaskWorkflow(ctx, "TASK-1"); summary.Current != "done" {
		t.Fatalf("merged reviews must finish the task: %s", summary.Current)
	}

	stale := `{"type":"feature","fingerprint":"other","state":"qa"}`
	if err := os.WriteFile(filepath.Join(mgr.taskDir("TASK-1"), taskFlowFileName), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.CloseTask(ctx, CloseTaskParams{TaskID: "TASK-1"}); !errors.Is(err, ErrTaskFlowStep) || !strings.Contains(err.Error(), "different feature machine") {
		t.Fatalf("close under a changed machine: %v", err)
	}
}
