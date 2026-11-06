package panels

import (
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestWorkflowPanel_BlockerAndRecoveryRenderOnce(t *testing.T) {
	for _, short := range []bool{false, true} {
		wf := testWorkflow()
		wf.Blocker = "api: source branch changed after merge"
		wf.NextAction = "compare branches and create a new MR"
		wf.Services = []domain.ServiceWorkflow{
			{ServiceName: "api", Status: "failed", Blocker: "source branch changed after merge", NextAction: wf.NextAction},
			{ServiceName: "api", Status: "failed", Blocker: "source branch changed after merge", NextAction: wf.NextAction},
		}
		got := stripAnsi(RenderWorkflowPanel(WorkflowPanelInput{
			Kind: WorkflowPanelTask, Identity: "TASK-1", Workflow: wf,
			Service: "api", Width: 120, ShortTerminal: short,
		}))
		for _, message := range []string{"source branch changed after merge", wf.NextAction} {
			if count := strings.Count(got, message); count != 1 {
				t.Errorf("short=%t: %q rendered %d times, want 1:\n%s", short, message, count, got)
			}
		}
	}
}

func TestWorkflowPanel_DistinctServiceRecoveryIsNotHidden(t *testing.T) {
	wf := testWorkflow()
	wf.Blocker = "worker: CI failed"
	wf.NextAction = "fix the worker pipeline"
	wf.Services = []domain.ServiceWorkflow{{
		ServiceName: "api", Status: "blocked",
		Blocker: "unresolved discussions", NextAction: "resolve review threads",
	}}
	got := stripAnsi(RenderWorkflowPanel(WorkflowPanelInput{
		Kind: WorkflowPanelTask, Identity: "TASK-1", Workflow: wf,
		Service: "api", Width: 120,
	}))
	for _, message := range []string{wf.Blocker, wf.NextAction, "api: unresolved discussions", "resolve review threads"} {
		if !strings.Contains(got, message) {
			t.Errorf("missing %q:\n%s", message, got)
		}
	}
}

func TestWorkflowPanel_AggregatedMessagesDoNotRepeatSelectedService(t *testing.T) {
	wf := testWorkflow()
	wf.Blocker = "worker: CI failed; api: unresolved discussions"
	wf.NextAction = "worker: fix CI; api: resolve review threads"
	wf.Services = []domain.ServiceWorkflow{{
		ServiceName: "api", Status: "blocked",
		Blocker: "unresolved discussions", NextAction: "resolve review threads",
	}}
	got := stripAnsi(RenderWorkflowPanel(WorkflowPanelInput{
		Kind: WorkflowPanelTask, Identity: "TASK-1", Workflow: wf,
		Service: "api", Width: 120,
	}))
	for _, message := range []string{"unresolved discussions", "resolve review threads", "CI failed", "fix CI"} {
		if count := strings.Count(got, message); count != 1 {
			t.Errorf("%q rendered %d times, want 1:\n%s", message, count, got)
		}
	}
}

func TestWorkflowPanel_SimilarMessagesRemainDistinct(t *testing.T) {
	wf := testWorkflow()
	wf.Blocker = "api: CI failed yesterday"
	wf.Services = []domain.ServiceWorkflow{{ServiceName: "api", Status: "blocked", Blocker: "CI failed"}}
	got := stripAnsi(RenderWorkflowPanel(WorkflowPanelInput{
		Kind: WorkflowPanelTask, Identity: "TASK-1", Workflow: wf,
		Service: "api", Width: 120,
	}))
	if !strings.Contains(got+"\n", "\n  api: CI failed\n") {
		t.Fatalf("a different summary message hid the selected-service blocker:\n%s", got)
	}
}

func TestWorkflowPanel_GroupedServiceActionRendersOnce(t *testing.T) {
	wf := testWorkflow()
	wf.Blocker = "api: forge unavailable; web: forge unavailable"
	wf.NextAction = "api, web: install the forge CLI; refresh workflow"
	wf.Services = []domain.ServiceWorkflow{
		{ServiceName: "api", Status: "failed", Blocker: "forge unavailable", NextAction: "install the forge CLI; refresh workflow"},
		{ServiceName: "web", Status: "failed", Blocker: "forge unavailable", NextAction: "install the forge CLI; refresh workflow"},
	}
	got := stripAnsi(RenderWorkflowPanel(WorkflowPanelInput{
		Kind: WorkflowPanelTask, Identity: "TASK-1", Workflow: wf,
		Service: "api", Width: 120,
	}))
	if count := strings.Count(got, "install the forge CLI"); count != 1 {
		t.Fatalf("grouped action rendered %d times, want 1:\n%s", count, got)
	}
}

func TestWorkflowPanel_ShortTerminalPointsToDistinctServiceActions(t *testing.T) {
	wf := testWorkflow()
	wf.Blocker = "api: master: conflicts"
	wf.NextAction = "api: master: resolve conflicts"
	wf.Services = []domain.ServiceWorkflow{
		{ServiceName: "api", Status: "blocked", Blocker: "master: conflicts", NextAction: "master: resolve conflicts"},
		{ServiceName: "api", Status: "ready", Detail: "develop: ready", NextAction: "develop: merge the ready MR"},
	}
	got := stripAnsi(RenderWorkflowPanel(WorkflowPanelInput{
		Kind: WorkflowPanelTask, Identity: "TASK-1", Workflow: wf,
		Service: "api", Width: 50, ShortTerminal: true,
	}))
	for _, text := range []string{"master: conflicts", "resolve conflicts", "? help"} {
		if !strings.Contains(got, text) {
			t.Errorf("short workflow lost %q:\n%s", text, got)
		}
	}
}
