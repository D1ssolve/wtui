package modal

import (
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/gitflow"
	tea "github.com/charmbracelet/bubbletea"
)

func TestWorkflowApplyRequiresVisibleWarningAndExplicitYes(t *testing.T) {
	flow, _ := gitflow.Resolve(nil)
	draft, _ := gitflow.NewDraft("git-flow")
	b := newWorkflowBuilder(NewFlowOverlay(flow, FlowPosition{}), draft)
	d := NewWorkflowApplyConfirm(b, "/tmp/example/config.yaml", 2)
	d.SetTerminalSize(80, 24)
	if view := d.View(); !strings.Contains(view, "2 in-flight") || !strings.Contains(view, "config.yaml") {
		t.Fatalf("missing warning or target: %s", view)
	}
	if _, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatal("Enter must not write")
	}
	y := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}
	if _, cmd := d.Update(y); cmd == nil || cmd().(ConfirmWorkflowApplyMsg).Dialog != d {
		t.Fatal("missing explicit confirmation")
	}
	d.SetTerminalSize(30, 5)
	if _, cmd := d.Update(y); cmd != nil {
		t.Fatal("confirmation accepted while warning was hidden")
	}
	if m, _ := d.Update(tea.KeyMsg{Type: tea.KeyEsc}); m != b {
		t.Fatal("cancel lost draft")
	}
}
