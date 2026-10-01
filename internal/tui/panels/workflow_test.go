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

func TestRenderWorkflow_OversizedStepTruncated(t *testing.T) {
	long := "this-label-is-far-too-long-for-the-row"
	for name, steps := range map[string][]domain.WorkflowStep{
		"first":  {{Label: long, State: "done"}, {Label: "MR", State: "next"}},
		"middle": {{Label: "code", State: "done"}, {Label: long, State: "now"}, {Label: "merge", State: "next"}},
		"last":   {{Label: "code", State: "done"}, {Label: long, State: "next"}},
	} {
		t.Run(name, func(t *testing.T) {
			got := renderWorkflow(&domain.WorkflowSummary{Steps: steps}, 20)
			for i, line := range strings.Split(got, "\n") {
				if width := lipgloss.Width(line); width > 20 {
					t.Errorf("line %d width = %d: %q", i, width, stripAnsi(line))
				}
			}
		})
	}
}

func TestRenderWorkflow_OversizedCJKStepTruncated(t *testing.T) {
	wf := &domain.WorkflowSummary{Steps: []domain.WorkflowStep{
		{Label: "レビューと継続的インテグレーション", State: "now"},
	}}

	got := renderWorkflow(wf, 10)
	for i, line := range strings.Split(got, "\n") {
		if width := lipgloss.Width(line); width > 10 {
			t.Errorf("line %d width = %d: %q", i, width, stripAnsi(line))
		}
	}
	if !strings.Contains(stripAnsi(got), "…") {
		t.Fatalf("oversized CJK step must be truncated with ellipsis: %q", stripAnsi(got))
	}
}

func TestRenderWorkflow_WidthSafeSweep(t *testing.T) {
	wf := &domain.WorkflowSummary{
		Steps: []domain.WorkflowStep{
			{Label: "code", State: "done"},
			{Label: "レビュー CI", State: "now"},
			{Label: "merge", State: "blocked"},
		},
		NextAction: "request review",
	}

	for width := 1; width <= 100; width++ {
		got := renderWorkflow(wf, width)
		for i, line := range strings.Split(got, "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Fatalf("width %d: line %d width = %d: %q", width, i, w, stripAnsi(line))
			}
		}
	}
}

func TestRenderPaneTitle_RightAlignsPeerTab(t *testing.T) {
	got := stripAnsi(renderPaneTitle("SERVICES - ITPR-1  [1/2]", "RELEASES  [3]  ›", 60))
	if !strings.Contains(got, "SERVICES - ITPR-1") || !strings.Contains(got, "RELEASES") {
		t.Fatalf("renderPaneTitle() = %q", got)
	}
	if !strings.HasSuffix(got, "RELEASES  [3]  ›") {
		t.Fatalf("peer tab should be right-aligned: %q", got)
	}
}

func TestRenderPaneTitle_NarrowWidthTruncatesLeft(t *testing.T) {
	got := renderPaneTitle("SERVICES - LONG-TASK-ID", "RELEASES", 10)
	if lipgloss.Width(got) > 10 {
		t.Fatalf("title width = %d, want <= 10: %q", lipgloss.Width(got), stripAnsi(got))
	}
}

func TestRenderWorkflow_WideUsesCards(t *testing.T) {
	wf := &domain.WorkflowSummary{Steps: []domain.WorkflowStep{
		{Phase: domain.TaskWorkflowCode, Label: "code", State: "done"},
		{Phase: domain.TaskWorkflowMR, Label: "MR", State: "now"},
		{Phase: domain.TaskWorkflowReviewCI, Label: "review + CI", State: "next"},
	}}

	got := stripAnsi(renderWorkflow(wf, 100))
	if !strings.Contains(got, "╭") || !strings.Contains(got, "→") {
		t.Fatalf("wide workflow is not card based: %q", got)
	}
}

func TestRenderWorkflow_CompactUsesChain(t *testing.T) {
	wf := &domain.WorkflowSummary{Steps: []domain.WorkflowStep{
		{Label: "code", State: "done"},
		{Label: "MR", State: "now"},
	}}

	got := stripAnsi(renderWorkflow(wf, 40))
	if strings.Contains(got, "╭") || !strings.Contains(got, "code") {
		t.Fatalf("compact workflow = %q", got)
	}
}
