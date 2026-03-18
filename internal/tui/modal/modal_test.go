package modal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// sendKey returns a tea.KeyMsg for a printable rune key.
func sendKey(key string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}

// sendSpecialKey returns a tea.KeyMsg for a named special key.
func sendSpecialKey(t tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: t}
}

// execCmd calls cmd() and returns the resulting tea.Msg.
// Returns nil if cmd is nil.
func execCmd(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

// stripAnsi removes ANSI escape sequences for plain-text comparison in tests.
func stripAnsi(s string) string {
	var result strings.Builder
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}

// ── 1. InitDialog Tab cycles through fields ───────────────────────────────────

func TestInitDialog_TabCycles(t *testing.T) {
	d := NewInitDialog("feature/")

	// Start at field 0.
	if d.focusIndex != 0 {
		t.Fatalf("expected focusIndex=0, got %d", d.focusIndex)
	}

	// Tab → field 1.
	modal, _ := d.Update(sendSpecialKey(tea.KeyTab))
	d = modal.(*InitDialog)
	if d.focusIndex != 1 {
		t.Errorf("after Tab: expected focusIndex=1, got %d", d.focusIndex)
	}

	// Tab → field 2.
	modal, _ = d.Update(sendSpecialKey(tea.KeyTab))
	d = modal.(*InitDialog)
	if d.focusIndex != 2 {
		t.Errorf("after Tab×2: expected focusIndex=2, got %d", d.focusIndex)
	}

	// Tab → field 3.
	modal, _ = d.Update(sendSpecialKey(tea.KeyTab))
	d = modal.(*InitDialog)
	if d.focusIndex != 3 {
		t.Errorf("after Tab×3: expected focusIndex=3, got %d", d.focusIndex)
	}

	// Tab → wraps back to field 0.
	modal, _ = d.Update(sendSpecialKey(tea.KeyTab))
	d = modal.(*InitDialog)
	if d.focusIndex != 0 {
		t.Errorf("after Tab×4 (wrap): expected focusIndex=0, got %d", d.focusIndex)
	}
}

// ── 2. InitDialog Enter on last field emits SubmitInitMsg ─────────────────────

func TestInitDialog_Enter_LastField_Submits(t *testing.T) {
	d := NewInitDialog("feature/")

	// Pre-fill all fields.
	d.fields[0].SetValue("IN-9999")
	d.fields[1].SetValue("svc1, svc2 svc3")
	d.fields[2].SetValue("feature/")
	d.fields[3].SetValue("main")

	// Navigate to last field.
	d.focusIndex = 3
	d.focusField(3)

	// Send Enter.
	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("Enter on last field must return a cmd")
	}
	msg := execCmd(cmd)
	sub, ok := msg.(SubmitInitMsg)
	if !ok {
		t.Fatalf("expected SubmitInitMsg, got %T", msg)
	}

	if sub.TaskID != "IN-9999" {
		t.Errorf("TaskID: expected IN-9999, got %q", sub.TaskID)
	}
	if sub.BranchPrefix != "feature/" {
		t.Errorf("BranchPrefix: expected feature/, got %q", sub.BranchPrefix)
	}
	if sub.BaseBranch != "main" {
		t.Errorf("BaseBranch: expected main, got %q", sub.BaseBranch)
	}
	// Services parsed from "svc1, svc2 svc3" → ["svc1", "svc2", "svc3"]
	if len(sub.Services) != 3 {
		t.Errorf("Services: expected 3, got %d: %v", len(sub.Services), sub.Services)
	} else {
		for i, want := range []string{"svc1", "svc2", "svc3"} {
			if sub.Services[i] != want {
				t.Errorf("Services[%d]: expected %q, got %q", i, want, sub.Services[i])
			}
		}
	}
}

// ── 3. InitDialog Esc emits CloseModalMsg ────────────────────────────────────

func TestInitDialog_Esc_Closes(t *testing.T) {
	d := NewInitDialog("feature/")

	_, cmd := d.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("Esc must return a cmd")
	}
	msg := execCmd(cmd)
	if _, ok := msg.(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", msg)
	}
}

// ── 4. InitDialog Enter on fields 0-2 advances to next field ─────────────────

func TestInitDialog_Enter_NonLastField_Advances(t *testing.T) {
	d := NewInitDialog("")

	// On field 0 — Enter should move to field 1.
	if d.focusIndex != 0 {
		t.Fatalf("expected focusIndex=0, got %d", d.focusIndex)
	}
	modal, _ := d.Update(sendSpecialKey(tea.KeyEnter))
	d = modal.(*InitDialog)
	if d.focusIndex != 1 {
		t.Errorf("Enter on field 0 should advance to field 1, got %d", d.focusIndex)
	}
}

// ── 5. AddDialog Enter emits SubmitAddMsg with parsed services ────────────────

