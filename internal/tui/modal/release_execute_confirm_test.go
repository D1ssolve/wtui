package modal

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/task"
)

func TestReleaseExecuteConfirmDialog_ImplementsModal(t *testing.T) {
	var _ Modal = NewReleaseExecuteConfirmDialog("", nil, nil, task.ReleasePreview{})
}

func TestReleaseExecuteConfirmDialog_ViewShowsDetails(t *testing.T) {
	pushIntegration := true
	pushReleaseBranches := false
	pushTags := true

	cfg := config.Config{
		Tag: &config.TagConfig{Format: "release-{{.Version}}"},
		GitFlow: &config.GitFlowConfig{
			Preset: "git-flow",
			BranchTypes: map[string]config.BranchTypeRule{
				"release": {Prefixes: []string{"rel/"}},
			},
		},
		Release: &config.ReleaseConfig{
			PushIntegration:     &pushIntegration,
			PushReleaseBranches: &pushReleaseBranches,
			PushTags:            &pushTags,
		},
	}

	preview, err := task.BuildReleasePreview(cfg, map[string]string{"worker": "2.0.0", "api": "1.2.3"})
	if err != nil {
		t.Fatalf("BuildReleasePreview() error = %v", err)
	}

	d := NewReleaseExecuteConfirmDialog(
		"",
		[]string{"FEAT-2", "FEAT-1"},
		map[string]string{"worker": "2.0.0", "api": "1.2.3"},
		preview,
	)

	view := stripAnsi(d.View())
	for _, want := range []string{
		"Release Execute Confirmation",
		"Selected tasks: FEAT-1, FEAT-2",
		"Service | Version | Release Branch | Tag",
		"api | 1.2.3 | rel/1.2.3 | release-1.2.3",
		"worker | 2.0.0 | rel/2.0.0 | release-2.0.0",
		"Push settings:",
		"integration branch: develop",
		"push integration: true",
		"push release branches: false",
		"push tags: true",
		"Stage 1: This will verify feature branches are merged into the integration branch, create release branches, and push release branches if enabled.",
		"Tags are NOT created yet. Use \"Finish Release\" after regression testing.",
		"[Enter/y] execute [Esc/n] cancel",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q: %s", want, view)
		}
	}
}

func TestReleaseExecuteConfirmDialog_EnterAndYEmitConfirmMessage(t *testing.T) {
	t.Run("enter", func(t *testing.T) {
		d := NewReleaseExecuteConfirmDialog("", []string{"FEAT-1"}, map[string]string{"api": "1.0.0"}, task.ReleasePreview{})
		_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
		if cmd == nil {
			t.Fatal("enter must emit confirm cmd")
		}
		msg := execCmd(cmd)
		confirm, ok := msg.(ConfirmReleaseExecuteMsg)
		if !ok {
			t.Fatalf("expected ConfirmReleaseExecuteMsg, got %T", msg)
		}
		if len(confirm.TaskIDs) != 1 || confirm.TaskIDs[0] != "FEAT-1" {
			t.Fatalf("unexpected task ids: %+v", confirm.TaskIDs)
		}
		if confirm.Versions["api"] != "1.0.0" {
			t.Fatalf("unexpected versions: %+v", confirm.Versions)
		}
	})

	t.Run("y", func(t *testing.T) {
		d := NewReleaseExecuteConfirmDialog("", []string{"FEAT-2"}, map[string]string{"worker": "2.0.0"}, task.ReleasePreview{})
		_, cmd := d.Update(sendKey("y"))
		if cmd == nil {
			t.Fatal("y must emit confirm cmd")
		}
		if _, ok := execCmd(cmd).(ConfirmReleaseExecuteMsg); !ok {
			t.Fatalf("expected ConfirmReleaseExecuteMsg, got %T", execCmd(cmd))
		}
	})
}

