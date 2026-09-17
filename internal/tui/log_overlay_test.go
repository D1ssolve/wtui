package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func appendLogText(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLogOverlay_ModesAndTaskFilterPreserveDetails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wtui.log")
	appendLogText(t, path, `{"time":"2026-09-15T10:00:00Z","level":"INFO","msg":"exec git","argv":["git","fetch","origin"],"task_id":"ITPR-570"}`+"\n"+
		`{"time":"2026-09-15T10:00:01Z","level":"ERROR","msg":"workflow failed","task_id":"ITPR-570","err":"Permission denied\nfatal: remote unavailable","stderr":"remote: rejected\ncheck access","exit_code":128,"detail":{"retry":false},"number":9007199254740993,"nullable":null}`+"\n"+
		`{"level":"INFO","msg":"other-task-event","task_id":"ITPR-571"}`+"\n"+
		`{"level":"WARN","msg":"global-event"}`+"\n")
	o := NewLogOverlay(path, 180, 60, "ITPR-570")
	view := ansi.Strip(o.viewport.View())
	if !strings.Contains(view, "$ git fetch origin") || strings.Contains(view, "workflow failed") {
		t.Fatalf("Commands mode = %s", view)
	}
	o.Update(tea.KeyMsg{Type: tea.KeyTab})
	view = ansi.Strip(o.viewport.View())
	for _, want := range []string{"ERROR", "workflow failed", "task_id", "ITPR-570", "Permission denied", "fatal: remote unavailable", "remote: rejected", "check access", "exit_code", "128", `{"retry":false}`, "9007199254740993", "nullable=null"} {
		if !strings.Contains(view, want) {
			t.Errorf("Application mode missing %q: %s", want, view)
		}
	}
	if strings.Contains(view, `\n`) || strings.Contains(view, "global-event") || strings.Contains(view, "other-task-event") {
		t.Fatalf("multiline decoding or task filter failed: %s", view)
	}
	o.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	view = ansi.Strip(o.viewport.View())
	if !strings.Contains(view, "global-event") || !strings.Contains(view, "other-task-event") {
		t.Fatalf("all-task mode = %s", view)
	}
	o.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	if strings.Contains(o.viewport.View(), "other-task-event") {
		t.Fatal("task filter was not restored")
	}
	o.Update(tea.KeyMsg{Type: tea.KeyTab})
	if strings.Contains(o.viewport.View(), "workflow failed") || !strings.Contains(o.viewport.View(), "git fetch origin") {
		t.Fatal("switching back did not restore Commands mode")
	}
}

func TestLogOverlay_WrapsLongUnicodeFieldsOnResize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wtui.log")
	text := strings.Repeat("界", 90) + "END-OF-ERROR"
	data, err := json.Marshal(map[string]string{"msg": "failed", "level": "ERROR", "err": text})
	if err != nil {
		t.Fatal(err)
	}
	appendLogText(t, path, string(data)+"\n")
	o := NewLogOverlay(path, 160, 70, "")
	o.Update(tea.KeyMsg{Type: tea.KeyTab})
	for _, width := range []int{160, 60, 100} {
		o.SetSize(width, 70)
		view := ansi.Strip(o.viewport.View())
		if strings.Count(view, "界") != 90 || !strings.Contains(strings.ReplaceAll(view, "\n", ""), "END-OF-ERROR") {
			t.Fatalf("width %d loses long error: %s", width, view)
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > o.viewport.Width {
				t.Fatalf("line exceeds viewport width %d: %q", o.viewport.Width, line)
			}
		}
		if lines := strings.Count(o.View(), "\n") + 1; lines > 70 {
			t.Fatalf("overlay exceeds terminal height: %d", lines)
		}
	}
}

func TestLogOverlay_ReadsCompletedLinesAndResetsAfterTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wtui.log")
	appendLogText(t, path, `{"msg":"exec git","argv":["git","fetch","a-long-remote-name"]}`+"\n"+`{"msg":"exec git","argv":["git","sta`)
	o := NewLogOverlay(path, 140, 40, "")
	appendLogText(t, path, `tus"]}`+"\n")
	o.Refresh()
	if view := o.viewport.View(); !strings.Contains(view, "git fetch") || !strings.Contains(view, "git status") {
		t.Fatalf("incomplete line was lost: %s", view)
	}
	if err := os.WriteFile(path, []byte(`{"msg":"exec git","argv":["git","new"]}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	o.Refresh()
	if view := o.viewport.View(); !strings.Contains(view, "git new") || strings.Contains(view, "git fetch") || strings.Contains(view, "git status") {
		t.Fatalf("truncation did not reset records: %s", view)
	}
}

func TestLogOverlay_ReportsReadFailureAndRecovers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.log")
	o := NewLogOverlay(path, 160, 40, "")
	if !strings.Contains(o.viewport.View(), "read logs") {
		t.Fatalf("missing file error is invisible: %s", o.viewport.View())
	}
	appendLogText(t, path, `{"msg":"exec git","argv":["git","recovered"]}`+"\n")
	o.Refresh()
	if view := o.viewport.View(); strings.Contains(view, "read logs") || !strings.Contains(view, "git recovered") {
		t.Fatalf("read error did not recover: %s", view)
	}
}

func TestLogOverlay_RefreshFollowsBottomButPreservesHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wtui.log")
	var data strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&data, "{\"msg\":\"exec git\",\"argv\":[\"git\",\"entry-%d\"]}\n", i)
	}
	appendLogText(t, path, data.String())
	o := NewLogOverlay(path, 140, 25, "")
	if !strings.Contains(o.viewport.View(), "entry-79") {
		t.Fatal("initial view should start at bottom")
	}
	o.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	before := o.viewport.View()
	appendLogText(t, path, `{"msg":"exec git","argv":["git","appended"]}`+"\n")
	o.Refresh()
	if o.viewport.View() != before {
		t.Fatal("refresh moved the user away from history")
	}
	o.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	appendLogText(t, path, `{"msg":"exec git","argv":["git","latest"]}`+"\n")
	o.Refresh()
	if !o.viewport.AtBottom() || !strings.Contains(o.viewport.View(), "git latest") {
		t.Fatal("bottom view did not follow appended record")
	}
}

func TestLogOverlay_UnavailableControlsAreExplicit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wtui.log")
	appendLogText(t, path, "")
	o := NewLogOverlay(path, 160, 40, "")
	o.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	o.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	view := ansi.Strip(o.View())
	if !strings.Contains(view, "unavailable") || !strings.Contains(view, "no task") {
		t.Fatalf("unavailable controls are not explained: %s", view)
	}
}
