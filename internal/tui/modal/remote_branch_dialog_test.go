package modal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/task"
)

func TestRemoteBranchConflictDialog_DefaultSelectionIsFetchAndSwitch(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-1234", "api-gateway", "feature/IN-1234", "/path/to/repo")

	if d.selectedIndex != 0 {
		t.Errorf("expected default selectedIndex=0 (Track Remote Branch), got %d", d.selectedIndex)
	}

	if remoteBranchOptions[0].strategy != task.StrategyFetchAndSwitch {
		t.Errorf("expected strategy at index 0 to be StrategyFetchAndSwitch, got %v", remoteBranchOptions[0].strategy)
	}
}

func TestRemoteBranchConflictDialog_J_NavigatesDown(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-1234", "api-gateway", "feature/IN-1234", "/path/to/repo")

	if d.selectedIndex != 0 {
		t.Fatalf("expected initial selectedIndex=0, got %d", d.selectedIndex)
	}

	modal, _ := d.Update(sendKey("j"))
	d = modal.(*RemoteBranchConflictDialog)

	if d.selectedIndex != 1 {
		t.Errorf("'j' should move selection down to index 1, got %d", d.selectedIndex)
	}
}

func TestRemoteBranchConflictDialog_K_NavigatesUp(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-1234", "api-gateway", "feature/IN-1234", "/path/to/repo")

	d.selectedIndex = 1

	modal, _ := d.Update(sendKey("k"))
	d = modal.(*RemoteBranchConflictDialog)

	if d.selectedIndex != 0 {
		t.Errorf("'k' should move selection up to index 0, got %d", d.selectedIndex)
	}
}

func TestRemoteBranchConflictDialog_J_WrapsToTop(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-1234", "api-gateway", "feature/IN-1234", "/path/to/repo")

	d.selectedIndex = len(remoteBranchOptions) - 1

	modal, _ := d.Update(sendKey("j"))
	d = modal.(*RemoteBranchConflictDialog)

	if d.selectedIndex != 0 {
		t.Errorf("'j' at bottom should wrap to index 0, got %d", d.selectedIndex)
	}
}

func TestRemoteBranchConflictDialog_K_WrapsToBottom(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-1234", "api-gateway", "feature/IN-1234", "/path/to/repo")

	if d.selectedIndex != 0 {
		t.Fatalf("expected initial selectedIndex=0, got %d", d.selectedIndex)
	}

	modal, _ := d.Update(sendKey("k"))
	d = modal.(*RemoteBranchConflictDialog)

	expectedBottom := len(remoteBranchOptions) - 1
	if d.selectedIndex != expectedBottom {
		t.Errorf("'k' at top should wrap to index %d, got %d", expectedBottom, d.selectedIndex)
	}
}

func TestRemoteBranchConflictDialog_DownArrow_NavigatesDown(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-1234", "api-gateway", "feature/IN-1234", "/path/to/repo")

	if d.selectedIndex != 0 {
		t.Fatalf("expected initial selectedIndex=0, got %d", d.selectedIndex)
	}

	modal, _ := d.Update(sendSpecialKey(tea.KeyDown))
	d = modal.(*RemoteBranchConflictDialog)

	if d.selectedIndex != 1 {
		t.Errorf("down arrow should move selection to index 1, got %d", d.selectedIndex)
	}
}

func TestRemoteBranchConflictDialog_UpArrow_NavigatesUp(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-1234", "api-gateway", "feature/IN-1234", "/path/to/repo")

	d.selectedIndex = 1

	modal, _ := d.Update(sendSpecialKey(tea.KeyUp))
	d = modal.(*RemoteBranchConflictDialog)

	if d.selectedIndex != 0 {
		t.Errorf("up arrow should move selection to index 0, got %d", d.selectedIndex)
	}
}

func TestRemoteBranchConflictDialog_DownArrow_WrapsToTop(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-1234", "api-gateway", "feature/IN-1234", "/path/to/repo")

	d.selectedIndex = len(remoteBranchOptions) - 1

	modal, _ := d.Update(sendSpecialKey(tea.KeyDown))
	d = modal.(*RemoteBranchConflictDialog)

	if d.selectedIndex != 0 {
		t.Errorf("down arrow at bottom should wrap to index 0, got %d", d.selectedIndex)
	}
}

func TestRemoteBranchConflictDialog_UpArrow_WrapsToBottom(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-1234", "api-gateway", "feature/IN-1234", "/path/to/repo")

	if d.selectedIndex != 0 {
		t.Fatalf("expected initial selectedIndex=0, got %d", d.selectedIndex)
	}

	modal, _ := d.Update(sendSpecialKey(tea.KeyUp))
	d = modal.(*RemoteBranchConflictDialog)

	expectedBottom := len(remoteBranchOptions) - 1
	if d.selectedIndex != expectedBottom {
		t.Errorf("up arrow at top should wrap to index %d, got %d", expectedBottom, d.selectedIndex)
	}
}