func TestReleaseExecuteConfirmDialog_EscAndNClose(t *testing.T) {
	t.Run("esc", func(t *testing.T) {
		d := NewReleaseExecuteConfirmDialog("", []string{"FEAT-1"}, map[string]string{"api": "1.0.0"}, task.ReleasePreview{})
		_, cmd := d.Update(sendSpecialKey(tea.KeyEsc))
		if cmd == nil {
			t.Fatal("esc must emit close cmd")
		}
		if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
			t.Fatalf("expected CloseModalMsg, got %T", execCmd(cmd))
		}
	})

	t.Run("n", func(t *testing.T) {
		d := NewReleaseExecuteConfirmDialog("", []string{"FEAT-1"}, map[string]string{"api": "1.0.0"}, task.ReleasePreview{})
		_, cmd := d.Update(sendKey("n"))
		if cmd == nil {
			t.Fatal("n must emit close cmd")
		}
		if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
			t.Fatalf("expected CloseModalMsg, got %T", execCmd(cmd))
		}
	})
}

func TestReleaseExecuteConfirmDialog_WithPreviewError_DisablesConfirm(t *testing.T) {
	preview := task.ReleasePreview{Err: errors.New("bad preview")}
	d := NewReleaseExecuteConfirmDialog("", []string{"FEAT-1"}, map[string]string{"api": "1.2.3"}, preview)

	view := stripAnsi(d.View())
	if !strings.Contains(view, "Cannot preview release") {
		t.Fatalf("view should show preview error, got: %s", view)
	}
	if strings.Contains(view, "[Enter/y] execute") {
		t.Fatalf("view should not show execute prompt on preview error")
	}

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd != nil {
		t.Fatal("enter must not emit command when preview error")
	}
	_, cmd = d.Update(sendKey("y"))
	if cmd != nil {
		t.Fatal("y must not emit command when preview error")
	}
	_, cmd = d.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("esc must emit close command when preview error")
	}
	if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", execCmd(cmd))
	}
}

func TestReleaseExecuteConfirmDialog_WithPreviewRows_RendersProvidedRows(t *testing.T) {
	preview := task.ReleasePreview{
		Rows: []task.ReleasePreviewRow{
			{ServiceName: "api", Version: "1.2.3", ReleaseBranch: "rel/1.2.3", Tag: "v1.2.3"},
			{ServiceName: "worker", Version: "2.0.0", ReleaseBranch: "rel/2.0.0", Tag: "v2.0.0"},
		},
		IntegrationBranch:   "develop",
		PushIntegration:     true,
		PushReleaseBranches: false,
		PushTags:            true,
	}
	d := NewReleaseExecuteConfirmDialog("", []string{"FEAT-1"}, map[string]string{"api": "1.2.3", "worker": "2.0.0"}, preview)

	view := stripAnsi(d.View())
	for _, want := range []string{
		"api | 1.2.3 | rel/1.2.3 | v1.2.3",
		"worker | 2.0.0 | rel/2.0.0 | v2.0.0",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q: %s", want, view)
		}
	}
}

func TestReleaseExecuteConfirmDialog_ConfirmsTitleAndMultilineTagDescriptions(t *testing.T) {
	preview := task.ReleasePreview{Rows: []task.ReleasePreviewRow{{
		ServiceName: "api", Version: "1.2.3", Tag: "v1.2.3", TagDescription: "Summary\nDetails",
	}}}
	d := NewReleaseExecuteConfirmDialog("August release", []string{"FEAT-1"}, map[string]string{"api": "1.2.3"}, preview)

	view := stripAnsi(d.View())
	for _, want := range []string{"August release", "Summary", "Details"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q: %s", want, view)
		}
	}
	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	confirm := execCmd(cmd).(ConfirmReleaseExecuteMsg)
	if confirm.Title != "August release" {
		t.Fatalf("title = %q", confirm.Title)
	}
	if confirm.TagDescriptions["api"] != "Summary\nDetails" {
		t.Fatalf("tag descriptions = %#v", confirm.TagDescriptions)
	}
}

