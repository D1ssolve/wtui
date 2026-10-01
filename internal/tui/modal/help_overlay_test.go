package modal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestHelpOverlay_WorkflowContext_ShowsIdentityStepsAndNextAction(t *testing.T) {
	h := NewHelpOverlayWithOptions(false)
	h.SetWorkflow("Task PROJ-101", &domain.WorkflowSummary{
		Steps: []domain.WorkflowStep{
			{Label: "code", State: "done"},
			{Label: "MR", State: "now"},
			{Label: "merge", State: "next"},
		},
		NextAction: "press M to merge ready MRs",
	})

	view := stripAnsi(h.View())
	for _, want := range []string{"Task PROJ-101", "code", "MR", "merge", "ⓘ press M to merge ready MRs", "✓", "●", "○"} {
		if !strings.Contains(view, want) {
			t.Errorf("help with workflow missing %q, got %q", want, view)
		}
	}
}

func TestHelpOverlay_WorkflowContext_BlockerOverridesNextAction(t *testing.T) {
	h := NewHelpOverlayWithOptions(false)
	h.SetWorkflow("Task PROJ-101", &domain.WorkflowSummary{
		Steps: []domain.WorkflowStep{
			{Label: "code", State: "done"},
			{Label: "MR", State: "blocked"},
		},
		NextAction: "should be hidden",
		Blocker:    "MR pipeline failed",
	})

	view := stripAnsi(h.View())
	if !strings.Contains(view, "ⓘ MR pipeline failed") {
		t.Fatalf("help must show blocker, got %q", view)
	}
	if strings.Contains(view, "should be hidden") {
		t.Fatalf("next action must be hidden when blocker present, got %q", view)
	}
	if !strings.Contains(view, "✗") {
		t.Fatalf("blocked step must use ✗ marker, got %q", view)
	}
}

func TestHelpOverlay_WorkflowContext_ShowsServiceTargetDetail(t *testing.T) {
	h := NewHelpOverlayWithOptions(false)
	h.SetWorkflow("Release REL-1", &domain.WorkflowSummary{
		Steps: []domain.WorkflowStep{{Label: "develop", State: "now"}},
		Services: []domain.ServiceWorkflow{
			{ServiceName: "api", Status: "merged", Detail: "MR #12: merged"},
			{ServiceName: "worker", Status: "pending"},
		},
	})

	view := stripAnsi(h.View())
	if !strings.Contains(view, "api: MR #12: merged") {
		t.Fatalf("help must show service detail, got %q", view)
	}
	if strings.Contains(view, "worker") {
		t.Fatalf("services without detail must be omitted, got %q", view)
	}
}

func TestHelpOverlay_SetWorkflowNil_ClearsContext(t *testing.T) {
	h := NewHelpOverlayWithOptions(false)
	h.SetWorkflow("Task PROJ-101", &domain.WorkflowSummary{
		Steps:      []domain.WorkflowStep{{Label: "code", State: "now"}},
		NextAction: "merge now",
	})
	h.SetWorkflow("Task PROJ-101", nil)

	view := stripAnsi(h.View())
	if strings.Contains(view, "Task PROJ-101") || strings.Contains(view, "merge now") {
		t.Fatalf("cleared workflow must not render, got %q", view)
	}
}

func TestHelpOverlay_WorkflowContext_WrapsWithinOverlayWidth(t *testing.T) {
	h := NewHelpOverlayWithOptions(false)
	h.SetTerminalSize(50, 40)
	h.SetWorkflow("Task PROJ-101", &domain.WorkflowSummary{
		Steps: []domain.WorkflowStep{
			{Label: "code", State: "done"},
			{Label: "MR", State: "done"},
			{Label: "review + CI", State: "now"},
			{Label: "merge", State: "next"},
			{Label: "release", State: "next"},
		},
		NextAction: "press M to merge ready MRs when every service review pipeline is green",
	})

	width, _ := overlayContentSize(50, 40)
	context, _, _ := strings.Cut(stripAnsi(h.View()), "Keyboard Shortcuts")
	for i, line := range strings.Split(context, "\n") {
		if lipgloss.Width(line) > width {
			t.Fatalf("line %d exceeds overlay width %d: %q", i, width, line)
		}
	}
}

