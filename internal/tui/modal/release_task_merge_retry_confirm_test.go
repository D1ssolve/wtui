package modal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/D1ssolve/wtui/internal/task"
)

func retryTestRows() []task.ReleaseTaskMergeRow {
	return []task.ReleaseTaskMergeRow{
		{
			ServiceName: "api", TaskID: "APP-1", Branch: "feature/APP-1",
			MRNumber: 12, HeadSHA: "abc12345", TargetBranch: "develop", TargetSHA: "def67890",
			Status: "merged", Ready: true,
		},
		{
			ServiceName: "worker", TaskID: "APP-2", Branch: "feature/APP-2",
			MRNumber: 7, HeadSHA: "11112222", TargetBranch: "develop", TargetSHA: "33334444",
			Status: "pending", Ready: true,
		},
	}
}

func TestReleaseTaskMergeRetryConfirmDialog_ImplementsModal(t *testing.T) {
	var _ Modal = NewReleaseTaskMergeRetryConfirmDialog("rel-1", nil, 1)
}

func TestReleaseTaskMergeRetryConfirmDialog_RendersReleaseAndRows(t *testing.T) {
	d := NewReleaseTaskMergeRetryConfirmDialog("rel-2026-09-21", retryTestRows(), 2)
	d.SetTerminalSize(120, 40)

	view := stripAnsi(d.View())
	for _, want := range []string{
		"rel-2026-09-21",
		"api | APP-1 | !12 | abc12345 | develop@def67890 | merged",
		"worker | APP-2 | !7 | 11112222 | develop@33334444 | ready",
		"[Enter/y] retry merges [Esc/n] cancel",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q: %s", want, view)
		}
	}
}

func TestReleaseTaskMergeRetryConfirmDialog_ConfirmEmitsGeneration(t *testing.T) {
	d := NewReleaseTaskMergeRetryConfirmDialog("rel-1", retryTestRows(), 11)
	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter must emit confirm cmd when all rows ready")
	}
	msg, ok := execCmd(cmd).(ConfirmReleaseTaskMergeRetryMsg)
	if !ok {
		t.Fatalf("expected ConfirmReleaseTaskMergeRetryMsg, got %T", execCmd(cmd))
	}
	if msg.ReleaseID != "rel-1" || msg.Generation != 11 {
		t.Fatalf("msg = %+v, want release rel-1 generation 11", msg)
	}
}

func TestReleaseTaskMergeRetryConfirmDialog_NotReadyRowsDisableConfirm(t *testing.T) {
	rows := retryTestRows()
	rows[1].Ready = false
	rows[1].Status = "unknown"
	rows[1].Blockers = []string{"target moved since failed attempt"}
	d := NewReleaseTaskMergeRetryConfirmDialog("rel-1", rows, 3)
	d.SetTerminalSize(120, 40)

	view := stripAnsi(d.View())
	if !strings.Contains(view, "target moved since failed attempt") {
		t.Fatalf("view missing blocker: %s", view)
	}
	if strings.Contains(view, "[Enter/y] retry") {
		t.Fatalf("view must omit retry hint when a row is not ready: %s", view)
	}
	if _, cmd := d.Update(sendSpecialKey(tea.KeyEnter)); cmd != nil {
		t.Fatal("enter must not emit command when a row is not ready")
	}
	if _, cmd := d.Update(sendKey("y")); cmd != nil {
		t.Fatal("y must not emit command when a row is not ready")
	}
}

func TestReleaseTaskMergeRetryConfirmDialog_NarrowWidthWrapsCriticalText(t *testing.T) {
	d := NewReleaseTaskMergeRetryConfirmDialog("rel-1", retryTestRows(), 3)
	d.SetTerminalSize(80, 40)

	view := stripAnsi(d.View())
	if !strings.Contains(strings.Join(strings.Fields(view), " "), "continue the release prepare.") {
		t.Fatalf("narrow view clips retry warning: %s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if width := ansi.StringWidth(line); width > d.viewport.Width {
			t.Fatalf("line width = %d, want <= %d: %q", width, d.viewport.Width, line)
		}
	}
}

func TestReleaseTaskMergeRetryConfirmDialog_EscCloses(t *testing.T) {
	d := NewReleaseTaskMergeRetryConfirmDialog("rel-1", retryTestRows(), 1)
	_, cmd := d.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("esc must emit close cmd")
	}
	if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", execCmd(cmd))
	}
}
