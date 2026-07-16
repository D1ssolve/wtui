package modal

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/task"
)

func TestReleaseExecuteConfirmDialog_ImplementsModal(t *testing.T) {
	var _ Modal = NewReleaseExecuteConfirmDialog(nil, nil, task.ReleasePreview{})
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
		"Stage 1: This will merge feature branches, create release branches, and push release branches if enabled.",
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
		d := NewReleaseExecuteConfirmDialog([]string{"FEAT-1"}, map[string]string{"api": "1.0.0"}, task.ReleasePreview{})
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
		d := NewReleaseExecuteConfirmDialog([]string{"FEAT-2"}, map[string]string{"worker": "2.0.0"}, task.ReleasePreview{})
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
		d := NewReleaseExecuteConfirmDialog([]string{"FEAT-1"}, map[string]string{"api": "1.0.0"}, task.ReleasePreview{})
		_, cmd := d.Update(sendSpecialKey(tea.KeyEsc))
		if cmd == nil {
			t.Fatal("esc must emit close cmd")
		}
		if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
			t.Fatalf("expected CloseModalMsg, got %T", execCmd(cmd))
		}
	})

	t.Run("n", func(t *testing.T) {
		d := NewReleaseExecuteConfirmDialog([]string{"FEAT-1"}, map[string]string{"api": "1.0.0"}, task.ReleasePreview{})
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
	d := NewReleaseExecuteConfirmDialog([]string{"FEAT-1"}, map[string]string{"api": "1.2.3"}, preview)

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
	d := NewReleaseExecuteConfirmDialog([]string{"FEAT-1"}, map[string]string{"api": "1.2.3", "worker": "2.0.0"}, preview)

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
