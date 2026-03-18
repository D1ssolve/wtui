package panels

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// ── OutputPanel tests ─────────────────────────────────────────────────────────

func TestOutputPanel_New_EmptyViewport(t *testing.T) {
	p := NewOutputPanel(80, 10)
	if p.lineCount() != 0 {
		t.Errorf("expected 0 lines on new panel, got %d", p.lineCount())
	}
	if p.width != 80 || p.height != 10 {
		t.Errorf("expected 80×10, got %d×%d", p.width, p.height)
	}
}

func TestOutputPanel_AppendLine_IncreasesLineCount(t *testing.T) {
	p := NewOutputPanel(80, 10)
	p.AppendLine("hello world")
	if p.lineCount() != 1 {
		t.Errorf("expected 1 line after AppendLine, got %d", p.lineCount())
	}
}

func TestOutputPanel_AppendLine_PrefixedWithArrow(t *testing.T) {
	p := NewOutputPanel(80, 10)
	p.AppendLine("build started")
	lines := p.rawLines()
	if len(lines) == 0 {
		t.Fatal("expected at least 1 raw line")
	}
	// Raw line may contain ANSI codes; check it contains "> " and the text.
	combined := strings.Join(lines, "")
	if !strings.Contains(combined, ">") {
		t.Errorf("AppendLine should prefix with '>': %q", combined)
	}
	if !strings.Contains(combined, "build started") {
		t.Errorf("AppendLine should contain original text: %q", combined)
	}
}

func TestOutputPanel_AppendLine_MultipleLines(t *testing.T) {
	p := NewOutputPanel(80, 15)
	for i := 0; i < 5; i++ {
		p.AppendLine("line")
	}
	if p.lineCount() != 5 {
		t.Errorf("expected 5 lines, got %d", p.lineCount())
	}
}

func TestOutputPanel_AppendLine_AutoScrollsToBottom(t *testing.T) {
	p := NewOutputPanel(80, 5) // small height to force scrolling
	// Fill beyond visible area.
	for i := 0; i < 20; i++ {
		p.AppendLine("line")
	}
	// After each AppendLine the viewport should be at bottom.
	if !p.viewport.AtBottom() {
		t.Error("AppendLine should auto-scroll to bottom")
	}
}

func TestOutputPanel_Clear_RemovesAllLines(t *testing.T) {
	p := NewOutputPanel(80, 10)
	p.AppendLine("line 1")
	p.AppendLine("line 2")
	p.Clear()
	if p.lineCount() != 0 {
		t.Errorf("Clear should remove all lines, got %d", p.lineCount())
	}
}

func TestOutputPanel_SetSize_UpdatesDimensions(t *testing.T) {
	p := NewOutputPanel(80, 10)
	p.SetSize(120, 20)
	if p.width != 120 || p.height != 20 {
		t.Errorf("SetSize: expected 120×20, got %d×%d", p.width, p.height)
	}
	// Viewport inner width should be (120 - 2) = 118.
	if p.viewport.Width != 118 {
		t.Errorf("viewport width should be 118, got %d", p.viewport.Width)
	}
}

func TestOutputPanel_SetFocused(t *testing.T) {
	p := NewOutputPanel(80, 10)
	p.SetFocused(true)
	if !p.focused {
		t.Error("SetFocused(true) should set focused")
	}
	p.SetFocused(false)
	if p.focused {
		t.Error("SetFocused(false) should clear focused")
	}
}

func TestOutputPanel_ScrollToBottom(t *testing.T) {
	p := NewOutputPanel(80, 5)
	for i := 0; i < 20; i++ {
		p.AppendLine("line")
	}
	// Manually scroll up.
	p.viewport.GotoTop()
	// Call ScrollToBottom.
	p.ScrollToBottom()
	if !p.viewport.AtBottom() {
		t.Error("ScrollToBottom should move viewport to bottom")
	}
}

// ── Keybinding tests ──────────────────────────────────────────────────────────

func TestOutputPanel_KeyEsc_EmitsFocusTasksMsg(t *testing.T) {
	p := NewOutputPanel(80, 10)
	p.SetFocused(true)

	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc should return a cmd")
	}
	msg := cmd()
	if _, ok := msg.(FocusTasksMsg); !ok {
		t.Fatalf("expected FocusTasksMsg, got %T", msg)
	}
}

func TestOutputPanel_KeyG_Lower_ScrollsToTop(t *testing.T) {
	p := NewOutputPanel(80, 5)
	for i := 0; i < 20; i++ {
		p.AppendLine("line")
	}
	// Viewport is at bottom after appends.
	p.SetFocused(true)

	p, _ = p.Update(sendKey("g"))
	if !p.viewport.AtTop() {
		t.Error("g key should scroll to top")
	}
}

func TestOutputPanel_KeyG_Upper_ScrollsToBottom(t *testing.T) {
	p := NewOutputPanel(80, 5)
	for i := 0; i < 20; i++ {
		p.AppendLine("line")
	}
	p.viewport.GotoTop() // scroll up first
	p.SetFocused(true)

	p, _ = p.Update(sendKey("G"))
	if !p.viewport.AtBottom() {
		t.Error("G key should scroll to bottom")
	}
}

func TestOutputPanel_KeyJ_ScrollsDown(t *testing.T) {
	p := NewOutputPanel(80, 5)
	for i := 0; i < 20; i++ {
		p.AppendLine("line")
	}
	p.viewport.GotoTop()
	p.SetFocused(true)
	before := p.viewport.YOffset

	p, _ = p.Update(sendKey("j"))
	if p.viewport.YOffset <= before {
		t.Error("j key should scroll viewport down")
	}
}

func TestOutputPanel_KeyK_ScrollsUp(t *testing.T) {
	p := NewOutputPanel(80, 5)
	for i := 0; i < 20; i++ {
		p.AppendLine("line")
	}
	// Start at bottom.
	p.SetFocused(true)
	before := p.viewport.YOffset

	p, _ = p.Update(sendKey("k"))
	if p.viewport.YOffset >= before {
		t.Error("k key should scroll viewport up")
	}
}

func TestOutputPanel_Unfocused_KeysIgnored(t *testing.T) {
	p := NewOutputPanel(80, 10)
	// focused = false (default)

	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(FocusTasksMsg); ok {
			t.Error("unfocused panel should not emit FocusTasksMsg on Esc")
		}
	}
}

// ── View tests ────────────────────────────────────────────────────────────────

func TestOutputPanel_View_ContainsTitle(t *testing.T) {
	p := NewOutputPanel(80, 10)
	view := stripAnsi(p.View())
	if !strings.Contains(view, "Output") {
		t.Errorf("View should contain 'Output' title, got: %q", view)
	}
}

func TestOutputPanel_View_ContainsAppendedLine(t *testing.T) {
	p := NewOutputPanel(80, 10)
	p.AppendLine("hello from subprocess")
	view := p.View() // keep ANSI for content check
	if !strings.Contains(view, "hello from subprocess") {
		t.Errorf("View should contain appended line text, got: %q", view)
	}
}
