package modal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
)

func TestReleaseFinishConfirmDialog_ImplementsModal(t *testing.T) {
	var _ Modal = NewReleaseFinishConfirmDialog("rel-1", domain.Release{}, nil)
}

func TestReleaseFinishConfirmDialog_ViewShowsDetails(t *testing.T) {
	pushTags := true
	d := NewReleaseFinishConfirmDialog(
		"rel-2026.01.01-001",
		domain.Release{
			Services: []domain.ReleaseService{
				{Name: "api", Version: "1.2.3", Tag: "release-1.2.3"},
				{Name: "worker", Version: "2.0.0", Tag: "release-2.0.0"},
			},
		},
		&config.Config{
			Release: &config.ReleaseConfig{
				PushTags: &pushTags,
			},
		},
	)

	view := stripAnsi(d.View())
	for _, want := range []string{
		"Confirm Finish Release",
		"Release ID: rel-2026.01.01-001",
		"Service | Version | Tag",
		"api | 1.2.3 | release-1.2.3",
		"worker | 2.0.0 | release-2.0.0",
		"Push tags: true",
		"Warning: creating and pushing annotated tags; cannot be undone; run only after regression.",
		"[Enter/y] finish  [Esc/n] cancel",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q: %s", want, view)
		}
	}
}

func TestReleaseFinishConfirmDialog_EnterAndYEmitConfirmMessage(t *testing.T) {
	release := domain.Release{ID: "rel-1"}

	t.Run("enter", func(t *testing.T) {
		d := NewReleaseFinishConfirmDialog("rel-1", release, nil)
		_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
		if cmd == nil {
			t.Fatal("enter must emit confirm cmd")
		}

		msg := execCmd(cmd)
		confirm, ok := msg.(ConfirmFinishReleaseMsg)
		if !ok {
			t.Fatalf("expected ConfirmFinishReleaseMsg, got %T", msg)
		}
		if confirm.ReleaseID != "rel-1" {
			t.Fatalf("unexpected release id: %s", confirm.ReleaseID)
		}
	})

	t.Run("y", func(t *testing.T) {
		d := NewReleaseFinishConfirmDialog("rel-2", release, nil)
		_, cmd := d.Update(sendKey("y"))
		if cmd == nil {
			t.Fatal("y must emit confirm cmd")
		}

		msg := execCmd(cmd)
		confirm, ok := msg.(ConfirmFinishReleaseMsg)
		if !ok {
			t.Fatalf("expected ConfirmFinishReleaseMsg, got %T", msg)
		}
		if confirm.ReleaseID != "rel-2" {
			t.Fatalf("unexpected release id: %s", confirm.ReleaseID)
		}
	})
}

func TestReleaseFinishConfirmDialog_EscAndNClose(t *testing.T) {
	t.Run("esc", func(t *testing.T) {
		d := NewReleaseFinishConfirmDialog("rel-1", domain.Release{ID: "rel-1"}, nil)
		_, cmd := d.Update(sendSpecialKey(tea.KeyEsc))
		if cmd == nil {
			t.Fatal("esc must emit close cmd")
		}

		if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
			t.Fatalf("expected CloseModalMsg, got %T", execCmd(cmd))
		}
	})

	t.Run("n", func(t *testing.T) {
		d := NewReleaseFinishConfirmDialog("rel-1", domain.Release{ID: "rel-1"}, nil)
		_, cmd := d.Update(sendKey("n"))
		if cmd == nil {
			t.Fatal("n must emit close cmd")
		}

		if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
			t.Fatalf("expected CloseModalMsg, got %T", execCmd(cmd))
		}
	})
}