func TestHelpOverlay_WorkflowContext_ScrollStaysClamped(t *testing.T) {
	h := NewHelpOverlayWithOptions(false)
	h.SetTerminalSize(80, 12)
	h.SetWorkflow("Task PROJ-101", &domain.WorkflowSummary{
		Steps:      []domain.WorkflowStep{{Label: "code", State: "now"}},
		NextAction: "merge now",
	})

	h.scrollOffset = h.maxScrollOffset()
	h.SetWorkflow("Task PROJ-101", nil)
	if h.scrollOffset > h.maxScrollOffset() {
		t.Fatalf("scrollOffset %d exceeds max %d after workflow cleared", h.scrollOffset, h.maxScrollOffset())
	}
}

func TestHelpOverlay_WorkflowContext_StepsEmptyStillShowsIdentityAndNextAction(t *testing.T) {
	h := NewHelpOverlayWithOptions(false)
	h.SetWorkflow("Task PROJ-101", &domain.WorkflowSummary{
		NextAction: "no close action configured",
	})

	view := stripAnsi(h.View())
	if !strings.Contains(view, "Task PROJ-101") {
		t.Fatalf("identity must render without steps, got %q", view)
	}
	if !strings.Contains(view, "ⓘ no close action configured") {
		t.Fatalf("next action must render without steps, got %q", view)
	}
}

func TestHelpOverlay_FullView_FitsOverlayWidthAtNarrowTerminal(t *testing.T) {
	h := NewHelpOverlayWithOptions(true)
	h.SetTerminalSize(50, 200)
	h.SetWorkflow("Task PROJ-101", &domain.WorkflowSummary{
		Steps: []domain.WorkflowStep{
			{Label: "code", State: "done"},
			{Label: "MR", State: "now"},
		},
		NextAction: "press M to merge ready MRs when every review pipeline is green",
	})

	width, _ := overlayContentSize(50, 40)
	for i, line := range strings.Split(stripAnsi(h.View()), "\n") {
		if lipgloss.Width(line) > width {
			t.Errorf("line %d exceeds overlay width %d: %q", i, width, line)
		}
	}
}

func TestHelpOverlay_FullView_WideLayoutKeepsRowsOnOneLine(t *testing.T) {
	h := NewHelpOverlayWithOptions(false)
	h.SetTerminalSize(120, 200)

	view := stripAnsi(h.View())
	for _, want := range []string{
		"  q / Ctrl+C      Quit",
		"  R               Open <taskID>.sln in Rider",
		"  a               Add service to task",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("wide layout must keep fitting rows on one line, missing %q in %q", want, view)
		}
	}
	width, _ := overlayContentSize(120, 40)
	for i, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > width {
			t.Errorf("line %d exceeds overlay width %d: %q", i, width, line)
		}
	}
}

func TestHelpOverlay_NarrowView_KeepsKeyWithDescriptionStart(t *testing.T) {
	h := NewHelpOverlayWithOptions(false)
	h.SetTerminalSize(50, 40)

	view := stripAnsi(h.View())
	if !strings.Contains(view, "Tab / 1 / 2 / 3 / 0 Move focus:") {
		t.Fatalf("long key must stay on the same line as its description start, got %q", view)
	}
}

func helpViewLines(h *HelpOverlay) []string {
	return strings.Split(stripAnsi(h.View()), "\n")
}

func TestHelpOverlay_Scrollable_ShowsScrollFooter(t *testing.T) {
	h := NewHelpOverlayWithOptions(false)
	h.SetTerminalSize(80, 20)
	if h.maxScrollOffset() == 0 {
		t.Fatal("precondition: help must be scrollable at 80x20")
	}

	lines := helpViewLines(h)
	if len(lines) != h.visibleLines() {
		t.Fatalf("scrollable view must reserve one line for footer, got %d lines, want %d", len(lines), h.visibleLines())
	}
	footer := lines[len(lines)-1]
	if !strings.Contains(footer, "more below") || strings.Contains(footer, "more above") {
		t.Fatalf("initial footer must cue downward only, got %q", footer)
	}
	if !strings.Contains(footer, "scroll") {
		t.Fatalf("footer must name scroll controls, got %q", footer)
	}
	if strings.Contains(lines[len(lines)-2], "more below") {
		t.Fatalf("footer must not overwrite content line, got %q", lines[len(lines)-2])
	}
}

