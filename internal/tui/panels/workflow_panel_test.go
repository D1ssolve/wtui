package panels

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/domain"
)

func testWorkflow() *domain.WorkflowSummary {
	return &domain.WorkflowSummary{
		Steps: []domain.WorkflowStep{
			{Phase: domain.TaskWorkflowCode, Label: "code", State: "done"},
			{Phase: domain.TaskWorkflowMR, Label: "MR", State: "now"},
			{Phase: domain.TaskWorkflowMerge, Label: "merge", State: "next"},
		},
		NextAction: "request review",
	}
}

func TestWorkflowPanel_NoContext_RendersNothing(t *testing.T) {
	if got := RenderWorkflowPanel(WorkflowPanelInput{Width: 80}); got != "" {
		t.Fatalf("RenderWorkflowPanel() = %q, want empty", got)
	}
}

func TestWorkflowPanel_TaskIdentityStepsAndAction(t *testing.T) {
	got := stripAnsi(RenderWorkflowPanel(WorkflowPanelInput{
		Kind:     WorkflowPanelTask,
		Identity: "ITPR-42",
		Workflow: testWorkflow(),
		Width:    80,
	}))
	for _, want := range []string{"TASK", "ITPR-42", "✓ code", "● MR", "○ merge", "ⓘ request review"} {
		if !strings.Contains(got, want) {
			t.Errorf("workflow panel missing %q: %q", want, got)
		}
	}
	if strings.Index(got, "ITPR-42") > strings.Index(got, "✓ code") {
		t.Fatalf("identity must precede steps: %q", got)
	}
}

func TestWorkflowPanel_ReleaseIdentityWithVersion(t *testing.T) {
	got := stripAnsi(RenderWorkflowPanel(WorkflowPanelInput{
		Kind:     WorkflowPanelRelease,
		Identity: "rel-1.2.3",
		Version:  "1.2.3",
		Workflow: testWorkflow(),
		Width:    80,
	}))
	for _, want := range []string{"RELEASE", "rel-1.2.3", "v1.2.3"} {
		if !strings.Contains(got, want) {
			t.Errorf("release panel missing %q: %q", want, got)
		}
	}
}

func TestWorkflowPanel_ReleaseMixedVersions(t *testing.T) {
	got := stripAnsi(RenderWorkflowPanel(WorkflowPanelInput{
		Kind:     WorkflowPanelRelease,
		Identity: "rel-mixed",
		Workflow: testWorkflow(),
		Width:    80,
	}))
	if !strings.Contains(got, "Versions: mixed") {
		t.Fatalf("mixed release must say so: %q", got)
	}
}

func TestWorkflowPanel_BlockerAuthoritativeOverNextAction(t *testing.T) {
	wf := testWorkflow()
	wf.Blocker = "CI failed on MR"
	got := stripAnsi(RenderWorkflowPanel(WorkflowPanelInput{
		Kind:     WorkflowPanelTask,
		Identity: "ITPR-42",
		Workflow: wf,
		Width:    80,
	}))
	if !strings.Contains(got, "ⓘ CI failed on MR") {
		t.Fatalf("blocker missing: %q", got)
	}
	if strings.Contains(got, "ⓘ request review") {
		t.Fatalf("next action must yield to blocker: %q", got)
	}
}

func TestWorkflowPanel_GuidanceRows_KeepAllMatchesForSelectedService(t *testing.T) {
	wf := testWorkflow()
	wf.Services = []domain.ServiceWorkflow{
		{ServiceName: "api", Status: "blocked", Detail: "merge blocked: need rebase"},
		{ServiceName: "api", Status: "next", NextAction: "push hotfix branch"},
		{ServiceName: "worker", Status: "done", Detail: "irrelevant"},
	}
	got := stripAnsi(RenderWorkflowPanel(WorkflowPanelInput{
		Kind:     WorkflowPanelTask,
		Identity: "ITPR-42",
		Workflow: wf,
		Service:  "api",
		Width:    100,
	}))
	if !strings.Contains(got, "api: merge blocked: need rebase") {
		t.Fatalf("missing first guidance row: %q", got)
	}
	if !strings.Contains(got, "api: push hotfix branch") {
		t.Fatalf("missing duplicate guidance row: %q", got)
	}
	if strings.Contains(got, "worker") {
		t.Fatalf("guidance must be limited to the selected service: %q", got)
	}
}