func TestAddDialog_Enter_Submits(t *testing.T) {
	d := NewAddDialog("IN-6748")
	d.input.SetValue("alpha beta,gamma")

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("Enter must return a cmd")
	}
	msg := execCmd(cmd)
	sub, ok := msg.(SubmitAddMsg)
	if !ok {
		t.Fatalf("expected SubmitAddMsg, got %T", msg)
	}
	if sub.TaskID != "IN-6748" {
		t.Errorf("TaskID: expected IN-6748, got %q", sub.TaskID)
	}
	// "alpha beta,gamma" → ["alpha", "beta", "gamma"]
	if len(sub.Services) != 3 {
		t.Fatalf("Services: expected 3, got %d: %v", len(sub.Services), sub.Services)
	}
	for i, want := range []string{"alpha", "beta", "gamma"} {
		if sub.Services[i] != want {
			t.Errorf("Services[%d]: expected %q, got %q", i, want, sub.Services[i])
		}
	}
}

// ── 6. AddDialog Esc emits CloseModalMsg ─────────────────────────────────────

func TestAddDialog_Esc_Closes(t *testing.T) {
	d := NewAddDialog("IN-0001")
	_, cmd := d.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("Esc must return a cmd")
	}
	msg := execCmd(cmd)
	if _, ok := msg.(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", msg)
	}
}

// ── 7. RemoveDialog y is no-op when dirty and force not checked ───────────────

func TestRemoveDialog_YNoopWhenDirty(t *testing.T) {
	d := NewRemoveDialog("IN-6748", 3, []string{"service-a", "service-b"})

	// forceChecked is false by default.
	if d.forceChecked {
		t.Fatal("forceChecked should be false initially")
	}
	if d.canConfirm() {
		t.Fatal("canConfirm should be false when dirty and force not checked")
	}

	_, cmd := d.Update(sendKey("y"))
	if cmd != nil {
		msg := execCmd(cmd)
		if _, ok := msg.(SubmitRemoveMsg); ok {
			t.Error("y should be a no-op when dirty services exist and Force is unchecked")
		}
	}
}

// ── 8. RemoveDialog force toggle then y emits SubmitRemoveMsg ────────────────

func TestRemoveDialog_ForceToggle_ThenY_Submits(t *testing.T) {
	d := NewRemoveDialog("IN-6748", 2, []string{"service-a"})

	// Toggle force.
	modal, _ := d.Update(sendKey("f"))
	d = modal.(*RemoveDialog)
	if !d.forceChecked {
		t.Fatal("f should toggle forceChecked to true")
	}
	if !d.canConfirm() {
		t.Fatal("canConfirm should be true after force checked")
	}

	// Confirm with y.
	_, cmd := d.Update(sendKey("y"))
	if cmd == nil {
		t.Fatal("y after force check must return a cmd")
	}
	msg := execCmd(cmd)
	sub, ok := msg.(SubmitRemoveMsg)
	if !ok {
		t.Fatalf("expected SubmitRemoveMsg, got %T", msg)
	}
	if sub.TaskID != "IN-6748" {
		t.Errorf("TaskID: expected IN-6748, got %q", sub.TaskID)
	}
	if !sub.Force {
		t.Error("Force should be true")
	}
}

// ── 9. RemoveDialog n emits CloseModalMsg ────────────────────────────────────

func TestRemoveDialog_N_Closes(t *testing.T) {
	d := NewRemoveDialog("IN-6748", 1, nil)
	_, cmd := d.Update(sendKey("n"))
	if cmd == nil {
		t.Fatal("n must return a cmd")
	}
	msg := execCmd(cmd)
	if _, ok := msg.(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", msg)
	}
}

// ── 10. RemoveDialog Esc emits CloseModalMsg ─────────────────────────────────

func TestRemoveDialog_Esc_Closes(t *testing.T) {
	d := NewRemoveDialog("IN-6748", 1, nil)
	_, cmd := d.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("Esc must return a cmd")
	}
	msg := execCmd(cmd)
	if _, ok := msg.(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", msg)
	}
}

// ── 11. RemoveDialog y on clean task emits SubmitRemoveMsg with Force=false ───

func TestRemoveDialog_Y_CleanTask_Submits(t *testing.T) {
	d := NewRemoveDialog("IN-1234", 2, nil) // no dirty services

	_, cmd := d.Update(sendKey("y"))
	if cmd == nil {
		t.Fatal("y on clean task must return a cmd")
	}
	msg := execCmd(cmd)
	sub, ok := msg.(SubmitRemoveMsg)
	if !ok {
		t.Fatalf("expected SubmitRemoveMsg, got %T", msg)
	}
	if sub.TaskID != "IN-1234" {
		t.Errorf("TaskID: expected IN-1234, got %q", sub.TaskID)
	}
	if sub.Force {
		t.Error("Force should be false for clean task without toggling")
	}
}

// ── 12. HelpOverlay contains key text in View() ───────────────────────────────

func TestHelpOverlay_ViewContainsKeyText(t *testing.T) {
	h := NewHelpOverlay()
	view := stripAnsi(h.View())

	mustContain := []string{
		"Keyboard Shortcuts",
		"Tasks Panel",
		"Services Panel",
		"Output Panel",
		"Global",
		"Init new task group",
		"Remove task group",
		"Add service to task",
		"Scroll up/down",
		"Toggle this help",
		"Quit",
	}
	for _, want := range mustContain {
		if !strings.Contains(view, want) {
			t.Errorf("HelpOverlay.View() missing expected text %q", want)
		}
	}
}

