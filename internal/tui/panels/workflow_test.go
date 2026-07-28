package panels

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestRenderWorkflow_RendersCompactChainAndAction(t *testing.T) {
	wf := &domain.WorkflowSummary{
		Steps: []domain.WorkflowStep{
			{Phase: domain.TaskWorkflowCode, Label: "code", State: "done"},
			{Phase: domain.TaskWorkflowMR, Label: "MR", State: "now"},
			{Phase: domain.TaskWorkflowReviewCI, Label: "review + CI", State: "next"},
			{Phase: domain.TaskWorkflowMerge, Label: "merge", State: "blocked"},
		},
		NextAction: "wait for approvals",
	}

	got := stripAnsi(renderWorkflow(wf, 80))
	for _, want := range []string{"✓ code", "● MR", "○ review + CI", "✗ merge", "─▶", "ⓘ wait for approvals"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderWorkflow() missing %q: %q", want, got)
		}
	}
}

func TestRenderWorkflow_BlockedMessageTakesPriority(t *testing.T) {
	wf := &domain.WorkflowSummary{
		Steps:   []domain.WorkflowStep{{Label: "master MR", State: "blocked"}},
		Blocker: "CI failed",
	}

	got := stripAnsi(renderWorkflow(wf, 40))
	if !strings.Contains(got, "ⓘ CI failed") {
		t.Fatalf("renderWorkflow() = %q", got)
	}
}

func TestRenderWorkflow_WideChainIsCentered(t *testing.T) {
	wf := &domain.WorkflowSummary{
		Steps: []domain.WorkflowStep{{Label: "code", State: "done"}},
	}

	got := renderWorkflow(wf, 40)
	line := strings.Split(got, "\n")[0]
	if !strings.HasPrefix(stripAnsi(line), " ") {
		t.Fatalf("centered chain should have leading padding: %q", stripAnsi(line))
	}
}

func TestRenderWorkflow_Width40WrapsWithoutTruncation(t *testing.T) {
	wf := &domain.WorkflowSummary{
		Steps: []domain.WorkflowStep{
			{Label: "code", State: "done"},
			{Label: "MR", State: "done"},
			{Label: "review + CI", State: "now"},
			{Label: "merge", State: "next"},
			{Label: "release", State: "next"},
		},
		NextAction: "address every review comment before merge",
	}

	got := renderWorkflow(wf, 40)
	for i, line := range strings.Split(got, "\n") {
		if width := lipgloss.Width(line); width > 40 {
			t.Errorf("line %d width = %d: %q", i, width, stripAnsi(line))
		}
	}
	plain := stripAnsi(got)
	for _, want := range []string{"review + CI", "release", "address", "every", "review", "comment", "before", "merge"} {
		if !strings.Contains(plain, want) {
			t.Errorf("wrapped output lost %q: %q", want, plain)
		}
	}
}

func TestRenderPaneTitle_RightAlignsPeerTab(t *testing.T) {
	got := stripAnsi(renderPaneTitle("[2] Services · ITPR-1  [1/2]", "[3] Releases", 60))
	if !strings.Contains(got, "[2] Services · ITPR-1") || !strings.Contains(got, "[3] Releases") {
		t.Fatalf("renderPaneTitle() = %q", got)
	}
	if !strings.HasSuffix(got, "[3] Releases") {
		t.Fatalf("peer tab should be right-aligned: %q", got)
	}
}

func TestRenderPaneTitle_NarrowWidthTruncatesLeft(t *testing.T) {
	got := renderPaneTitle("[2] Services · LONG-TASK-ID", "[3] Releases", 10)
	if lipgloss.Width(got) > 10 {
		t.Fatalf("title width = %d, want <= 10: %q", lipgloss.Width(got), stripAnsi(got))
	}
}