func TestReleaseExecuteConfirmDialog_ScrollsLongPreviewWithinTerminal(t *testing.T) {
	rows := make([]task.ReleasePreviewRow, 20)
	for i := range rows {
		rows[i] = task.ReleasePreviewRow{ServiceName: "service", Version: "1.0.0", ReleaseBranch: "release/1.0.0", Tag: "v1.0.0"}
	}
	d := NewReleaseExecuteConfirmDialog("", []string{"FEAT-1"}, map[string]string{"service": "1.0.0"}, task.ReleasePreview{Rows: rows})
	d.SetTerminalSize(80, 24)

	if got := lipgloss.Height(d.View()); got > 15 {
		t.Fatalf("view height = %d, want <= 15", got)
	}

	_, _ = d.Update(sendKey("G"))
	if !strings.Contains(stripAnsi(d.View()), "[Enter/y] execute [Esc/n] cancel") {
		t.Fatal("bottom hint must be visible after scrolling to end")
	}
}

func taskMergeTestRows() []task.ReleaseTaskMergeRow {
	return []task.ReleaseTaskMergeRow{
		{
			ServiceName: "api", TaskID: "APP-1", Branch: "feature/APP-1",
			MRNumber: 12, MRURL: "https://gitlab.example.com/api/-/merge_requests/12",
			HeadSHA: "abc12345deadbeef", TargetBranch: "develop", TargetSHA: "def67890cafe",
			Status: "pending", Ready: true,
		},
		{
			ServiceName: "worker", TaskID: "APP-2", Branch: "feature/APP-2",
			MRNumber: 7, HeadSHA: "11112222aaaa", TargetBranch: "develop", TargetSHA: "33334444bbbb",
			Status: "pending", Ready: false, Blockers: []string{"MR is not ready"},
		},
	}
}

func TestReleaseExecuteConfirmDialog_TaskMergeRowsRenderUnderReleaseRows(t *testing.T) {
	preview := task.ReleasePreview{
		Rows: []task.ReleasePreviewRow{{ServiceName: "api", Version: "1.2.3", ReleaseBranch: "release/1.2.3", Tag: "v1.2.3"}},
	}
	rows := taskMergeTestRows()
	rows[1].Ready = true
	rows[1].Blockers = nil
	d := NewReleaseExecuteConfirmDialogWithTaskMerge("", []string{"APP-1", "APP-2"}, map[string]string{"api": "1.2.3"}, preview, rows, 3)
	d.SetTerminalSize(120, 40)

	view := stripAnsi(d.View())
	for _, want := range []string{
		"api | 1.2.3 | release/1.2.3 | v1.2.3",
		"Task MR merges:",
		"Service | Task | MR | Source | Target | Status",
		"api | APP-1 | !12 | abc12345 | develop@def67890 | ready",
		"worker | APP-2 | !7 | 11112222 | develop@33334444 | ready",
		"Tags are NOT created yet",
		"[Enter/y] execute [Esc/n] cancel",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q: %s", want, view)
		}
	}
	if !strings.Contains(strings.Join(strings.Fields(view), " "), "merge confirmed ready reviews sequentially") {
		t.Fatalf("view clips stage-1 warning: %s", view)
	}
}

func TestReleaseExecuteConfirmDialog_TaskMergeBlockedDisablesConfirm(t *testing.T) {
	preview := task.ReleasePreview{
		Rows: []task.ReleasePreviewRow{{ServiceName: "api", Version: "1.2.3", ReleaseBranch: "release/1.2.3", Tag: "v1.2.3"}},
	}
	d := NewReleaseExecuteConfirmDialogWithTaskMerge("", []string{"APP-1", "APP-2"}, map[string]string{"api": "1.2.3"}, preview, taskMergeTestRows(), 4)
	d.SetTerminalSize(120, 40)

	view := stripAnsi(d.View())
	if !strings.Contains(view, "MR is not ready") {
		t.Fatalf("view missing blocker: %s", view)
	}
	if strings.Contains(view, "[Enter/y] execute") {
		t.Fatalf("view must omit execute hint when a row is blocked: %s", view)
	}
	if _, cmd := d.Update(sendSpecialKey(tea.KeyEnter)); cmd != nil {
		t.Fatal("enter must not emit command when a task MR row is blocked")
	}
	if _, cmd := d.Update(sendKey("y")); cmd != nil {
		t.Fatal("y must not emit command when a task MR row is blocked")
	}
}