// ── 13. HelpOverlay Esc closes ────────────────────────────────────────────────

func TestHelpOverlay_Esc_Closes(t *testing.T) {
	h := NewHelpOverlay()
	_, cmd := h.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("Esc must return a cmd")
	}
	msg := execCmd(cmd)
	if _, ok := msg.(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", msg)
	}
}

// ── 14. HelpOverlay ? closes ─────────────────────────────────────────────────

func TestHelpOverlay_QuestionMark_Closes(t *testing.T) {
	h := NewHelpOverlay()
	_, cmd := h.Update(sendKey("?"))
	if cmd == nil {
		t.Fatal("? must return a cmd")
	}
	msg := execCmd(cmd)
	if _, ok := msg.(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", msg)
	}
}

// ── 15. OverlayView returns non-empty string ──────────────────────────────────

func TestOverlayView_ReturnsNonEmpty(t *testing.T) {
	tests := []struct {
		name    string
		content string
		termW   int
		termH   int
	}{
		{"normal size", "hello world", 120, 40},
		{"tiny terminal", "x", 10, 5},
		{"empty content", "", 80, 24},
		{"wide content", strings.Repeat("a", 200), 80, 24},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := OverlayView(tc.content, tc.termW, tc.termH)
			if result == "" {
				t.Error("OverlayView must return a non-empty string")
			}
		})
	}
}

// ── 16. parseServices correctly handles mixed separators ─────────────────────

func TestParseServices(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"svc1 svc2 svc3", []string{"svc1", "svc2", "svc3"}},
		{"svc1,svc2,svc3", []string{"svc1", "svc2", "svc3"}},
		{"svc1, svc2 ,svc3", []string{"svc1", "svc2", "svc3"}},
		{"  svc1  ", []string{"svc1"}},
		{"", []string{}},
		{"svc1,,svc2", []string{"svc1", "svc2"}},
	}

	for _, tc := range tests {
		got := parseServices(tc.input)
		if len(got) != len(tc.want) {
			t.Errorf("parseServices(%q): expected %v, got %v", tc.input, tc.want, got)
			continue
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Errorf("parseServices(%q)[%d]: expected %q, got %q", tc.input, i, tc.want[i], got[i])
			}
		}
	}
}

// ── 17. InitDialog BranchPrefix pre-filled ───────────────────────────────────

func TestInitDialog_BranchPrefixPreFilled(t *testing.T) {
	d := NewInitDialog("hotfix/")
	if got := d.fields[2].Value(); got != "hotfix/" {
		t.Errorf("Branch Prefix field should be pre-filled with 'hotfix/', got %q", got)
	}
}

// ── 18. InitDialog ShiftTab moves backwards ──────────────────────────────────

func TestInitDialog_ShiftTab_MovesBack(t *testing.T) {
	d := NewInitDialog("")
	// Move to field 2.
	d.focusField(2)

	modal, _ := d.Update(sendSpecialKey(tea.KeyShiftTab))
	d = modal.(*InitDialog)
	if d.focusIndex != 1 {
		t.Errorf("Shift+Tab from field 2 should go to field 1, got %d", d.focusIndex)
	}

	// Wrap: Shift+Tab from field 0 should go to field 3.
	d.focusField(0)
	modal, _ = d.Update(sendSpecialKey(tea.KeyShiftTab))
	d = modal.(*InitDialog)
	if d.focusIndex != 3 {
		t.Errorf("Shift+Tab from field 0 should wrap to field 3, got %d", d.focusIndex)
	}
}

// ── 19. RemoveDialog Space toggles force ─────────────────────────────────────

func TestRemoveDialog_SpaceTogglesForce(t *testing.T) {
	d := NewRemoveDialog("IN-0001", 1, []string{"dirty-svc"})

	modal, _ := d.Update(sendSpecialKey(tea.KeySpace))
	d = modal.(*RemoveDialog)
	if !d.forceChecked {
		t.Error("Space should toggle forceChecked to true")
	}

	modal, _ = d.Update(sendSpecialKey(tea.KeySpace))
	d = modal.(*RemoveDialog)
	if d.forceChecked {
		t.Error("Space again should toggle forceChecked back to false")
	}
}

// ── 20. RemoveDialog View contains dirty service warnings ────────────────────

func TestRemoveDialog_ViewContainsDirtyWarnings(t *testing.T) {
	d := NewRemoveDialog("IN-7777", 2, []string{"my-service"})
	view := stripAnsi(d.View())

	if !strings.Contains(view, "my-service") {
		t.Error("View should contain the dirty service name")
	}
	if !strings.Contains(view, "uncommitted changes") {
		t.Error("View should mention uncommitted changes")
	}
}

// ── 21. AddDialog Title includes taskID ──────────────────────────────────────

func TestAddDialog_TitleIncludesTaskID(t *testing.T) {
	d := NewAddDialog("IN-5555")
	if !strings.Contains(d.Title(), "IN-5555") {
		t.Errorf("Title() should contain taskID, got %q", d.Title())
	}
}
