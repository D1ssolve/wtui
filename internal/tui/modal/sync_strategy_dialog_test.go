package modal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/diss0x/wtui/internal/task"
)

func TestSyncStrategyDialog_DefaultSelectionIsMerge(t *testing.T) {
	d := NewSyncStrategyDialog("IN-1234")

	if d.selectedIndex != 0 {
		t.Errorf("expected default selectedIndex=0 (Merge), got %d", d.selectedIndex)
	}

	if strategyOptions[0].strategy != task.SyncStrategyMerge {
		t.Errorf("expected strategy at index 0 to be SyncStrategyMerge, got %v", strategyOptions[0].strategy)
	}
}

func TestSyncStrategyDialog_OptionsContainMergeRebaseCancelNoReset(t *testing.T) {
	if len(strategyOptions) != 3 {
		t.Fatalf("expected 3 sync strategy options, got %d", len(strategyOptions))
	}

	want := []struct {
		name     string
		strategy task.SyncStrategy
	}{
		{"Merge", task.SyncStrategyMerge},
		{"Rebase", task.SyncStrategyRebase},
		{"Cancel", task.SyncStrategyNoop},
	}

	for i, opt := range want {
		if strategyOptions[i].name != opt.name {
			t.Errorf("option %d name = %q, want %q", i, strategyOptions[i].name, opt.name)
		}
		if strategyOptions[i].strategy != opt.strategy {
			t.Errorf("option %d strategy = %v, want %v", i, strategyOptions[i].strategy, opt.strategy)
		}
	}

	view := stripAnsi(NewSyncStrategyDialog("IN-1234").View())
	if strings.Contains(strings.ToLower(view), "reset") {
		t.Fatalf("sync strategy dialog must not mention reset, got:\n%s", view)
	}
}

func TestSyncStrategyDialog_J_NavigatesDown(t *testing.T) {
	d := NewSyncStrategyDialog("IN-1234")

	if d.selectedIndex != 0 {
		t.Fatalf("expected initial selectedIndex=0, got %d", d.selectedIndex)
	}

	modal, _ := d.Update(sendKey("j"))
	d = modal.(*SyncStrategyDialog)

	if d.selectedIndex != 1 {
		t.Errorf("'j' should move selection down to index 1, got %d", d.selectedIndex)
	}
}

func TestSyncStrategyDialog_K_NavigatesUp(t *testing.T) {
	d := NewSyncStrategyDialog("IN-1234")

	d.selectedIndex = 1

	modal, _ := d.Update(sendKey("k"))
	d = modal.(*SyncStrategyDialog)

	if d.selectedIndex != 0 {
		t.Errorf("'k' should move selection up to index 0, got %d", d.selectedIndex)
	}
}

func TestSyncStrategyDialog_J_WrapsToTop(t *testing.T) {
	d := NewSyncStrategyDialog("IN-1234")

	d.selectedIndex = len(strategyOptions) - 1

	modal, _ := d.Update(sendKey("j"))
	d = modal.(*SyncStrategyDialog)

	if d.selectedIndex != 0 {
		t.Errorf("'j' at bottom should wrap to index 0, got %d", d.selectedIndex)
	}
}

func TestSyncStrategyDialog_K_WrapsToBottom(t *testing.T) {
	d := NewSyncStrategyDialog("IN-1234")

	if d.selectedIndex != 0 {
		t.Fatalf("expected initial selectedIndex=0, got %d", d.selectedIndex)
	}

	modal, _ := d.Update(sendKey("k"))
	d = modal.(*SyncStrategyDialog)

	expectedBottom := len(strategyOptions) - 1
	if d.selectedIndex != expectedBottom {
		t.Errorf("'k' at top should wrap to index %d, got %d", expectedBottom, d.selectedIndex)
	}
}

func TestSyncStrategyDialog_DownArrow_NavigatesDown(t *testing.T) {
	d := NewSyncStrategyDialog("IN-1234")

	if d.selectedIndex != 0 {
		t.Fatalf("expected initial selectedIndex=0, got %d", d.selectedIndex)
	}

	modal, _ := d.Update(sendSpecialKey(tea.KeyDown))
	d = modal.(*SyncStrategyDialog)

	if d.selectedIndex != 1 {
		t.Errorf("down arrow should move selection to index 1, got %d", d.selectedIndex)
	}
}

func TestSyncStrategyDialog_UpArrow_NavigatesUp(t *testing.T) {
	d := NewSyncStrategyDialog("IN-1234")

	d.selectedIndex = 1

	modal, _ := d.Update(sendSpecialKey(tea.KeyUp))
	d = modal.(*SyncStrategyDialog)

	if d.selectedIndex != 0 {
		t.Errorf("up arrow should move selection to index 0, got %d", d.selectedIndex)
	}
}

func TestSyncStrategyDialog_DownArrow_WrapsToTop(t *testing.T) {
	d := NewSyncStrategyDialog("IN-1234")

	d.selectedIndex = len(strategyOptions) - 1

	modal, _ := d.Update(sendSpecialKey(tea.KeyDown))
	d = modal.(*SyncStrategyDialog)

	if d.selectedIndex != 0 {
		t.Errorf("down arrow at bottom should wrap to index 0, got %d", d.selectedIndex)
	}
}

func TestSyncStrategyDialog_UpArrow_WrapsToBottom(t *testing.T) {
	d := NewSyncStrategyDialog("IN-1234")

	if d.selectedIndex != 0 {
		t.Fatalf("expected initial selectedIndex=0, got %d", d.selectedIndex)
	}

	modal, _ := d.Update(sendSpecialKey(tea.KeyUp))
	d = modal.(*SyncStrategyDialog)

	expectedBottom := len(strategyOptions) - 1
	if d.selectedIndex != expectedBottom {
		t.Errorf("up arrow at top should wrap to index %d, got %d", expectedBottom, d.selectedIndex)
	}
}