func TestRemoteBranchConflictDialog_Enter_EmitsSubmitMsg_FetchAndSwitch(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-1234", "api-gateway", "feature/IN-1234", "/path/to/repo")

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("Enter must return a cmd")
	}

	msg := execCmd(cmd)
	sub, ok := msg.(SubmitRemoteBranchStrategyMsg)
	if !ok {
		t.Fatalf("expected SubmitRemoteBranchStrategyMsg, got %T", msg)
	}

	if sub.TaskID != "IN-1234" {
		t.Errorf("TaskID: expected IN-1234, got %q", sub.TaskID)
	}

	if sub.ServiceName != "api-gateway" {
		t.Errorf("ServiceName: expected api-gateway, got %q", sub.ServiceName)
	}

	if sub.Strategy != task.StrategyFetchAndSwitch {
		t.Errorf("Strategy: expected StrategyFetchAndSwitch, got %v", sub.Strategy)
	}

	if sub.BranchSuffix != "" {
		t.Errorf("BranchSuffix: expected empty, got %q", sub.BranchSuffix)
	}
}

func TestRemoteBranchConflictDialog_Enter_EmitsSubmitMsg_Cancel(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-5678", "backend", "feature/IN-5678", "/path/to/repo")

	d.selectedIndex = 2

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("Enter must return a cmd")
	}

	msg := execCmd(cmd)
	sub, ok := msg.(SubmitRemoteBranchStrategyMsg)
	if !ok {
		t.Fatalf("expected SubmitRemoteBranchStrategyMsg, got %T", msg)
	}

	if sub.TaskID != "IN-5678" {
		t.Errorf("TaskID: expected IN-5678, got %q", sub.TaskID)
	}

	if sub.Strategy != task.StrategyCancel {
		t.Errorf("Strategy: expected StrategyCancel, got %v", sub.Strategy)
	}
}

func TestRemoteBranchConflictDialog_Enter_NewBranch_EntersSuffixMode(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-1234", "api-gateway", "feature/IN-1234", "/path/to/repo")

	d.selectedIndex = 1

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))

	if cmd != nil {
		t.Fatal("Enter on 'New Branch' should not emit a message yet")
	}

	if !d.inSuffixMode {
		t.Error("Enter on 'New Branch' should enter suffix mode")
	}

	if !d.suffixInput.Focused() {
		t.Error("Suffix input should be focused in suffix mode")
	}
}

func TestRemoteBranchConflictDialog_Esc_SelectionMode_EmitsCloseModalMsg(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-1234", "api-gateway", "feature/IN-1234", "/path/to/repo")

	_, cmd := d.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("Esc must return a cmd")
	}

	msg := execCmd(cmd)
	if _, ok := msg.(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", msg)
	}
}

func TestRemoteBranchConflictDialog_Esc_SuffixMode_ReturnsToSelectionMode(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-1234", "api-gateway", "feature/IN-1234", "/path/to/repo")

	d.selectedIndex = 1
	d.Update(sendSpecialKey(tea.KeyEnter))

	if !d.inSuffixMode {
		t.Fatal("should be in suffix mode")
	}

	d.Update(sendKey("v"))
	d.Update(sendKey("2"))

	if d.suffixInput.Value() != "v2" {
		t.Errorf("suffix input should be 'v2', got %q", d.suffixInput.Value())
	}

	modal, cmd := d.Update(sendSpecialKey(tea.KeyEsc))
	d = modal.(*RemoteBranchConflictDialog)

	if cmd != nil {
		t.Errorf("Esc in suffix mode should not emit a message, got %T", execCmd(cmd))
	}

	if d.inSuffixMode {
		t.Error("Esc should exit suffix mode")
	}

	if d.suffixInput.Value() != "" {
		t.Errorf("Suffix input should be cleared, got %q", d.suffixInput.Value())
	}

	if d.suffixError != "" {
		t.Errorf("Suffix error should be cleared, got %q", d.suffixError)
	}
}

func TestRemoteBranchConflictDialog_SuffixInput_EmptySuffix(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-1234", "api-gateway", "feature/IN-1234", "/path/to/repo")

	d.selectedIndex = 1
	d.Update(sendSpecialKey(tea.KeyEnter))

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))

	if cmd != nil {
		t.Fatal("Enter with empty suffix should not emit a message")
	}

	if d.suffixError == "" {
		t.Error("Empty suffix should show an error")
	}
}