func TestWorkflowPanel_LoadingRendersSingleDimLine(t *testing.T) {
	got := RenderWorkflowPanel(WorkflowPanelInput{
		Kind:     WorkflowPanelTask,
		Identity: "ITPR-42",
		Loading:  true,
		Width:    80,
	})
	plain := stripAnsi(got)
	if !strings.Contains(plain, "Loading workflow") {
		t.Fatalf("loading state missing: %q", plain)
	}
	if strings.Count(plain, "\n") != 0 {
		t.Fatalf("loading state must be a single line: %q", plain)
	}
	if strings.Contains(plain, "ITPR-42") {
		t.Fatalf("loading state must not render identity/steps: %q", plain)
	}
}

func TestWorkflowPanel_ShortTerminal_MessageOnly(t *testing.T) {
	got := stripAnsi(RenderWorkflowPanel(WorkflowPanelInput{
		Kind:          WorkflowPanelTask,
		Identity:      "ITPR-42",
		Workflow:      testWorkflow(),
		Width:         60,
		ShortTerminal: true,
	}))
	if !strings.Contains(got, "ⓘ request review") {
		t.Fatalf("message line missing: %q", got)
	}
	if strings.Contains(got, "ITPR-42") || strings.Contains(got, "✓ code") {
		t.Fatalf("short terminal must collapse to message only: %q", got)
	}
}

func TestWorkflowPanel_NarrowWidth_ChainOnlyAndBounded(t *testing.T) {
	got := RenderWorkflowPanel(WorkflowPanelInput{
		Kind:     WorkflowPanelTask,
		Identity: "ITPR-42-with-a-long-task-id",
		Workflow: testWorkflow(),
		Width:    40,
	})
	if strings.Contains(stripAnsi(got), "╭") {
		t.Fatalf("narrow strip must not render cards: %q", stripAnsi(got))
	}
	for i, line := range strings.Split(got, "\n") {
		if w := lipgloss.Width(line); w > 40 {
			t.Fatalf("line %d width = %d, want <= 40: %q", i, w, stripAnsi(line))
		}
	}
}

func TestWorkflowPanel_LoadingBoundedAtTinyWidths(t *testing.T) {
	for width := 1; width <= 10; width++ {
		got := RenderWorkflowPanel(WorkflowPanelInput{
			Kind:     WorkflowPanelTask,
			Identity: "ITPR-42",
			Loading:  true,
			Width:    width,
		})
		for i, line := range strings.Split(got, "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Fatalf("width %d: line %d width = %d: %q", width, i, w, stripAnsi(line))
			}
		}
	}
}

func TestWorkflowPanel_WidthSafeSweep(t *testing.T) {
	wf := testWorkflow()
	wf.Services = []domain.ServiceWorkflow{
		{ServiceName: "api", Status: "blocked", Detail: "merge blocked: need rebase before the freeze"},
	}
	for width := 1; width <= 100; width++ {
		got := RenderWorkflowPanel(WorkflowPanelInput{
			Kind:     WorkflowPanelTask,
			Identity: "ITPR-42-with-a-long-task-id",
			Workflow: wf,
			Service:  "api",
			Width:    width,
		})
		for i, line := range strings.Split(got, "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Fatalf("width %d: line %d width = %d: %q", width, i, w, stripAnsi(line))
			}
		}
	}
}

func TestWorkflowPanel_NoWorkflow_RendersNothing(t *testing.T) {
	got := RenderWorkflowPanel(WorkflowPanelInput{
		Kind:     WorkflowPanelTask,
		Identity: "ITPR-42",
		Width:    80,
	})
	if got != "" {
		t.Fatalf("task without workflow must render nothing, got %q", got)
	}
}
