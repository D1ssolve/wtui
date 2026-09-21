package modal

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/D1ssolve/wtui/internal/task"
)

const (
	wideWorkerRow    = "worker | APP-2 | !7 | 11112222 | develop@33334444 | ready"
	compactWorkerRow = "worker/APP-2 !7 -> develop ready"
)

func overlayTestPreview() task.ReleasePreview {
	return task.ReleasePreview{
		Rows: []task.ReleasePreviewRow{{ServiceName: "api", Version: "1.2.3", ReleaseBranch: "release/1.2.3", Tag: "v1.2.3"}},
	}
}

func overlayReadyRows() []task.ReleaseTaskMergeRow {
	rows := taskMergeTestRows()
	rows[1].Ready = true
	rows[1].Blockers = nil
	return rows
}

func assertOverlayFitsTerminal(t *testing.T, view string, termW, termH int) {
	t.Helper()
	lines := strings.Split(stripAnsi(view), "\n")
	if len(lines) > termH {
		t.Fatalf("overlay height = %d, want <= %d", len(lines), termH)
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > termW {
			t.Fatalf("line %d width = %d, want <= %d: %q", i, w, termW, line)
		}
	}
}

func TestReleaseExecuteConfirmDialog_FinalOverlay_TransitionWidths(t *testing.T) {
	for _, termW := range []int{80, 100, 110, 119, 120} {
		t.Run(fmt.Sprintf("%d", termW), func(t *testing.T) {
			d := NewReleaseExecuteConfirmDialogWithTaskMerge("", []string{"APP-1", "APP-2"}, map[string]string{"api": "1.2.3"}, overlayTestPreview(), overlayReadyRows(), 7)
			d.SetTerminalSize(termW, 40)

			overlay := OverlayView(d.View(), termW, 40)
			assertOverlayFitsTerminal(t, overlay, termW, 40)
			view := stripAnsi(overlay)

			// Table mode is reserved for 120+ cols even when a row would fit narrower.
			if termW >= 120 {
				if !strings.Contains(view, taskMergeTableHeader) || !strings.Contains(view, wideWorkerRow) {
					t.Fatalf("table mode must render header and rows unwrapped at %d cols: %s", termW, view)
				}
			} else {
				if strings.Contains(view, taskMergeTableHeader) {
					t.Fatalf("table header must not render when rows exceed content width at %d cols: %s", termW, view)
				}
				if !strings.Contains(view, compactWorkerRow) {
					t.Fatalf("compact row missing at %d cols: %s", termW, view)
				}
			}
			if !strings.Contains(view, "[Enter/y] execute [Esc/n] cancel") {
				t.Fatalf("execute action must be visible at %d cols: %s", termW, view)
			}
		})
	}
}

func TestReleaseExecuteConfirmDialog_FinalOverlay_WideRowsStayCompactAtAnyWidth(t *testing.T) {
	rows := overlayReadyRows()
	rows[0].ServiceName = strings.Repeat("s", 120)
	for _, termW := range []int{120, 200} {
		d := NewReleaseExecuteConfirmDialogWithTaskMerge("", []string{"APP-1"}, map[string]string{"api": "1.2.3"}, overlayTestPreview(), rows, 7)
		d.SetTerminalSize(termW, 40)
		overlay := OverlayView(d.View(), termW, 40)
		assertOverlayFitsTerminal(t, overlay, termW, 40)
		if strings.Contains(stripAnsi(overlay), taskMergeTableHeader) {
			t.Fatalf("oversized rows must fall back to compact mode at %d cols", termW)
		}
	}
}

func TestReleaseExecuteConfirmDialog_FinalOverlay_BlockedExposesNoExecution(t *testing.T) {
	for _, termW := range []int{80, 110, 120} {
		t.Run(fmt.Sprintf("%d", termW), func(t *testing.T) {
			d := NewReleaseExecuteConfirmDialogWithTaskMerge("", []string{"APP-1", "APP-2"}, map[string]string{"api": "1.2.3"}, overlayTestPreview(), taskMergeTestRows(), 4)
			d.SetTerminalSize(termW, 40)

			overlay := OverlayView(d.View(), termW, 40)
			assertOverlayFitsTerminal(t, overlay, termW, 40)
			view := stripAnsi(overlay)
			if strings.Contains(view, "[Enter/y] execute") {
				t.Fatalf("blocked state must not expose execute action at %d cols: %s", termW, view)
			}
			for _, want := range []string{"⚠", "MR is not ready", "Resolve blocked task MRs", "[Esc/n] cancel"} {
				if !strings.Contains(view, want) {
					t.Fatalf("blocked overlay missing %q at %d cols: %s", want, termW, view)
				}
			}
		})
	}
}