func TestReleaseExecuteConfirmDialog_TaskMergeConfirmCarriesGeneration(t *testing.T) {
	preview := task.ReleasePreview{
		Rows: []task.ReleasePreviewRow{{ServiceName: "api", Version: "1.2.3", ReleaseBranch: "release/1.2.3", Tag: "v1.2.3"}},
	}
	rows := taskMergeTestRows()
	rows[1].Ready = true
	rows[1].Blockers = nil
	d := NewReleaseExecuteConfirmDialogWithTaskMerge("", []string{"APP-1"}, map[string]string{"api": "1.2.3"}, preview, rows, 9)

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter must emit confirm cmd when all rows ready")
	}
	confirm, ok := execCmd(cmd).(ConfirmReleaseExecuteMsg)
	if !ok {
		t.Fatalf("expected ConfirmReleaseExecuteMsg, got %T", execCmd(cmd))
	}
	if confirm.Generation != 9 {
		t.Fatalf("confirm generation = %d, want 9", confirm.Generation)
	}
}

func TestReleaseExecuteConfirmDialog_TaskMergeNarrowWidthShowsBlockers(t *testing.T) {
	preview := task.ReleasePreview{
		Rows: []task.ReleasePreviewRow{{ServiceName: "api", Version: "1.2.3", ReleaseBranch: "release/1.2.3", Tag: "v1.2.3"}},
	}
	d := NewReleaseExecuteConfirmDialogWithTaskMerge("", []string{"APP-2"}, map[string]string{"api": "1.2.3"}, preview, taskMergeTestRows(), 5)
	d.SetTerminalSize(64, 30)

	view := stripAnsi(d.View())
	for _, want := range []string{"worker", "APP-2", "MR is not ready"} {
		if !strings.Contains(view, want) {
			t.Fatalf("narrow view missing %q: %s", want, view)
		}
	}
	if strings.Contains(view, "[Enter/y] execute") {
		t.Fatalf("narrow view must omit execute hint when blocked: %s", view)
	}
}

func TestReleaseExecuteConfirmDialog_TaskMergeNarrowWidthWrapsCriticalText(t *testing.T) {
	preview := task.ReleasePreview{
		Rows: []task.ReleasePreviewRow{{ServiceName: "api", Version: "1.2.3", ReleaseBranch: "release/1.2.3", Tag: "v1.2.3"}},
	}
	d := NewReleaseExecuteConfirmDialogWithTaskMerge("", []string{"APP-1"}, map[string]string{"api": "1.2.3"}, preview, taskMergeTestRows()[:1], 5)
	d.SetTerminalSize(80, 40)

	view := stripAnsi(d.View())
	if !strings.Contains(strings.Join(strings.Fields(view), " "), "push release branches if enabled.") {
		t.Fatalf("narrow view clips execution warning: %s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if width := ansi.StringWidth(line); width > d.viewport.Width {
			t.Fatalf("line width = %d, want <= %d: %q", width, d.viewport.Width, line)
		}
	}
}

func TestReleaseExecuteConfirmDialog_TaskMergeScrollsWithRows(t *testing.T) {
	preview := task.ReleasePreview{
		Rows: []task.ReleasePreviewRow{{ServiceName: "api", Version: "1.2.3", ReleaseBranch: "release/1.2.3", Tag: "v1.2.3"}},
	}
	rows := make([]task.ReleaseTaskMergeRow, 30)
	for i := range rows {
		rows[i] = task.ReleaseTaskMergeRow{
			ServiceName: "api", TaskID: "APP-1", Branch: "feature/APP-1",
			MRNumber: i + 1, HeadSHA: "abc12345", TargetBranch: "develop", TargetSHA: "def67890",
			Status: "pending", Ready: true,
		}
	}
	d := NewReleaseExecuteConfirmDialogWithTaskMerge("", []string{"APP-1"}, map[string]string{"api": "1.2.3"}, preview, rows, 6)
	d.SetTerminalSize(100, 24)

	if got := lipgloss.Height(d.View()); got > 15 {
		t.Fatalf("view height = %d, want <= 15", got)
	}
	_, _ = d.Update(sendKey("G"))
	if !strings.Contains(stripAnsi(d.View()), "[Enter/y] execute [Esc/n] cancel") {
		t.Fatal("bottom hint must be visible after scrolling to end")
	}
}