func TestRemoteBranchConflictDialog_SuffixInput_ValidSuffix(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-1234", "api-gateway", "feature/IN-1234", "/path/to/repo")

	d.selectedIndex = 1
	d.Update(sendSpecialKey(tea.KeyEnter))

	d.Update(sendKey("-"))
	d.Update(sendKey("v"))
	d.Update(sendKey("2"))

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("Enter with valid suffix must return a cmd")
	}

	msg := execCmd(cmd)
	sub, ok := msg.(SubmitRemoteBranchStrategyMsg)
	if !ok {
		t.Fatalf("expected SubmitRemoteBranchStrategyMsg, got %T", msg)
	}

	if sub.Strategy != task.StrategyNewBranch {
		t.Errorf("Strategy: expected StrategyNewBranch, got %v", sub.Strategy)
	}

	if sub.BranchSuffix != "-v2" {
		t.Errorf("BranchSuffix: expected '-v2', got %q", sub.BranchSuffix)
	}
}

func TestRemoteBranchConflictDialog_SuffixInput_InvalidCharacters(t *testing.T) {
	invalidSuffixes := []string{
		"..",
		".suffix",
		"test/",
		"test.",
		"test ",
		"test~",
		"test^",
		"test:",
		"test?",
		"test*",
		"test[",
		"test\\",
		"test@{",
	}

	for _, suffix := range invalidSuffixes {
		t.Run(suffix, func(t *testing.T) {
			err := validateBranchSuffix(suffix)
			if err == nil {
				t.Errorf("validateBranchSuffix(%q) should return an error", suffix)
			}
		})
	}
}

func TestRemoteBranchConflictDialog_SuffixInput_ValidCharacters(t *testing.T) {
	validSuffixes := []string{
		"-v2",
		"-fix",
		"_new",
		"123",
		"feature-branch",
		"test_123",
		"v2.0",
		"release/1.0",
	}

	for _, suffix := range validSuffixes {
		t.Run(suffix, func(t *testing.T) {
			err := validateBranchSuffix(suffix)
			if err != nil {
				t.Errorf("validateBranchSuffix(%q) should not return an error, got %v", suffix, err)
			}
		})
	}
}

func TestRemoteBranchConflictDialog_Title(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-9999", "service", "feature/IN-9999", "/path")

	title := d.Title()
	if title != "Remote Branch Conflict" {
		t.Errorf("Title: expected 'Remote Branch Conflict', got %q", title)
	}
}