func TestHelpOverlay_ScrollFooter_TracksIntermediateAndBottom(t *testing.T) {
	h := NewHelpOverlayWithOptions(false)
	h.SetTerminalSize(80, 20)

	_, _ = h.Update(sendKey("j"))
	footer := helpViewLines(h)[h.visibleLines()-1]
	if !strings.Contains(footer, "more above/below") {
		t.Fatalf("intermediate footer must cue both directions, got %q", footer)
	}

	_, _ = h.Update(sendSpecialKey(tea.KeyEnd))
	footer = helpViewLines(h)[h.visibleLines()-1]
	if !strings.Contains(footer, "more above") || strings.Contains(footer, "more below") {
		t.Fatalf("final footer must cue upward only, got %q", footer)
	}
	if !strings.Contains(helpViewLines(h)[h.visibleLines()-2], "[Esc] or [?] to close") {
		t.Fatalf("bottom view must still show close hint as last content line, got %q", helpViewLines(h))
	}
}

func TestHelpOverlay_ScrollFooter_FitsOverlayWidth(t *testing.T) {
	h := NewHelpOverlayWithOptions(false)
	h.SetTerminalSize(50, 20)
	width, _ := overlayContentSize(50, 20)

	for _, offset := range []int{0, 1, h.maxScrollOffset()} {
		h.scrollOffset = offset
		lines := helpViewLines(h)
		footer := lines[len(lines)-1]
		if lipgloss.Width(footer) > width {
			t.Errorf("footer at offset %d exceeds overlay width %d: %q", offset, width, footer)
		}
	}
}

func TestHelpOverlay_NonScrollable_NoFooter(t *testing.T) {
	h := NewHelpOverlayWithOptions(false)
	h.SetTerminalSize(120, 200)

	lines := helpViewLines(h)
	last := lines[len(lines)-1]
	if !strings.Contains(last, "[Esc] or [?] to close") {
		t.Fatalf("non-scrollable view must end with close hint, got %q", last)
	}
	if strings.Contains(stripAnsi(h.View()), "more below") {
		t.Fatalf("non-scrollable view must not show scroll footer, got %q", stripAnsi(h.View()))
	}
}

func TestHelpOverlay_ScrollFooter_NarrowTopMiddleBottomKeepCloseLabel(t *testing.T) {
	h := NewHelpOverlayWithOptions(false)
	h.SetTerminalSize(50, 20)

	footerAt := func(offset int) string {
		h.scrollOffset = offset
		lines := helpViewLines(h)
		return lines[len(lines)-1]
	}

	top := footerAt(0)
	if !strings.Contains(top, "Esc close") {
		t.Errorf("top footer must keep full close label, got %q", top)
	}
	if !strings.Contains(top, "more below") || strings.Contains(top, "more above") {
		t.Errorf("top footer must cue downward only, got %q", top)
	}
	if !strings.Contains(top, "j/k") {
		t.Errorf("top footer must name scroll keys, got %q", top)
	}

	middle := footerAt(1)
	if !strings.Contains(middle, "Esc close") {
		t.Errorf("middle footer must keep full close label, got %q", middle)
	}
	if !strings.Contains(middle, "more above/below") {
		t.Errorf("middle footer must cue both directions, got %q", middle)
	}

	bottom := footerAt(h.maxScrollOffset())
	if !strings.Contains(bottom, "Esc close") {
		t.Errorf("bottom footer must keep full close label, got %q", bottom)
	}
	if !strings.Contains(bottom, "more above") || strings.Contains(bottom, "more below") {
		t.Errorf("bottom footer must cue upward only, got %q", bottom)
	}
}

func TestHelpOverlay_QuitKeys_QuitApplication(t *testing.T) {
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'q'}},
		{Type: tea.KeyCtrlC},
	} {
		h := NewHelpOverlayWithOptions(false)
		_, cmd := h.Update(key)
		if cmd == nil {
			t.Fatalf("%q must return a cmd", key.String())
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%q while help open must quit the application, got %T", key.String(), cmd())
		}
	}
}

func TestHelpOverlay_TasksPanel_IncludesMergeShortcutRow(t *testing.T) {
	h := NewHelpOverlayWithOptions(false)
	view := stripAnsi(h.View())
	if !strings.Contains(view, "Inspect and merge ready task MRs") {
		t.Fatalf("Tasks Panel section must explain M shortcut, got %q", view)
	}
}
