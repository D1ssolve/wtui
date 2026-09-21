package modal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestMRCreateConfirmDialog_EnterSubmitsForceCreate(t *testing.T) {
	d := NewMRCreateConfirmDialog("IN-1", "My title", []MRCreateConfirmItem{{ServiceName: "api", Reason: "MR #3 was closed"}})
	view := stripAnsi(d.View())
	if !strings.Contains(view, "api: MR #3 was closed") {
		t.Fatalf("view missing reason: %q", view)
	}
	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter produced no command")
	}
	msg, ok := cmd().(ForgeConfirmCreateMRMsg)
	if !ok || msg.TaskID != "IN-1" || msg.Title != "My title" {
		t.Fatalf("msg = %#v", msg)
	}
	_, cmd = d.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if _, ok := cmd().(CloseModalMsg); !ok {
		t.Fatalf("esc msg = %#v", cmd())
	}
}
