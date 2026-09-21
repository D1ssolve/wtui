package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The model renders modals through modal.OverlayView, whose content width is
// roughly half the terminal width. At transition widths (100-119) the confirm
// dialog must pick its compact row layout so no table row wraps mid-row.
func TestView_TaskMergeConfirmOverlay_TransitionWidthKeepsGeometry(t *testing.T) {
	for _, termW := range []int{110, 120} {
		t.Run(fmt.Sprintf("%d", termW), func(t *testing.T) {
			mgr := &mockManager{releasePreview: taskMergeTestPreview()}
			m := sendWindowSize(newTestModel(t, mgr), termW, 40)
			m, _ = submitTaskMergeRelease(t, m)
			generation := m.releaseTaskMerge.generation

			updated, _ := m.Update(ReleaseTaskMergePlanReadyMsg{Generation: generation, Plan: taskMergeReadyPlan()})
			m = updated.(Model)
			if m.modal == nil {
				t.Fatal("plan-ready must open confirm modal")
			}

			view := ansi.Strip(m.View())
			lines := strings.Split(view, "\n")
			if len(lines) > 40 {
				t.Fatalf("view height = %d, want <= 40", len(lines))
			}
			for i, line := range lines {
				if w := ansi.StringWidth(line); w > termW {
					t.Fatalf("line %d width = %d, want <= %d: %q", i, w, termW, line)
				}
			}

			// Wide row is 54 cols ("api | ZA-1 | !12 | abc12345 | develop@def67890 | ready").
			// Overlay content width: 53 at 110 cols, 58 at 120 cols.
			if termW >= 120 {
				if !strings.Contains(view, "api | ZA-1 | !12") {
					t.Fatalf("table row must render unwrapped at %d cols: %s", termW, view)
				}
			} else {
				if strings.Contains(view, "Service | Task | MR") {
					t.Fatalf("table header must not render when rows exceed content width at %d cols: %s", termW, view)
				}
				if !strings.Contains(view, "api/ZA-1 !12 -> develop ready") {
					t.Fatalf("compact row missing at %d cols: %s", termW, view)
				}
			}
			if !strings.Contains(view, "[Enter/y] execute") {
				t.Fatalf("execute action must be visible at %d cols: %s", termW, view)
			}
		})
	}
}