func TestRemoteBranchConflictDialog_View_SelectionMode_ContainsExpectedContent(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-1234", "api-gateway", "feature/IN-1234", "/path/to/repo")
	view := stripAnsi(d.View())

	if !strings.Contains(view, "IN-1234") {
		t.Error("View should contain task ID")
	}

	if !strings.Contains(view, "api-gateway") {
		t.Error("View should contain service name")
	}

	if !strings.Contains(view, "feature/IN-1234") {
		t.Error("View should contain branch name")
	}

	if !strings.Contains(view, "Track Remote Branch") {
		t.Error("View should contain 'Track Remote Branch'")
	}
	if !strings.Contains(view, "New Branch") {
		t.Error("View should contain 'New Branch'")
	}
	if !strings.Contains(view, "Cancel") {
		t.Error("View should contain 'Cancel'")
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

func TestRemoteBranchConflictDialog_View_SuffixMode_ContainsExpectedContent(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-1234", "api-gateway", "feature/IN-1234", "/path/to/repo")

	d.selectedIndex = 1
	d.Update(sendSpecialKey(tea.KeyEnter))

	d.Update(sendKey("-"))
	d.Update(sendKey("v"))
	d.Update(sendKey("2"))

	view := stripAnsi(d.View())

	if !strings.Contains(view, "feature/IN-1234-v2") {
		t.Errorf("View should contain 'feature/IN-1234-v2', got: %s", view)
	}

	if !strings.Contains(view, "Enter") {
		t.Error("View should contain 'Enter' hint in suffix mode")
	}
	if !strings.Contains(view, "Esc") {
		t.Error("View should contain 'Esc' hint in suffix mode")
	}
}

func TestRemoteBranchConflictDialog_View_ShowsErrorMessage(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-1234", "api-gateway", "feature/IN-1234", "/path/to/repo")

	d.selectedIndex = 1
	d.Update(sendSpecialKey(tea.KeyEnter))

	d.Update(sendSpecialKey(tea.KeyEnter))

	view := stripAnsi(d.View())

	if !strings.Contains(view, "Error:") {
		t.Error("View should contain 'Error:' for invalid suffix")
	}
}

func TestRemoteBranchConflictDialog_View_ShowsSelectionIndicator(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-1234", "api-gateway", "feature/IN-1234", "/path/to/repo")

	view := d.View()

	if !strings.Contains(view, "◉") {
		t.Error("View should contain selected indicator '◉'")
	}
	if !strings.Contains(view, "○") {
		t.Error("View should contain unselected indicator '○'")
	}
}

func TestRemoteBranchConflictDialog_SetTerminalSize(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-1234", "api-gateway", "feature/IN-1234", "/path/to/repo")

	d.SetTerminalSize(120, 40)

	if d.terminalWidth != 120 {
		t.Errorf("terminalWidth: expected 120, got %d", d.terminalWidth)
	}
	if d.terminalHeight != 40 {
		t.Errorf("terminalHeight: expected 40, got %d", d.terminalHeight)
	}
}

func TestRemoteBranchConflictDialog_UnknownKey_DoesNothing(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-1234", "api-gateway", "feature/IN-1234", "/path/to/repo")

	modal, cmd := d.Update(sendKey("x"))
	d = modal.(*RemoteBranchConflictDialog)

	if cmd != nil {
		t.Error("Unknown key should return nil cmd")
	}
	if d.selectedIndex != 0 {
		t.Errorf("Unknown key should not change selection, got %d", d.selectedIndex)
	}
}

func TestRemoteBranchConflictDialog_MultipleNavigationSteps(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-1234", "api-gateway", "feature/IN-1234", "/path/to/repo")

	modal, _ := d.Update(sendKey("j"))
	d = modal.(*RemoteBranchConflictDialog)
	if d.selectedIndex != 1 {
		t.Errorf("after first 'j': expected index 1, got %d", d.selectedIndex)
	}

	modal, _ = d.Update(sendKey("j"))
	d = modal.(*RemoteBranchConflictDialog)
	if d.selectedIndex != 2 {
		t.Errorf("after second 'j': expected index 2, got %d", d.selectedIndex)
	}

	modal, _ = d.Update(sendKey("j"))
	d = modal.(*RemoteBranchConflictDialog)
	if d.selectedIndex != 0 {
		t.Errorf("after third 'j': expected index 0 (wrap), got %d", d.selectedIndex)
	}

	modal, _ = d.Update(sendKey("k"))
	d = modal.(*RemoteBranchConflictDialog)
	if d.selectedIndex != 2 {
		t.Errorf("after 'k': expected index 2 (wrap), got %d", d.selectedIndex)
	}
}

func TestRemoteBranchConflictDialog_SuffixInput_ClearsErrorOnTyping(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-1234", "api-gateway", "feature/IN-1234", "/path/to/repo")

	d.selectedIndex = 1
	d.Update(sendSpecialKey(tea.KeyEnter))

	d.Update(sendSpecialKey(tea.KeyEnter))

	if d.suffixError == "" {
		t.Fatal("Expected error for empty suffix")
	}

	d.Update(sendKey("a"))

	if d.suffixError != "" {
		t.Errorf("Typing should clear error, got %q", d.suffixError)
	}
}

func TestRemoteBranchConflictDialog_SuffixInput_TrimsWhitespace(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-1234", "api-gateway", "feature/IN-1234", "/path/to/repo")

	d.selectedIndex = 1
	d.Update(sendSpecialKey(tea.KeyEnter))

	d.Update(sendKey(" "))
	d.Update(sendKey("-"))
	d.Update(sendKey("v"))
	d.Update(sendKey("2"))
	d.Update(sendKey(" "))

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("Enter with valid suffix must return a cmd")
	}

	msg := execCmd(cmd)
	sub, ok := msg.(SubmitRemoteBranchStrategyMsg)
	if !ok {
		t.Fatalf("expected SubmitRemoteBranchStrategyMsg, got %T", msg)
	}

	if sub.BranchSuffix != "-v2" {
		t.Errorf("BranchSuffix: expected '-v2' (trimmed), got %q", sub.BranchSuffix)
	}
}

func TestRemoteBranchConflictDialog_SuffixInput_WhitespaceOnlyError(t *testing.T) {
	d := NewRemoteBranchConflictDialog("IN-1234", "api-gateway", "feature/IN-1234", "/path/to/repo")

	d.selectedIndex = 1
	d.Update(sendSpecialKey(tea.KeyEnter))

	d.Update(sendKey(" "))
	d.Update(sendKey(" "))

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))

	if cmd != nil {
		t.Fatal("Enter with whitespace-only suffix should not emit a message")
	}

	if d.suffixError == "" {
		t.Error("Whitespace-only suffix should show an error")
	}
}
