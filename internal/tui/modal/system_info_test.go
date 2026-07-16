package modal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSystemInfoModal_Title(t *testing.T) {
	m := NewSystemInfoModal(true, true, false, "auto", "gitlab.com", "github.com", "git-flow")
	if got := m.Title(); got != "System Status" {
		t.Errorf("title = %q, want System Status", got)
	}
}

func TestSystemInfoModal_View_ShowsToolStatus(t *testing.T) {
	m := NewSystemInfoModal(true, true, false, "auto", "gitlab.com", "github.com", "git-flow")
	view := stripAnsi(m.View())

	for _, want := range []string{
		"System Status",
		"External Tools",
		"git",
		"lazygit",
		"glab",
		"gh",
		"Forge Config",
		"Git Flow",
		"Preset:",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q, got:\n%s", want, view)
		}
	}

	if !strings.Contains(view, "available") {
		t.Error("expected at least one tool marked available")
	}
}

func TestSystemInfoModal_EscCloses(t *testing.T) {
	m := NewSystemInfoModal(false, false, false, "auto", "gitlab.com", "github.com", "")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc must return cmd")
	}
	msg := execCmd(cmd)
	if _, ok := msg.(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", msg)
	}
}

func TestSystemInfoModal_DotCloses(t *testing.T) {
	m := NewSystemInfoModal(false, false, false, "auto", "gitlab.com", "github.com", "")
	_, cmd := m.Update(sendKey("."))
	if cmd == nil {
		t.Fatal(". must return cmd")
	}
	msg := execCmd(cmd)
	if _, ok := msg.(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", msg)
	}
}