func TestReleaseExecuteConfirmDialog_FinalOverlay_LongListScrollsToAction(t *testing.T) {
	rows := make([]task.ReleaseTaskMergeRow, 30)
	for i := range rows {
		rows[i] = task.ReleaseTaskMergeRow{
			ServiceName: "api", TaskID: "APP-1", Branch: "feature/APP-1",
			MRNumber: i + 1, HeadSHA: "abc12345", TargetBranch: "develop", TargetSHA: "def67890",
			Status: "pending", Ready: true,
		}
	}
	d := NewReleaseExecuteConfirmDialogWithTaskMerge("", []string{"APP-1"}, map[string]string{"api": "1.2.3"}, overlayTestPreview(), rows, 6)
	d.SetTerminalSize(110, 24)

	_, _ = d.Update(sendKey("G"))
	overlay := OverlayView(d.View(), 110, 24)
	assertOverlayFitsTerminal(t, overlay, 110, 24)
	if !strings.Contains(stripAnsi(overlay), "[Enter/y] execute [Esc/n] cancel") {
		t.Fatalf("scrolled overlay must show action hint: %s", stripAnsi(overlay))
	}
}

func TestReleaseExecuteConfirmDialog_FinalOverlay_WideGlyphRowsMeasureDisplayWidth(t *testing.T) {
	// Fixture rows: <=58 cells (fit at 120 cols) but >58 bytes, exposing byte-vs-cell divergence.
	scenarios := map[string]struct {
		service, target string
		wideRow         string
		compactRow      string
	}{
		"cjk": {
			service: "接口服务", target: "main",
			wideRow:    "接口服务 | APP-1 | !12 | abc12345 | main@def67890 | ready",
			compactRow: "接口服务/APP-1 !12 -> main ready",
		},
		"emoji": {
			service: "api🙂", target: "develop",
			wideRow:    "api🙂 | APP-1 | !12 | abc12345 | develop@def67890 | ready",
			compactRow: "api🙂/APP-1 !12 -> develop ready",
		},
	}
	for name, sc := range scenarios {
		t.Run(name, func(t *testing.T) {
			rows := overlayReadyRows()
			rows[0].ServiceName = sc.service
			for i := range rows {
				rows[i].TargetBranch = sc.target
			}
			at120 := NewReleaseExecuteConfirmDialogWithTaskMerge("", []string{"APP-1"}, map[string]string{"api": "1.2.3"}, overlayTestPreview(), rows, 7)
			at120.SetTerminalSize(120, 40)
			overlay := OverlayView(at120.View(), 120, 40)
			assertOverlayFitsTerminal(t, overlay, 120, 40)
			view := stripAnsi(overlay)
			if !strings.Contains(view, taskMergeTableHeader) || !strings.Contains(view, sc.wideRow) {
				t.Fatalf("row fits in cells at 120 cols; table must render unwrapped: %s", view)
			}

			at110 := NewReleaseExecuteConfirmDialogWithTaskMerge("", []string{"APP-1"}, map[string]string{"api": "1.2.3"}, overlayTestPreview(), rows, 7)
			at110.SetTerminalSize(110, 40)
			overlay = OverlayView(at110.View(), 110, 40)
			assertOverlayFitsTerminal(t, overlay, 110, 40)
			view = stripAnsi(overlay)
			if strings.Contains(view, taskMergeTableHeader) || !strings.Contains(view, sc.compactRow) {
				t.Fatalf("row exceeds 53-cell content at 110 cols; compact row must render: %s", view)
			}
		})
	}
}