func TestSyncStrategyDialog_Enter_EmitsSubmitSyncStrategyMsg(t *testing.T) {
	d := NewSyncStrategyDialog("IN-1234")

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("Enter must return a cmd")
	}

	msg := execCmd(cmd)
	sub, ok := msg.(SubmitSyncStrategyMsg)
	if !ok {
		t.Fatalf("expected SubmitSyncStrategyMsg, got %T", msg)
	}

	if sub.TaskID != "IN-1234" {
		t.Errorf("TaskID: expected IN-1234, got %q", sub.TaskID)
	}

	if sub.Strategy != task.SyncStrategyMerge {
		t.Errorf("Strategy: expected SyncStrategyMerge, got %v", sub.Strategy)
	}
}

func TestSyncStrategyDialog_Enter_EmitsCorrectStrategy_WhenRebaseSelected(t *testing.T) {
	d := NewSyncStrategyDialog("IN-5678")

	d.selectedIndex = 1

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("Enter must return a cmd")
	}

	msg := execCmd(cmd)
	sub, ok := msg.(SubmitSyncStrategyMsg)
	if !ok {
		t.Fatalf("expected SubmitSyncStrategyMsg, got %T", msg)
	}

	if sub.TaskID != "IN-5678" {
		t.Errorf("TaskID: expected IN-5678, got %q", sub.TaskID)
	}

	if sub.Strategy != task.SyncStrategyRebase {
		t.Errorf("Strategy: expected SyncStrategyRebase, got %v", sub.Strategy)
	}
}

func TestSyncStrategyDialog_Esc_EmitsCloseModalMsg(t *testing.T) {
	d := NewSyncStrategyDialog("IN-1234")

	_, cmd := d.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("Esc must return a cmd")
	}

	msg := execCmd(cmd)
	if _, ok := msg.(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", msg)
	}
}

func TestSyncStrategyDialog_Title(t *testing.T) {
	d := NewSyncStrategyDialog("IN-9999")

	title := d.Title()
	if title != "Sync Strategy" {
		t.Errorf("Title: expected 'Sync Strategy', got %q", title)
	}
}

func TestSyncStrategyDialog_View_ContainsExpectedContent(t *testing.T) {
	d := NewSyncStrategyDialog("IN-1234")
	view := stripAnsi(d.View())

	if !strings.Contains(view, "IN-1234") {
		t.Error("View should contain task ID")
	}

	if !strings.Contains(view, "Merge") {
		t.Error("View should contain 'Merge'")
	}
	if !strings.Contains(view, "Rebase") {
		t.Error("View should contain 'Rebase'")
	}

	if !strings.Contains(view, "safer") {
		t.Error("View should contain Merge description hint")
	}
	if !strings.Contains(view, "cleaner history") {
		t.Error("View should contain Rebase description hint")
	}

	if !strings.Contains(view, "j/k") {
		t.Error("View should contain 'j/k' navigation hint")
	}
	if !strings.Contains(view, "Enter") {
		t.Error("View should contain 'Enter' hint")
	}
	if !strings.Contains(view, "Esc") {
		t.Error("View should contain 'Esc' hint")
	}
}

func TestSyncStrategyDialog_View_ShowsSelectionIndicator(t *testing.T) {
	d := NewSyncStrategyDialog("IN-1234")

	view := d.View()

	if !strings.Contains(view, "◉") {
		t.Error("View should contain selected indicator '◉'")
	}
	if !strings.Contains(view, "○") {
		t.Error("View should contain unselected indicator '○'")
	}
}

func TestSyncStrategyDialog_SetTerminalSize(t *testing.T) {
	d := NewSyncStrategyDialog("IN-1234")

	d.SetTerminalSize(120, 40)

	if d.terminalWidth != 120 {
		t.Errorf("terminalWidth: expected 120, got %d", d.terminalWidth)
	}
	if d.terminalHeight != 40 {
		t.Errorf("terminalHeight: expected 40, got %d", d.terminalHeight)
	}
}

func TestSyncStrategyDialog_UnknownKey_DoesNothing(t *testing.T) {
	d := NewSyncStrategyDialog("IN-1234")

	modal, cmd := d.Update(sendKey("x"))
	d = modal.(*SyncStrategyDialog)

	if cmd != nil {
		t.Error("Unknown key should return nil cmd")
	}
	if d.selectedIndex != 0 {
		t.Errorf("Unknown key should not change selection, got %d", d.selectedIndex)
	}
}

func TestSyncStrategyDialog_MultipleNavigationSteps(t *testing.T) {
	d := NewSyncStrategyDialog("IN-1234")

	modal, _ := d.Update(sendKey("j"))
	d = modal.(*SyncStrategyDialog)
	if d.selectedIndex != 1 {
		t.Errorf("after first 'j': expected index 1, got %d", d.selectedIndex)
	}

	modal, _ = d.Update(sendKey("j"))
	d = modal.(*SyncStrategyDialog)
	if d.selectedIndex != 2 {
		t.Errorf("after second 'j': expected index 2, got %d", d.selectedIndex)
	}

	modal, _ = d.Update(sendKey("j"))
	d = modal.(*SyncStrategyDialog)
	if d.selectedIndex != 0 {
		t.Errorf("after third 'j': expected index 0 (wrap), got %d", d.selectedIndex)
	}

	modal, _ = d.Update(sendKey("k"))
	d = modal.(*SyncStrategyDialog)
	if d.selectedIndex != 2 {
		t.Errorf("after 'k': expected index 2 (wrap), got %d", d.selectedIndex)
	}
}
