package modal

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// TestCaptureReleaseConfirmOverlays dumps full final overlays (ANSI + stripped +
// per-line widths) for visual QA. Runs only when WTUI_CAPTURE_DIR is set.
// Limitation: tests have no TTY, so lipgloss defaults to the Ascii profile and
// emits no style bytes. The TrueColor profile is forced for the duration of
// this test only; hue fidelity against a real terminal theme is not proven,
// only that style sequences are emitted and geometry is intact.
func TestCaptureReleaseConfirmOverlays(t *testing.T) {
	dir := os.Getenv("WTUI_CAPTURE_DIR")
	if dir == "" {
		t.Skip("WTUI_CAPTURE_DIR not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(previous)

	blockedRows := taskMergeTestRows()
	scenarios := map[string]func(termW, termH int) string{
		"create-ready": func(termW, termH int) string {
			d := NewReleaseExecuteConfirmDialogWithTaskMerge("", []string{"APP-1", "APP-2"}, map[string]string{"api": "1.2.3"}, overlayTestPreview(), overlayReadyRows(), 7)
			d.SetTerminalSize(termW, termH)
			return OverlayView(d.View(), termW, termH)
		},
		"create-blocked": func(termW, termH int) string {
			d := NewReleaseExecuteConfirmDialogWithTaskMerge("", []string{"APP-1", "APP-2"}, map[string]string{"api": "1.2.3"}, overlayTestPreview(), blockedRows, 4)
			d.SetTerminalSize(termW, termH)
			return OverlayView(d.View(), termW, termH)
		},
		"retry-ready": func(termW, termH int) string {
			d := NewReleaseTaskMergeRetryConfirmDialog("rel-1", retryTestRows(), 3)
			d.SetTerminalSize(termW, termH)
			return OverlayView(d.View(), termW, termH)
		},
	}

	for _, termW := range []int{80, 110, 120} {
		for name, render := range scenarios {
			view := render(termW, 40)
			base := filepath.Join(dir, fmt.Sprintf("%s_%dcol", name, termW))
			if !bytes.Contains([]byte(view), []byte{0x1b}) {
				t.Fatalf("%s %dcol: .ansi capture contains no ESC bytes", name, termW)
			}
			if err := os.WriteFile(base+".ansi", []byte(view), 0o644); err != nil {
				t.Fatal(err)
			}
			plain := stripAnsi(view)
			if bytes.Contains([]byte(plain), []byte{0x1b}) {
				t.Fatalf("%s %dcol: .txt capture still contains ESC bytes", name, termW)
			}
			if err := os.WriteFile(base+".txt", []byte(plain), 0o644); err != nil {
				t.Fatal(err)
			}
			var meta strings.Builder
			fmt.Fprintf(&meta, "scenario=%s terminal=%dx40\n", name, termW)
			for i, line := range strings.Split(plain, "\n") {
				fmt.Fprintf(&meta, "line %02d width %d: %s\n", i, ansi.StringWidth(line), strings.TrimRight(line, " "))
			}
			if err := os.WriteFile(base+".meta.txt", []byte(meta.String()), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}