func TestReleaseTaskMergeRetryConfirmDialog_FinalOverlay_CJKRowMeasuresDisplayWidth(t *testing.T) {
	rows := retryTestRows()
	rows[0].ServiceName = "接口服务"
	for i := range rows {
		rows[i].TargetBranch = "main"
	}
	// Widest row is 58 cells / 62 bytes: fits at 120 cols only under cell measurement.
	d := NewReleaseTaskMergeRetryConfirmDialog("rel-1", rows, 3)
	d.SetTerminalSize(120, 40)

	overlay := OverlayView(d.View(), 120, 40)
	assertOverlayFitsTerminal(t, overlay, 120, 40)
	view := stripAnsi(overlay)
	if !strings.Contains(view, "接口服务 | APP-1 | !12 | abc12345 | main@def67890 | merged") {
		t.Fatalf("CJK row fits in cells at 120 cols; table must render unwrapped: %s", view)
	}
}

func TestReleaseTaskMergeRetryConfirmDialog_FinalOverlay_LongListScrollsToAction(t *testing.T) {
	rows := make([]task.ReleaseTaskMergeRow, 30)
	for i := range rows {
		rows[i] = task.ReleaseTaskMergeRow{
			ServiceName: "api", TaskID: "APP-1", Branch: "feature/APP-1",
			MRNumber: i + 1, HeadSHA: "abc12345", TargetBranch: "develop", TargetSHA: "def67890",
			Status: "pending", Ready: true,
		}
	}
	d := NewReleaseTaskMergeRetryConfirmDialog("rel-1", rows, 3)
	d.SetTerminalSize(110, 24)

	_, _ = d.Update(sendKey("G"))
	overlay := OverlayView(d.View(), 110, 24)
	assertOverlayFitsTerminal(t, overlay, 110, 24)
	if !strings.Contains(stripAnsi(overlay), "[Enter/y] retry merges [Esc/n] cancel") {
		t.Fatalf("scrolled overlay must show retry hint: %s", stripAnsi(overlay))
	}
}

func TestReleaseTaskMergeRetryConfirmDialog_FinalOverlay_TransitionWidths(t *testing.T) {
	for _, termW := range []int{80, 100, 110, 119, 120} {
		t.Run(fmt.Sprintf("%d", termW), func(t *testing.T) {
			d := NewReleaseTaskMergeRetryConfirmDialog("rel-1", retryTestRows(), 3)
			d.SetTerminalSize(termW, 40)

			overlay := OverlayView(d.View(), termW, 40)
			assertOverlayFitsTerminal(t, overlay, termW, 40)
			view := stripAnsi(overlay)

			// Table mode is reserved for 120+ cols even when a row would fit narrower.
			if termW >= 120 {
				if !strings.Contains(view, taskMergeTableHeader) || !strings.Contains(view, wideWorkerRow) {
					t.Fatalf("table mode must render unwrapped at %d cols: %s", termW, view)
				}
			} else {
				if strings.Contains(view, taskMergeTableHeader) {
					t.Fatalf("table header must not render when rows exceed content width at %d cols: %s", termW, view)
				}
				if !strings.Contains(view, compactWorkerRow) {
					t.Fatalf("compact row missing at %d cols: %s", termW, view)
				}
			}
			if !strings.Contains(view, "[Enter/y] retry merges [Esc/n] cancel") {
				t.Fatalf("retry action must be visible at %d cols: %s", termW, view)
			}
		})
	}
}

func TestReleaseTaskMergeRetryConfirmDialog_FinalOverlay_BlockedExposesNoRetry(t *testing.T) {
	rows := retryTestRows()
	rows[1].Ready = false
	rows[1].Status = "unknown"
	rows[1].Blockers = []string{"target moved since failed attempt"}
	for _, termW := range []int{80, 110, 120} {
		t.Run(fmt.Sprintf("%d", termW), func(t *testing.T) {
			d := NewReleaseTaskMergeRetryConfirmDialog("rel-1", rows, 3)
			d.SetTerminalSize(termW, 40)

			overlay := OverlayView(d.View(), termW, 40)
			assertOverlayFitsTerminal(t, overlay, termW, 40)
			view := stripAnsi(overlay)
			if strings.Contains(view, "[Enter/y]") {
				t.Fatalf("blocked state must not expose retry action at %d cols: %s", termW, view)
			}
			for _, want := range []string{"⚠", "target moved since failed attempt", "[Esc/n] cancel"} {
				if !strings.Contains(view, want) {
					t.Fatalf("blocked overlay missing %q at %d cols: %s", want, termW, view)
				}
			}
		})
	}
}
