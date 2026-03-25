package modal

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/diss0x/wtui/internal/config"
	"github.com/diss0x/wtui/internal/domain"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// makeTestRepos builds a []domain.Repo from a list of names for use in tests.
func makeTestRepos(names ...string) []domain.Repo {
	repos := make([]domain.Repo, len(names))
	for i, n := range names {
		repos[i] = domain.Repo{Name: n}
	}
	return repos
}

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
	d := NewInitDialog("feature/", nil, 80, 24)

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
	d := NewInitDialog("feature/", nil, 80, 24)

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
	d := NewInitDialog("feature/", nil, 80, 24)

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
	d := NewInitDialog("", nil, 80, 24)

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
	d := NewAddDialog("IN-6748", nil, 80, 24)
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
	d := NewAddDialog("IN-0001", nil, 80, 24)
	_, cmd := d.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("Esc must return a cmd")
	}
	msg := execCmd(cmd)
	if _, ok := msg.(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", msg)
	}
}

// ── 7. RemoveDialog y emits SubmitRemoveTaskMsg with Force=false, DeleteBranches=false ──

func TestRemoveDialog_Y_Submits(t *testing.T) {
	d := NewRemoveTaskDialog("IN-6748", 3, []string{"service-a", "service-b"})

	_, cmd := d.Update(sendKey("y"))
	if cmd == nil {
		t.Fatal("y must return a cmd")
	}
	msg := execCmd(cmd)
	sub, ok := msg.(SubmitRemoveTaskMsg)
	if !ok {
		t.Fatalf("expected SubmitRemoveTaskMsg, got %T", msg)
	}
	if sub.TaskID != "IN-6748" {
		t.Errorf("TaskID: expected IN-6748, got %q", sub.TaskID)
	}
	if sub.Force {
		t.Error("Force should be false for y")
	}
	if sub.DeleteBranches {
		t.Error("DeleteBranches should be false for y")
	}
}

// ── 8. RemoveDialog f emits SubmitRemoveTaskMsg with Force=true ───────────────

func TestRemoveDialog_F_ForceRemoves(t *testing.T) {
	d := NewRemoveTaskDialog("IN-6748", 2, []string{"service-a"})

	_, cmd := d.Update(sendKey("f"))
	if cmd == nil {
		t.Fatal("f must return a cmd")
	}
	msg := execCmd(cmd)
	sub, ok := msg.(SubmitRemoveTaskMsg)
	if !ok {
		t.Fatalf("expected SubmitRemoveTaskMsg, got %T", msg)
	}
	if sub.TaskID != "IN-6748" {
		t.Errorf("TaskID: expected IN-6748, got %q", sub.TaskID)
	}
	if !sub.Force {
		t.Error("Force should be true for f")
	}
	if sub.DeleteBranches {
		t.Error("DeleteBranches should be false for f")
	}
}

// ── 9. RemoveDialog n emits CloseModalMsg ────────────────────────────────────

func TestRemoveDialog_N_Closes(t *testing.T) {
	d := NewRemoveTaskDialog("IN-6748", 1, nil)
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
	d := NewRemoveTaskDialog("IN-6748", 1, nil)
	_, cmd := d.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("Esc must return a cmd")
	}
	msg := execCmd(cmd)
	if _, ok := msg.(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", msg)
	}
}

// ── 11. RemoveDialog b emits SubmitRemoveTaskMsg with DeleteBranches=true ─────

func TestRemoveDialog_B_DeletesBranches(t *testing.T) {
	d := NewRemoveTaskDialog("IN-1234", 2, nil)

	_, cmd := d.Update(sendKey("b"))
	if cmd == nil {
		t.Fatal("b must return a cmd")
	}
	msg := execCmd(cmd)
	sub, ok := msg.(SubmitRemoveTaskMsg)
	if !ok {
		t.Fatalf("expected SubmitRemoveTaskMsg, got %T", msg)
	}
	if sub.TaskID != "IN-1234" {
		t.Errorf("TaskID: expected IN-1234, got %q", sub.TaskID)
	}
	if sub.Force {
		t.Error("Force should be false for b")
	}
	if !sub.DeleteBranches {
		t.Error("DeleteBranches should be true for b")
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
		name        string
		content     string
		termW       int
		termH       int
		maxContentH int
	}{
		{"normal size", "hello world", 120, 40, 28},
		{"tiny terminal", "x", 10, 5, 3},
		{"empty content", "", 80, 24, 16},
		{"wide content", strings.Repeat("a", 200), 80, 24, 16},
		{"small terminal 80x24", "content", 80, 24, 16},
		{"large terminal 200x60", "content", 200, 60, 42},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := OverlayView(tc.content, tc.termW, tc.termH, tc.maxContentH)
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
	d := NewInitDialog("hotfix/", nil, 80, 24)
	if got := d.fields[2].Value(); got != "hotfix/" {
		t.Errorf("Branch Prefix field should be pre-filled with 'hotfix/', got %q", got)
	}
}

// ── 18. InitDialog ShiftTab moves backwards ──────────────────────────────────

func TestInitDialog_ShiftTab_MovesBack(t *testing.T) {
	d := NewInitDialog("", nil, 80, 24)
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

// ── 19. InitDialog Tab cycles fields (not within repo picker) ────────────────────

func TestInitDialog_Tab_CyclesFields_WithRepoPicker(t *testing.T) {
	repos := makeTestRepos("alpha", "beta", "gamma")
	d := NewInitDialog("feature/", repos, 80, 24)

	// Focus field 1 (repo picker).
	d.focusField(1)
	if !d.repoPickerFocused {
		t.Fatal("expected repoPickerFocused=true")
	}
	if d.focusIndex != 1 {
		t.Fatalf("expected focusIndex=1, got %d", d.focusIndex)
	}

	// Tab should advance to next field (Branch Prefix), NOT navigate within repo picker.
	modal, _ := d.Update(sendSpecialKey(tea.KeyTab))
	d = modal.(*InitDialog)
	if d.focusIndex != 2 {
		t.Errorf("Tab from repo picker should advance to field 2 (Branch Prefix), got focusIndex=%d", d.focusIndex)
	}

	// Tab again should advance to field 3 (Base Branch).
	modal, _ = d.Update(sendSpecialKey(tea.KeyTab))
	d = modal.(*InitDialog)
	if d.focusIndex != 3 {
		t.Errorf("Tab should advance to field 3 (Base Branch), got focusIndex=%d", d.focusIndex)
	}

	// Tab from last field wraps to first field (Task ID).
	modal, _ = d.Update(sendSpecialKey(tea.KeyTab))
	d = modal.(*InitDialog)
	if d.focusIndex != 0 {
		t.Errorf("Tab from last field should wrap to field 0 (Task ID), got focusIndex=%d", d.focusIndex)
	}
}

// ── 19b. InitDialog Shift+Tab cycles fields backward ─────────────────────────────

func TestInitDialog_ShiftTab_CyclesFieldsBackward_WithRepoPicker(t *testing.T) {
	repos := makeTestRepos("alpha", "beta", "gamma")
	d := NewInitDialog("feature/", repos, 80, 24)

	// Focus field 1 (repo picker).
	d.focusField(1)

	// Shift+Tab should move to previous field (Task ID), NOT navigate within repo picker.
	modal, _ := d.Update(sendSpecialKey(tea.KeyShiftTab))
	d = modal.(*InitDialog)
	if d.focusIndex != 0 {
		t.Errorf("Shift+Tab from repo picker should move to field 0 (Task ID), got focusIndex=%d", d.focusIndex)
	}

	// Shift+Tab from first field wraps to last field (Base Branch).
	modal, _ = d.Update(sendSpecialKey(tea.KeyShiftTab))
	d = modal.(*InitDialog)
	if d.focusIndex != 3 {
		t.Errorf("Shift+Tab from field 0 should wrap to field 3 (Base Branch), got focusIndex=%d", d.focusIndex)
	}
}

// ── 20. RemoveDialog View contains dirty service warnings ────────────────────

func TestRemoveDialog_ViewContainsDirtyWarnings(t *testing.T) {
	d := NewRemoveTaskDialog("IN-7777", 2, []string{"my-service"})
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
	d := NewAddDialog("IN-5555", nil, 80, 24)
	if !strings.Contains(d.Title(), "IN-5555") {
		t.Errorf("Title() should contain taskID, got %q", d.Title())
	}
}

// ── 22. ConfigModal View contains config values ───────────────────────────────

func TestConfigModal_ViewContainsConfigValues(t *testing.T) {
	d := NewConfigModal(&config.Config{
		RootDir:          "/tmp/root",
		TasksRoot:        "/tmp/root/.tasks",
		BranchPrefix:     "feature/",
		Editor:           "code",
		DiscoveryDepth:   4,
		OutputPanelLines: 6,
		LogLevel:         "INFO",
	})

	view := stripAnsi(d.View())
	for _, want := range []string{
		"Configuration",
		"root_dir:",
		"/tmp/root",
		"tasks_root:",
		"/tmp/root/.tasks",
		"branch_prefix:",
		"feature/",
		"editor:",
		"code",
		"discovery_depth:",
		"4",
		"output_panel_lines:",
		"6",
		"log_level:",
		"INFO",
		"[Esc] close",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("ConfigModal.View() missing %q", want)
		}
	}
}

// ── 23. ConfigModal Esc closes ────────────────────────────────────────────────

func TestConfigModal_Esc_Closes(t *testing.T) {
	d := NewConfigModal(&config.Config{})

	_, cmd := d.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("Esc must return a cmd")
	}

	msg := execCmd(cmd)
	if _, ok := msg.(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", msg)
	}
}

// ── test helpers for list-based dialogs ─────────────────────────────────────

// newInitDialogWithRepos builds an InitDialog directly from a list of repo names
// for testing the repo picker functionality.
func newInitDialogWithRepos(names ...string) *InitDialog {
	repos := make([]domain.Repo, len(names))
	for i, name := range names {
		repos[i] = domain.Repo{Name: name}
	}
	d := NewInitDialog("feature/", repos, 80, 24)
	d.focusField(1) // focus the repo picker
	return d
}

// newAddDialogWithRepos builds an AddDialog with repos for testing.
// Note: This focuses the repo picker by default. Use NewAddDialog directly
// if you need text input focused.
func newAddDialogWithRepos(names ...string) *AddDialog {
	repos := make([]domain.Repo, len(names))
	for i, name := range names {
		repos[i] = domain.Repo{Name: name}
	}
	d := NewAddDialog("IN-1234", repos, 80, 24)
	d.focusField(1) // focus the repo picker
	return d
}

// getRepoNames returns the names of repos currently in the list.
func getRepoNames(d *InitDialog) []string {
	items := d.repoList.Items()
	names := make([]string, len(items))
	for i, item := range items {
		ri := item.(repoPickerItem)
		names[i] = ri.name
	}
	return names
}

// ── 24. InitDialog repo picker filter — list filters items ───────────────

func TestInitDialog_RepoPicker_Filter_Filters(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service", "beta-service", "gamma-service")

	// Enter filter mode and type "be" (matches "beta-service")
	m, _ := d.Update(sendKey("f"))
	d = m.(*InitDialog)

	// Type "be" into filter
	d.Update(sendKey("b"))
	d.Update(sendKey("e"))

	// Check that list is in filtering state
	if d.repoList.FilterState() != list.Filtering {
		t.Error("List should be in filtering state after typing filter")
	}

	// The filter value should be "be"
	if d.repoList.FilterValue() != "be" {
		t.Errorf("Filter value should be 'be', got %q", d.repoList.FilterValue())
	}
}

// ── 25. InitDialog repo picker filter — case-insensitive matching ─────────────

func TestInitDialog_RepoPicker_Filter_CaseInsensitive(t *testing.T) {
	d := newInitDialogWithRepos("MyFancyRepo", "other-repo")

	// Enter filter mode and type "fanc"
	m, _ := d.Update(sendKey("f"))
	d = m.(*InitDialog)
	d.Update(sendKey("f"))
	d.Update(sendKey("a"))
	d.Update(sendKey("n"))
	d.Update(sendKey("c"))

	// List should be filtering
	if d.repoList.FilterState() != list.Filtering {
		t.Error("List should be in filtering state")
	}
}

// ── 26. InitDialog repo picker filter — Esc clears filter ──

func TestInitDialog_RepoPicker_Esc_ClearsFilter(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service", "beta-service")

	// Enter filter mode and type something
	m, _ := d.Update(sendKey("f"))
	d = m.(*InitDialog)
	d.Update(sendKey("a"))
	d.Update(sendKey("l"))

	// Press ESC to clear filter
	m, _ = d.Update(sendSpecialKey(tea.KeyEsc))
	d = m.(*InitDialog)

	// Filter should be cleared
	if d.repoList.FilterState() != list.Unfiltered {
		t.Errorf("ESC should clear filter, got state %v", d.repoList.FilterState())
	}
}

// ── 27. InitDialog repo picker — Space toggles checkbox ────────────────────

func TestInitDialog_RepoPicker_Space_TogglesCheckbox(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service", "beta-service")

	// Toggle first item
	d.Update(sendKey(" "))

	// First item should be checked
	if !d.repoChecked["alpha-service"] {
		t.Error("First item should be checked after Space")
	}

	// Toggle again to uncheck
	d.Update(sendKey(" "))
	if d.repoChecked["alpha-service"] {
		t.Error("First item should be unchecked after second Space")
	}
}

// ── 28. InitDialog repo picker — j/k navigation ─────────────────────────────

func TestInitDialog_RepoPicker_JK_Navigation(t *testing.T) {
	d := newInitDialogWithRepos("alpha", "beta", "gamma")

	// Initial index should be 0
	if d.repoList.Index() != 0 {
		t.Errorf("Initial index should be 0, got %d", d.repoList.Index())
	}

	// Press 'j' to move down
	m, _ := d.Update(sendKey("j"))
	d = m.(*InitDialog)
	if d.repoList.Index() != 1 {
		t.Errorf("'j' should move cursor down, got index %d", d.repoList.Index())
	}

	// Press 'k' to move up
	m, _ = d.Update(sendKey("k"))
	d = m.(*InitDialog)
	if d.repoList.Index() != 0 {
		t.Errorf("'k' should move cursor up, got index %d", d.repoList.Index())
	}
}

// ── 29. InitDialog repo picker — h/l page navigation ────────────────────────

func TestInitDialog_RepoPicker_HL_PageNavigation(t *testing.T) {
	// Create enough repos to have multiple pages
	repos := make([]domain.Repo, 20)
	for i := range repos {
		repos[i] = domain.Repo{Name: fmt.Sprintf("service-%d", i)}
	}
	d := NewInitDialog("feature/", repos, 80, 24)
	d.focusField(1)

	// Initial page should be 0
	if d.repoList.Paginator.Page != 0 {
		t.Errorf("Initial page should be 0, got %d", d.repoList.Paginator.Page)
	}

	// Press 'l' to go to next page
	m, _ := d.Update(sendKey("l"))
	d = m.(*InitDialog)
	if d.repoList.Paginator.Page != 1 {
		t.Errorf("'l' should go to next page, got page %d", d.repoList.Paginator.Page)
	}

	// Press 'h' to go to previous page
	m, _ = d.Update(sendKey("h"))
	d = m.(*InitDialog)
	if d.repoList.Paginator.Page != 0 {
		t.Errorf("'h' should go to previous page, got page %d", d.repoList.Paginator.Page)
	}
}

// ── 30. InitDialog repo picker filter — submit includes all checked repos ──────

func TestInitDialog_RepoPicker_Filter_SubmitIncludesAllChecked(t *testing.T) {
	d := newInitDialogWithRepos("api-gateway", "backend-app", "frontend-ui")
	d.fields[0].SetValue("IN-0001")
	d.fields[2].SetValue("feature/")

	// Check first two items
	d.repoChecked["api-gateway"] = true
	d.repoChecked["backend-app"] = true

	// Navigate to last field and submit
	d.focusIndex = 3
	d.focusField(3)

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("Enter on last field must return a cmd")
	}
	sub, ok := execCmd(cmd).(SubmitInitMsg)
	if !ok {
		t.Fatalf("expected SubmitInitMsg, got %T", execCmd(cmd))
	}

	// api-gateway and backend-app are checked; frontend-ui is not — submit must include both
	if len(sub.Services) != 2 {
		t.Errorf("expected 2 services (api-gateway+backend-app), got %d: %v", len(sub.Services), sub.Services)
	}
}

// ── Task 1: filterMode state management tests ─────────────────────────────────

// TestInitDialog_FilterMode_UsingListFilterState verifies filter state is tracked via list.FilterState.
func TestInitDialog_FilterMode_UsingListFilterState(t *testing.T) {
	d := newInitDialogWithRepos("alpha")

	if d.repoList.FilterState() != list.Unfiltered {
		t.Error("FilterState should be Unfiltered by default")
	}
}

// TestInitDialog_F_Key_EntersFilterMode verifies 'f' key enters filter mode.
func TestInitDialog_F_Key_EntersFilterMode(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service")

	m, _ := d.Update(sendKey("f"))
	d = m.(*InitDialog)

	if d.repoList.FilterState() != list.Filtering {
		t.Error("'f' key should enter filter mode when repo picker is focused")
	}
}

// TestInitDialog_F_Key_NotInRepoPicker_TypesIntoTextInput verifies 'f' types into
// text input when text input is focused (not entering filter mode).
func TestInitDialog_F_Key_NotInRepoPicker_TypesIntoTextInput(t *testing.T) {
	d := NewInitDialog("feature/", nil, 80, 24)
	// Text input is focused by default (field 0)

	m, _ := d.Update(sendKey("f"))
	d = m.(*InitDialog)

	if d.repoList.FilterState() == list.Filtering {
		t.Error("'f' key should not enter filter mode when text input is focused")
	}
	if d.fields[0].Value() != "f" {
		t.Errorf("'f' key should type into text input, got %q", d.fields[0].Value())
	}
}

// TestInitDialog_Esc_InFilterMode_ClearsFilter verifies ESC in filter mode clears filter.
func TestInitDialog_Esc_InFilterMode_ClearsFilter(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service")

	// Enter filter mode
	m, _ := d.Update(sendKey("f"))
	d = m.(*InitDialog)
	d.Update(sendKey("a"))
	d.Update(sendKey("l"))

	// Press ESC
	m, _ = d.Update(sendSpecialKey(tea.KeyEsc))
	d = m.(*InitDialog)

	if d.repoList.FilterState() != list.Unfiltered {
		t.Error("ESC in filter mode should clear filter and exit filter mode")
	}
}

// TestInitDialog_Esc_NotInFilterMode_NoFilter_ClosesDialog verifies ESC outside
// filter mode with no filter closes the dialog.
func TestInitDialog_Esc_NotInFilterMode_NoFilter_ClosesDialog(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service")

	_, cmd := d.Update(sendSpecialKey(tea.KeyEsc))

	if cmd == nil {
		t.Fatal("ESC with no filter should close dialog")
	}
	msg := execCmd(cmd)
	if _, ok := msg.(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", msg)
	}
}

// ── Task 2: AddDialog filter mode tests ────────────────────────────────────────

// TestAddDialog_Tab_TogglesToRepoPicker verifies TAB toggles from text input to repo picker.
func TestAddDialog_Tab_TogglesToRepoPicker(t *testing.T) {
	repos := []domain.Repo{{Name: "alpha-service"}, {Name: "beta-service"}}
	d := NewAddDialog("IN-1234", repos, 80, 24)
	// Text input is focused by default (field 0).

	if d.repoPickerFocused {
		t.Fatal("expected repoPickerFocused=false initially")
	}

	// Tab should toggle to repo picker.
	m, _ := d.Update(sendSpecialKey(tea.KeyTab))
	d = m.(*AddDialog)

	if !d.repoPickerFocused {
		t.Error("Tab should toggle focus to repo picker")
	}
}

// TestAddDialog_Tab_FromRepoPicker_TogglesToTextInput verifies TAB toggles from repo picker to text input.
func TestAddDialog_Tab_FromRepoPicker_TogglesToTextInput(t *testing.T) {
	d := newAddDialogWithRepos("alpha-service", "beta-service")
	d.focusField(1) // focus repo picker

	if !d.repoPickerFocused {
		t.Fatal("expected repoPickerFocused=true after focusField(1)")
	}

	// Tab should toggle back to text input.
	m, _ := d.Update(sendSpecialKey(tea.KeyTab))
	d = m.(*AddDialog)

	if d.repoPickerFocused {
		t.Error("Tab should toggle focus back to text input")
	}
}

// TestAddDialog_ShiftTab_TogglesToRepoPicker verifies Shift+TAB toggles from text input to repo picker.
func TestAddDialog_ShiftTab_TogglesToRepoPicker(t *testing.T) {
	repos := []domain.Repo{{Name: "alpha-service"}, {Name: "beta-service"}}
	d := NewAddDialog("IN-1234", repos, 80, 24)

	// Text input is focused by default.
	if d.repoPickerFocused {
		t.Fatal("expected repoPickerFocused=false initially")
	}

	// Shift+Tab should toggle to repo picker.
	m, _ := d.Update(sendSpecialKey(tea.KeyShiftTab))
	d = m.(*AddDialog)

	if !d.repoPickerFocused {
		t.Error("Shift+Tab should toggle focus to repo picker")
	}
}

// TestAddDialog_ShiftTab_FromRepoPicker_TogglesToTextInput verifies Shift+TAB toggles from repo picker to text input.
func TestAddDialog_ShiftTab_FromRepoPicker_TogglesToTextInput(t *testing.T) {
	d := newAddDialogWithRepos("alpha-service", "beta-service")
	d.focusField(1) // focus repo picker

	if !d.repoPickerFocused {
		t.Fatal("expected repoPickerFocused=true after focusField(1)")
	}

	// Shift+Tab should toggle back to text input.
	m, _ := d.Update(sendSpecialKey(tea.KeyShiftTab))
	d = m.(*AddDialog)

	if d.repoPickerFocused {
		t.Error("Shift+Tab should toggle focus back to text input")
	}
}

// TestInitDialog_Tab_DoesNotNavigateWithinRepoPicker verifies TAB no longer navigates within repo picker.
func TestInitDialog_Tab_DoesNotNavigateWithinRepoPicker(t *testing.T) {
	d := newInitDialogWithRepos("alpha", "beta", "gamma")

	initialIndex := d.repoList.Index()

	// Tab should NOT change list index - it should change focusIndex instead.
	m, _ := d.Update(sendSpecialKey(tea.KeyTab))
	d = m.(*InitDialog)

	if d.repoList.Index() != initialIndex {
		t.Errorf("Tab should not change list index, got %d (was %d)", d.repoList.Index(), initialIndex)
	}
	if d.focusIndex == 1 {
		t.Error("Tab should move focus away from repo picker (field 1)")
	}
}

// TestAddDialog_Tab_DoesNotNavigateWithinRepoPicker verifies TAB no longer navigates within repo picker.
func TestAddDialog_Tab_DoesNotNavigateWithinRepoPicker(t *testing.T) {
	d := newAddDialogWithRepos("alpha-service", "beta-service", "gamma-service")

	initialIndex := d.repoList.Index()

	// Tab should NOT change list index - it should toggle focus to text input.
	m, _ := d.Update(sendSpecialKey(tea.KeyTab))
	d = m.(*AddDialog)

	if d.repoList.Index() != initialIndex {
		t.Errorf("Tab should not change list index, got %d (was %d)", d.repoList.Index(), initialIndex)
	}
	if d.repoPickerFocused {
		t.Error("Tab should toggle focus away from repo picker")
	}
}

// ── Task 3: Filter mode entry/exit tests ────────────────────────────────────────

// TestInitDialog_Enter_InFilterMode_ExitsAndKeepsFilter verifies ENTER in filter mode
// exits filter mode and keeps the filter active.
func TestInitDialog_Enter_InFilterMode_ExitsAndKeepsFilter(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service")

	// Enter filter mode and type
	m, _ := d.Update(sendKey("f"))
	d = m.(*InitDialog)
	d.Update(sendKey("a"))
	d.Update(sendKey("l"))

	// Press Enter to exit filter mode
	m, _ = d.Update(sendSpecialKey(tea.KeyEnter))
	d = m.(*InitDialog)

	// Filter should be applied (not filtering anymore, but filter applied)
	if d.repoList.FilterState() == list.Filtering {
		t.Error("ENTER in filter mode should exit filter mode")
	}
}

// TestInitDialog_J_InFilterMode_TypesIntoFilter verifies 'j' in filter mode types into filter.
func TestInitDialog_J_InFilterMode_TypesIntoFilter(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service")

	// Enter filter mode
	m, _ := d.Update(sendKey("f"))
	d = m.(*InitDialog)

	// Type 'j' - should go to filter, not navigate
	d.Update(sendKey("j"))

	if d.repoList.FilterValue() != "j" {
		t.Errorf("'j' in filter mode should append to filter, got %q", d.repoList.FilterValue())
	}
}

// TestInitDialog_K_InFilterMode_TypesIntoFilter verifies 'k' in filter mode types into filter.
func TestInitDialog_K_InFilterMode_TypesIntoFilter(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service")

	// Enter filter mode
	m, _ := d.Update(sendKey("f"))
	d = m.(*InitDialog)

	// Type 'k' - should go to filter, not navigate
	d.Update(sendKey("k"))

	if d.repoList.FilterValue() != "k" {
		t.Errorf("'k' in filter mode should append to filter, got %q", d.repoList.FilterValue())
	}
}

// TestInitDialog_J_InNormalMode_NavigatesDown verifies 'j' in normal mode navigates down.
func TestInitDialog_J_InNormalMode_NavigatesDown(t *testing.T) {
	d := newInitDialogWithRepos("alpha", "beta", "gamma")

	if d.repoList.Index() != 0 {
		t.Fatalf("expected initial cursor at 0, got %d", d.repoList.Index())
	}

	m, _ := d.Update(sendKey("j"))
	d = m.(*InitDialog)

	if d.repoList.Index() != 1 {
		t.Errorf("'j' in normal mode should move cursor down, got %d", d.repoList.Index())
	}
}

// TestInitDialog_K_InNormalMode_NavigatesUp verifies 'k' in normal mode navigates up.
func TestInitDialog_K_InNormalMode_NavigatesUp(t *testing.T) {
	d := newInitDialogWithRepos("alpha", "beta", "gamma")
	d.repoList.Select(1) // Start at second item

	m, _ := d.Update(sendKey("k"))
	d = m.(*InitDialog)

	if d.repoList.Index() != 0 {
		t.Errorf("'k' in normal mode should move cursor up, got %d", d.repoList.Index())
	}
}

// TestInitDialog_H_InNormalMode_GoToPrevPage verifies 'h' goes to previous page when repo picker is focused.
func TestInitDialog_H_InNormalMode_GoToPrevPage(t *testing.T) {
	// Create enough repos to have multiple pages
	repos := make([]domain.Repo, 20)
	for i := range repos {
		repos[i] = domain.Repo{Name: fmt.Sprintf("service-%d", i)}
	}
	d := NewInitDialog("feature/", repos, 80, 24)
	d.focusField(1) // focus repo picker

	// Move to page 2
	d.repoList.Paginator.Page = 1

	if d.repoList.Paginator.Page != 1 {
		t.Fatalf("expected to start on page 1 (0-indexed), got page %d", d.repoList.Paginator.Page)
	}

	m, _ := d.Update(sendKey("h"))
	d = m.(*InitDialog)

	if d.repoList.Paginator.Page != 0 {
		t.Errorf("'h' should go to previous page, got page %d", d.repoList.Paginator.Page)
	}
}

// TestInitDialog_L_InNormalMode_GoToNextPage verifies 'l' goes to next page when repo picker is focused.
func TestInitDialog_L_InNormalMode_GoToNextPage(t *testing.T) {
	// Create enough repos to have multiple pages
	repos := make([]domain.Repo, 20)
	for i := range repos {
		repos[i] = domain.Repo{Name: fmt.Sprintf("service-%d", i)}
	}
	d := NewInitDialog("feature/", repos, 80, 24)
	d.focusField(1) // focus repo picker

	if d.repoList.Paginator.Page != 0 {
		t.Fatalf("expected to start on page 0, got page %d", d.repoList.Paginator.Page)
	}

	m, _ := d.Update(sendKey("l"))
	d = m.(*InitDialog)

	if d.repoList.Paginator.Page != 1 {
		t.Errorf("'l' should go to next page, got page %d", d.repoList.Paginator.Page)
	}
}

// TestAddDialog_J_InNormalMode_NavigatesDown verifies 'j' in normal mode navigates down.
func TestAddDialog_J_InNormalMode_NavigatesDown(t *testing.T) {
	d := newAddDialogWithRepos("alpha", "beta", "gamma")

	if d.repoList.Index() != 0 {
		t.Fatalf("expected initial cursor at 0, got %d", d.repoList.Index())
	}

	m, _ := d.Update(sendKey("j"))
	d = m.(*AddDialog)

	if d.repoList.Index() != 1 {
		t.Errorf("'j' in normal mode should move cursor down, got %d", d.repoList.Index())
	}
}

// TestAddDialog_K_InNormalMode_NavigatesUp verifies 'k' in normal mode navigates up.
func TestAddDialog_K_InNormalMode_NavigatesUp(t *testing.T) {
	d := newAddDialogWithRepos("alpha", "beta", "gamma")
	d.repoList.Select(1) // Start at second item

	m, _ := d.Update(sendKey("k"))
	d = m.(*AddDialog)

	if d.repoList.Index() != 0 {
		t.Errorf("'k' in normal mode should move cursor up, got %d", d.repoList.Index())
	}
}

// TestAddDialog_H_InNormalMode_GoToPrevPage verifies 'h' goes to previous page when repo picker is focused.
func TestAddDialog_H_InNormalMode_GoToPrevPage(t *testing.T) {
	// Create enough repos to have multiple pages
	repos := make([]domain.Repo, 20)
	for i := range repos {
		repos[i] = domain.Repo{Name: fmt.Sprintf("service-%d", i)}
	}
	d := NewAddDialog("IN-1234", repos, 80, 24)
	d.focusField(1) // focus repo picker

	// Move to page 2
	d.repoList.Paginator.Page = 1

	if d.repoList.Paginator.Page != 1 {
		t.Fatalf("expected to start on page 1 (0-indexed), got page %d", d.repoList.Paginator.Page)
	}

	m, _ := d.Update(sendKey("h"))
	d = m.(*AddDialog)

	if d.repoList.Paginator.Page != 0 {
		t.Errorf("'h' should go to previous page, got page %d", d.repoList.Paginator.Page)
	}
}

// TestAddDialog_L_InNormalMode_GoToNextPage verifies 'l' goes to next page when repo picker is focused.
func TestAddDialog_L_InNormalMode_GoToNextPage(t *testing.T) {
	// Create enough repos to have multiple pages
	repos := make([]domain.Repo, 20)
	for i := range repos {
		repos[i] = domain.Repo{Name: fmt.Sprintf("service-%d", i)}
	}
	d := NewAddDialog("IN-1234", repos, 80, 24)
	d.focusField(1) // focus repo picker

	if d.repoList.Paginator.Page != 0 {
		t.Fatalf("expected to start on page 0, got page %d", d.repoList.Paginator.Page)
	}

	m, _ := d.Update(sendKey("l"))
	d = m.(*AddDialog)

	if d.repoList.Paginator.Page != 1 {
		t.Errorf("'l' should go to next page, got page %d", d.repoList.Paginator.Page)
	}
}

// TestAddDialog_F_Key_EntersFilterMode verifies 'f' key enters filter mode.
func TestAddDialog_F_Key_EntersFilterMode(t *testing.T) {
	d := newAddDialogWithRepos("alpha-service")

	m, _ := d.Update(sendKey("f"))
	d = m.(*AddDialog)

	if d.repoList.FilterState() != list.Filtering {
		t.Error("'f' key should enter filter mode when repo picker is focused")
	}
}

// TestAddDialog_F_Key_NotInRepoPicker_TypesIntoTextInput verifies 'f' types into
// text input when text input is focused (not entering filter mode).
func TestAddDialog_F_Key_NotInRepoPicker_TypesIntoTextInput(t *testing.T) {
	d := NewAddDialog("IN-1234", []domain.Repo{{Name: "alpha-service"}}, 80, 24)
	// Text input is focused by default (field 0)

	m, _ := d.Update(sendKey("f"))
	d = m.(*AddDialog)

	if d.repoList.FilterState() == list.Filtering {
		t.Error("'f' key should not enter filter mode when text input is focused")
	}
	if d.input.Value() != "f" {
		t.Errorf("'f' key should type into text input, got %q", d.input.Value())
	}
}

// TestAddDialog_Esc_InFilterMode_ClearsFilter verifies ESC in filter mode clears filter.
func TestAddDialog_Esc_InFilterMode_ClearsFilter(t *testing.T) {
	d := newAddDialogWithRepos("alpha-service")

	// Enter filter mode
	m, _ := d.Update(sendKey("f"))
	d = m.(*AddDialog)
	d.Update(sendKey("a"))
	d.Update(sendKey("l"))

	// Press ESC
	m, _ = d.Update(sendSpecialKey(tea.KeyEsc))
	d = m.(*AddDialog)

	if d.repoList.FilterState() != list.Unfiltered {
		t.Error("ESC in filter mode should clear filter and exit filter mode")
	}
}

// TestAddDialog_Esc_NotInFilterMode_NoFilter_ClosesDialog verifies ESC outside
// filter mode with no filter closes the dialog.
func TestAddDialog_Esc_NotInFilterMode_NoFilter_ClosesDialog(t *testing.T) {
	d := newAddDialogWithRepos("alpha-service")

	_, cmd := d.Update(sendSpecialKey(tea.KeyEsc))

	if cmd == nil {
		t.Fatal("ESC with no filter should close dialog")
	}
	msg := execCmd(cmd)
	if _, ok := msg.(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", msg)
	}
}

// ── Task 4: View() shows filter mode indicator ───────────────────────────────────

// TestInitDialog_View_ShowsFilterModeIndicator verifies [FILTER] prefix appears when in filter mode.
func TestInitDialog_View_ShowsFilterModeIndicator(t *testing.T) {
	d := newInitDialogWithRepos("alpha", "beta")

	// Enter filter mode
	m, _ := d.Update(sendKey("f"))
	d = m.(*InitDialog)

	view := d.View()
	if !strings.Contains(view, "[FILTER]") {
		t.Error("View should contain [FILTER] when in filter mode")
	}
}

// TestInitDialog_View_NoFilterModeIndicatorWhenNotInFilterMode verifies [FILTER] prefix does not appear when not in filter mode.
func TestInitDialog_View_NoFilterModeIndicatorWhenNotInFilterMode(t *testing.T) {
	d := newInitDialogWithRepos("alpha", "beta")

	view := d.View()
	if strings.Contains(view, "[FILTER]") {
		t.Error("View should NOT contain [FILTER] when not in filter mode")
	}
}

// TestInitDialog_View_ShowsFilterModeHints verifies hint bar shows filter mode hints when in filter mode.
func TestInitDialog_View_ShowsFilterModeHints(t *testing.T) {
	d := newInitDialogWithRepos("alpha", "beta")

	// Enter filter mode
	m, _ := d.Update(sendKey("f"))
	d = m.(*InitDialog)

	view := d.View()
	if !strings.Contains(view, "[Type] filter") {
		t.Error("View should contain '[Type] filter' hint when in filter mode")
	}
	if !strings.Contains(view, "[Backspace] delete") {
		t.Error("View should contain '[Backspace] delete' hint when in filter mode")
	}
}

// TestInitDialog_View_ShowsNormalModeHints verifies hint bar shows normal mode hints when not in filter mode.
func TestInitDialog_View_ShowsNormalModeHints(t *testing.T) {
	d := newInitDialogWithRepos("alpha", "beta")

	view := d.View()
	if !strings.Contains(view, "[Space] toggle") {
		t.Error("View should contain '[Space] toggle' hint when not in filter mode")
	}
	if !strings.Contains(view, "[j/k] navigate") {
		t.Error("View should contain '[j/k] navigate' hint when not in filter mode")
	}
	if !strings.Contains(view, "[f] filter") {
		t.Error("View should contain '[f] filter' hint when not in filter mode")
	}
}

// TestAddDialog_View_ShowsFilterModeIndicator verifies [FILTER] prefix appears when in filter mode.
func TestAddDialog_View_ShowsFilterModeIndicator(t *testing.T) {
	d := newAddDialogWithRepos("alpha", "beta")

	// Enter filter mode
	m, _ := d.Update(sendKey("f"))
	d = m.(*AddDialog)

	view := d.View()
	if !strings.Contains(view, "[FILTER]") {
		t.Error("View should contain [FILTER] when in filter mode")
	}
}

// TestAddDialog_View_NoFilterModeIndicatorWhenNotInFilterMode verifies [FILTER] prefix does not appear when not in filter mode.
func TestAddDialog_View_NoFilterModeIndicatorWhenNotInFilterMode(t *testing.T) {
	d := newAddDialogWithRepos("alpha", "beta")

	view := d.View()
	if strings.Contains(view, "[FILTER]") {
		t.Error("View should NOT contain [FILTER] when not in filter mode")
	}
}

// TestAddDialog_View_ShowsFilterModeHints verifies hint bar shows filter mode hints when in filter mode.
func TestAddDialog_View_ShowsFilterModeHints(t *testing.T) {
	d := newAddDialogWithRepos("alpha", "beta")

	// Enter filter mode
	m, _ := d.Update(sendKey("f"))
	d = m.(*AddDialog)

	view := d.View()
	if !strings.Contains(view, "[Type] filter") {
		t.Error("View should contain '[Type] filter' hint when in filter mode")
	}
	if !strings.Contains(view, "[Backspace] delete") {
		t.Error("View should contain '[Backspace] delete' hint when in filter mode")
	}
}

// TestAddDialog_View_ShowsNormalModeHints verifies hint bar shows normal mode hints when not in filter mode.
func TestAddDialog_View_ShowsNormalModeHints(t *testing.T) {
	d := newAddDialogWithRepos("alpha", "beta")

	view := d.View()
	if !strings.Contains(view, "[Space] toggle") {
		t.Error("View should contain '[Space] toggle' hint when not in filter mode")
	}
	if !strings.Contains(view, "[j/k] navigate") {
		t.Error("View should contain '[j/k] navigate' hint when not in filter mode")
	}
	if !strings.Contains(view, "[f] filter") {
		t.Error("View should contain '[f] filter' hint when not in filter mode")
	}
}

// TestInitDialog_RepoPicker_PaginationShowsDots verifies pagination dots appear when multiple pages.
func TestInitDialog_RepoPicker_PaginationShowsDots(t *testing.T) {
	// Create enough repos to have multiple pages
	repos := make([]domain.Repo, 20)
	for i := range repos {
		repos[i] = domain.Repo{Name: fmt.Sprintf("service-%d", i)}
	}
	d := NewInitDialog("feature/", repos, 80, 24)
	d.focusField(1)

	view := d.View()
	// Pagination dots should appear (• or ○)
	if !strings.Contains(view, "•") && !strings.Contains(view, "○") {
		t.Error("View should contain pagination dots when multiple pages")
	}
}

// TestAddDialog_RepoPicker_PaginationShowsDots verifies pagination dots appear when multiple pages.
func TestAddDialog_RepoPicker_PaginationShowsDots(t *testing.T) {
	// Create enough repos to have multiple pages
	repos := make([]domain.Repo, 20)
	for i := range repos {
		repos[i] = domain.Repo{Name: fmt.Sprintf("service-%d", i)}
	}
	d := NewAddDialog("IN-1234", repos, 80, 24)
	d.focusField(1)

	view := d.View()
	// Pagination dots should appear (• or ○)
	if !strings.Contains(view, "•") && !strings.Contains(view, "○") {
		t.Error("View should contain pagination dots when multiple pages")
	}
}

// TestInitDialog_RepoPicker_CheckboxRenders verifies checkbox style is rendered.
func TestInitDialog_RepoPicker_CheckboxRenders(t *testing.T) {
	d := newInitDialogWithRepos("alpha", "beta")

	view := d.View()
	// Should show unchecked checkbox
	if !strings.Contains(view, "[ ]") {
		t.Error("View should contain unchecked checkbox '[ ]'")
	}
}

// TestInitDialog_RepoPicker_CheckboxToggles verifies checkbox toggles on space.
func TestInitDialog_RepoPicker_CheckboxToggles(t *testing.T) {
	d := newInitDialogWithRepos("alpha", "beta")

	// Toggle first item
	d.Update(sendKey(" "))

	if !d.repoChecked["alpha"] {
		t.Error("First item should be checked after Space")
	}

	// Toggle again
	d.Update(sendKey(" "))

	if d.repoChecked["alpha"] {
		t.Error("First item should be unchecked after second Space")
	}
}

// TestAddDialog_RepoPicker_CheckboxRenders verifies checkbox style is rendered.
func TestAddDialog_RepoPicker_CheckboxRenders(t *testing.T) {
	d := newAddDialogWithRepos("alpha", "beta")

	view := d.View()
	// Should show unchecked checkbox
	if !strings.Contains(view, "[ ]") {
		t.Error("View should contain unchecked checkbox '[ ]'")
	}
}

// TestAddDialog_RepoPicker_CheckboxToggles verifies checkbox toggles on space.
func TestAddDialog_RepoPicker_CheckboxToggles(t *testing.T) {
	d := newAddDialogWithRepos("alpha", "beta")

	// Toggle first item
	d.Update(sendKey(" "))

	if !d.repoChecked["alpha"] {
		t.Error("First item should be checked after Space")
	}

	// Toggle again
	d.Update(sendKey(" "))

	if d.repoChecked["alpha"] {
		t.Error("First item should be unchecked after second Space")
	}
}

// ── Cursor clamping tests for filtered items ─────────────────────────────────────

// visibleNames returns the names of repos currently visible after filtering.
func visibleNames(d *InitDialog) []string {
	allItems := d.repoList.Items()
	filterValue := strings.ToLower(d.repoList.FilterValue())

	var names []string
	for _, item := range allItems {
		ri, ok := item.(repoPickerItem)
		if !ok {
			continue
		}
		if filterValue == "" || strings.Contains(strings.ToLower(ri.name), filterValue) {
			names = append(names, ri.name)
		}
	}
	return names
}

// visibleNamesAddDialog returns the names of repos currently visible after filtering for AddDialog.
func visibleNamesAddDialog(d *AddDialog) []string {
	allItems := d.repoList.Items()
	filterValue := strings.ToLower(d.repoList.FilterValue())

	var names []string
	for _, item := range allItems {
		ri, ok := item.(repoPickerItem)
		if !ok {
			continue
		}
		if filterValue == "" || strings.Contains(strings.ToLower(ri.name), filterValue) {
			names = append(names, ri.name)
		}
	}
	return names
}

// typeIntoFilter enters filter mode and types the given characters.
func typeIntoFilter(d *InitDialog, chars string) {
	d.Update(sendKey("f")) // enter filter mode
	for _, c := range chars {
		d.Update(sendKey(string(c)))
	}
}

// typeIntoFilterAddDialog enters filter mode and types the given characters for AddDialog.
func typeIntoFilterAddDialog(d *AddDialog, chars string) {
	d.Update(sendKey("f")) // enter filter mode
	for _, c := range chars {
		d.Update(sendKey(string(c)))
	}
}

// TestInitDialog_CursorClamp_NavigationWithEmptyFilter verifies navigation works normally with no filter.
func TestInitDialog_CursorClamp_NavigationWithEmptyFilter(t *testing.T) {
	d := newInitDialogWithRepos("alpha", "beta", "gamma", "delta")

	// No filter - all items visible
	if len(visibleNames(d)) != 4 {
		t.Errorf("expected 4 visible items, got %d", len(visibleNames(d)))
	}

	// Navigate down through all items
	d.Update(sendKey("j"))
	if d.repoList.Index() != 1 {
		t.Errorf("expected cursor at 1, got %d", d.repoList.Index())
	}

	d.Update(sendKey("j"))
	if d.repoList.Index() != 2 {
		t.Errorf("expected cursor at 2, got %d", d.repoList.Index())
	}

	d.Update(sendKey("j"))
	if d.repoList.Index() != 3 {
		t.Errorf("expected cursor at 3, got %d", d.repoList.Index())
	}

	// Navigate back up
	d.Update(sendKey("k"))
	if d.repoList.Index() != 2 {
		t.Errorf("expected cursor at 2, got %d", d.repoList.Index())
	}
}

// TestInitDialog_CursorClamp_FilterReducesVisibleItems verifies cursor clamps when filter reduces items.
// The clamping happens during navigation (j/k), not during filter application.
func TestInitDialog_CursorClamp_FilterReducesVisibleItems(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service", "beta-service", "gamma-service", "delta-other")

	// Enter filter mode and type "service" (matches only 3 items: alpha-service, beta-service, gamma-service)
	typeIntoFilter(d, "service")
	d.Update(sendSpecialKey(tea.KeyEnter)) // exit filter mode, keep filter applied

	// The list library may reset cursor to 0 after filter application.
	// Navigate to the last visible item (index 2 for the 3 filtered items)
	d.Update(sendKey("j")) // move to 1
	d.Update(sendKey("j")) // move to 2

	if d.repoList.Index() != 2 {
		t.Fatalf("expected cursor at 2 (last visible), got %d", d.repoList.Index())
	}

	// Navigate down - should stay at last visible item (clamped)
	d.Update(sendKey("j"))
	if d.repoList.Index() != 2 {
		t.Errorf("cursor should stay at 2 (clamped), got %d", d.repoList.Index())
	}

	// Navigate up - should work normally
	d.Update(sendKey("k"))
	if d.repoList.Index() != 1 {
		t.Errorf("cursor should be at 1, got %d", d.repoList.Index())
	}
}

// TestInitDialog_CursorClamp_FilterResultsInSingleItem verifies cursor clamps to single item.
func TestInitDialog_CursorClamp_FilterResultsInSingleItem(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service", "beta-service", "gamma-service")

	// Move cursor to last item (index 2)
	d.repoList.Select(2)
	if d.repoList.Index() != 2 {
		t.Fatalf("expected cursor at 2, got %d", d.repoList.Index())
	}

	// Enter filter mode and type "alpha" (matches only 1 item)
	typeIntoFilter(d, "alpha")
	d.Update(sendSpecialKey(tea.KeyEnter)) // exit filter mode, keep filter applied

	// After filter, only 1 item visible, cursor should be clamped to index 0
	if d.repoList.Index() != 0 {
		t.Errorf("cursor should be clamped to 0 (only visible item), got %d", d.repoList.Index())
	}

	// Navigate down - should stay at 0
	d.Update(sendKey("j"))
	if d.repoList.Index() != 0 {
		t.Errorf("cursor should stay at 0, got %d", d.repoList.Index())
	}

	// Navigate up - should stay at 0
	d.Update(sendKey("k"))
	if d.repoList.Index() != 0 {
		t.Errorf("cursor should stay at 0, got %d", d.repoList.Index())
	}
}

// TestInitDialog_CursorClamp_FilterResultsInNoItems verifies behavior when filter matches nothing.
func TestInitDialog_CursorClamp_FilterResultsInNoItems(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service", "beta-service", "gamma-service")

	// Move cursor to index 1
	d.repoList.Select(1)
	if d.repoList.Index() != 1 {
		t.Fatalf("expected cursor at 1, got %d", d.repoList.Index())
	}

	// Enter filter mode and type "xyz" (matches no items)
	typeIntoFilter(d, "xyz")
	d.Update(sendSpecialKey(tea.KeyEnter)) // exit filter mode, keep filter applied

	// After filter, 0 items visible - cursor should not crash
	// The clamp function has: if filteredCount > 0 && d.repoList.Index() >= filteredCount
	// With filteredCount == 0, the condition is false, so cursor stays where it was
	// This is acceptable behavior - user sees "No repos match the filter"

	// Navigate down - should not crash
	d.Update(sendKey("j"))
	// Cursor position is undefined when no items, but should not panic

	// Navigate up - should not crash
	d.Update(sendKey("k"))
	// Cursor position is undefined when no items, but should not panic
}

// TestInitDialog_CursorClamp_NavigationAfterClearingFilter verifies cursor after clearing filter.
func TestInitDialog_CursorClamp_NavigationAfterClearingFilter(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service", "beta-service", "gamma-service", "delta-other")

	// Apply filter that matches 3 items
	typeIntoFilter(d, "service")
	d.Update(sendSpecialKey(tea.KeyEnter)) // exit filter mode, keep filter applied

	// Cursor should be clamped
	if d.repoList.Index() > 2 {
		t.Errorf("cursor should be clamped to <= 2, got %d", d.repoList.Index())
	}

	// Clear filter
	d.Update(sendSpecialKey(tea.KeyEsc))

	// Now all 4 items visible, navigation should work normally
	if d.repoList.Index() > 2 {
		t.Errorf("cursor should still be <= 2 after clearing filter, got %d", d.repoList.Index())
	}

	// Navigate down - should work
	d.Update(sendKey("j"))
	// Cursor should move down (or stay if already at last visible from before)
}

// TestInitDialog_CursorClamp_DownFromLastFilteredItem verifies down from last filtered item stays in bounds.
func TestInitDialog_CursorClamp_DownFromLastFilteredItem(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service", "beta-service", "gamma-service")

	// Apply filter matching all items
	typeIntoFilter(d, "service")
	d.Update(sendSpecialKey(tea.KeyEnter)) // exit filter mode, keep filter applied

	// Move to last item
	d.repoList.Select(2)

	// Try to navigate down - should stay at last item
	d.Update(sendKey("j"))
	if d.repoList.Index() != 2 {
		t.Errorf("cursor should stay at 2 (last item), got %d", d.repoList.Index())
	}
}

// TestInitDialog_CursorClamp_UpFromFirstItem verifies up from first item stays in bounds.
func TestInitDialog_CursorClamp_UpFromFirstItem(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service", "beta-service", "gamma-service")

	// Apply filter
	typeIntoFilter(d, "service")
	d.Update(sendSpecialKey(tea.KeyEnter)) // exit filter mode, keep filter applied

	// Cursor at first item (default)
	if d.repoList.Index() != 0 {
		t.Fatalf("expected cursor at 0, got %d", d.repoList.Index())
	}

	// Try to navigate up - should stay at first item
	d.Update(sendKey("k"))
	if d.repoList.Index() != 0 {
		t.Errorf("cursor should stay at 0 (first item), got %d", d.repoList.Index())
	}
}

// ── AddDialog cursor clamping tests ──────────────────────────────────────────────

// TestAddDialog_CursorClamp_NavigationWithEmptyFilter verifies navigation works normally with no filter.
func TestAddDialog_CursorClamp_NavigationWithEmptyFilter(t *testing.T) {
	d := newAddDialogWithRepos("alpha", "beta", "gamma", "delta")

	// No filter - all items visible
	if len(visibleNamesAddDialog(d)) != 4 {
		t.Errorf("expected 4 visible items, got %d", len(visibleNamesAddDialog(d)))
	}

	// Navigate down through all items
	d.Update(sendKey("j"))
	if d.repoList.Index() != 1 {
		t.Errorf("expected cursor at 1, got %d", d.repoList.Index())
	}

	d.Update(sendKey("j"))
	if d.repoList.Index() != 2 {
		t.Errorf("expected cursor at 2, got %d", d.repoList.Index())
	}

	d.Update(sendKey("j"))
	if d.repoList.Index() != 3 {
		t.Errorf("expected cursor at 3, got %d", d.repoList.Index())
	}

	// Navigate back up
	d.Update(sendKey("k"))
	if d.repoList.Index() != 2 {
		t.Errorf("expected cursor at 2, got %d", d.repoList.Index())
	}
}

// TestAddDialog_CursorClamp_FilterReducesVisibleItems verifies cursor clamps when filter reduces items.
// The clamping happens during navigation (j/k), not during filter application.
func TestAddDialog_CursorClamp_FilterReducesVisibleItems(t *testing.T) {
	d := newAddDialogWithRepos("alpha-service", "beta-service", "gamma-service", "delta-other")

	// Enter filter mode and type "service" (matches only 3 items)
	typeIntoFilterAddDialog(d, "service")
	d.Update(sendSpecialKey(tea.KeyEnter)) // exit filter mode, keep filter applied

	// Navigate to the last visible item (index 2 for the 3 filtered items)
	d.Update(sendKey("j")) // move to 1
	d.Update(sendKey("j")) // move to 2

	if d.repoList.Index() != 2 {
		t.Fatalf("expected cursor at 2 (last visible), got %d", d.repoList.Index())
	}

	// Navigate down - should stay at last visible item (clamped)
	d.Update(sendKey("j"))
	if d.repoList.Index() != 2 {
		t.Errorf("cursor should stay at 2 (clamped), got %d", d.repoList.Index())
	}

	// Navigate up - should work normally
	d.Update(sendKey("k"))
	if d.repoList.Index() != 1 {
		t.Errorf("cursor should be at 1, got %d", d.repoList.Index())
	}
}

// TestAddDialog_CursorClamp_FilterResultsInSingleItem verifies cursor clamps to single item.
func TestAddDialog_CursorClamp_FilterResultsInSingleItem(t *testing.T) {
	d := newAddDialogWithRepos("alpha-service", "beta-service", "gamma-service")

	// Move cursor to last item (index 2)
	d.repoList.Select(2)
	if d.repoList.Index() != 2 {
		t.Fatalf("expected cursor at 2, got %d", d.repoList.Index())
	}

	// Enter filter mode and type "alpha" (matches only 1 item)
	typeIntoFilterAddDialog(d, "alpha")
	d.Update(sendSpecialKey(tea.KeyEnter)) // exit filter mode, keep filter applied

	// After filter, only 1 item visible, cursor should be clamped to index 0
	if d.repoList.Index() != 0 {
		t.Errorf("cursor should be clamped to 0 (only visible item), got %d", d.repoList.Index())
	}

	// Navigate down - should stay at 0
	d.Update(sendKey("j"))
	if d.repoList.Index() != 0 {
		t.Errorf("cursor should stay at 0, got %d", d.repoList.Index())
	}

	// Navigate up - should stay at 0
	d.Update(sendKey("k"))
	if d.repoList.Index() != 0 {
		t.Errorf("cursor should stay at 0, got %d", d.repoList.Index())
	}
}

// TestAddDialog_CursorClamp_FilterResultsInNoItems verifies behavior when filter matches nothing.
func TestAddDialog_CursorClamp_FilterResultsInNoItems(t *testing.T) {
	d := newAddDialogWithRepos("alpha-service", "beta-service", "gamma-service")

	// Move cursor to index 1
	d.repoList.Select(1)
	if d.repoList.Index() != 1 {
		t.Fatalf("expected cursor at 1, got %d", d.repoList.Index())
	}

	// Enter filter mode and type "xyz" (matches no items)
	typeIntoFilterAddDialog(d, "xyz")
	d.Update(sendSpecialKey(tea.KeyEnter)) // exit filter mode, keep filter applied

	// After filter, 0 items visible - cursor should not crash
	// Navigate down - should not crash
	d.Update(sendKey("j"))
	// Navigate up - should not crash
	d.Update(sendKey("k"))
}

// TestAddDialog_CursorClamp_NavigationAfterClearingFilter verifies cursor after clearing filter.
func TestAddDialog_CursorClamp_NavigationAfterClearingFilter(t *testing.T) {
	d := newAddDialogWithRepos("alpha-service", "beta-service", "gamma-service", "delta-other")

	// Apply filter that matches 3 items
	typeIntoFilterAddDialog(d, "service")
	d.Update(sendSpecialKey(tea.KeyEnter)) // exit filter mode, keep filter applied

	// Cursor should be clamped
	if d.repoList.Index() > 2 {
		t.Errorf("cursor should be clamped to <= 2, got %d", d.repoList.Index())
	}

	// Clear filter
	d.Update(sendSpecialKey(tea.KeyEsc))

	// Now all 4 items visible, navigation should work normally
	if d.repoList.Index() > 2 {
		t.Errorf("cursor should still be <= 2 after clearing filter, got %d", d.repoList.Index())
	}
}

// TestAddDialog_CursorClamp_DownFromLastFilteredItem verifies down from last filtered item stays in bounds.
func TestAddDialog_CursorClamp_DownFromLastFilteredItem(t *testing.T) {
	d := newAddDialogWithRepos("alpha-service", "beta-service", "gamma-service")

	// Apply filter matching all items
	typeIntoFilterAddDialog(d, "service")
	d.Update(sendSpecialKey(tea.KeyEnter)) // exit filter mode, keep filter applied

	// Move to last item
	d.repoList.Select(2)

	// Try to navigate down - should stay at last item
	d.Update(sendKey("j"))
	if d.repoList.Index() != 2 {
		t.Errorf("cursor should stay at 2 (last item), got %d", d.repoList.Index())
	}
}

// TestAddDialog_CursorClamp_UpFromFirstItem verifies up from first item stays in bounds.
func TestAddDialog_CursorClamp_UpFromFirstItem(t *testing.T) {
	d := newAddDialogWithRepos("alpha-service", "beta-service", "gamma-service")

	// Apply filter
	typeIntoFilterAddDialog(d, "service")
	d.Update(sendSpecialKey(tea.KeyEnter)) // exit filter mode, keep filter applied

	// Cursor at first item (default)
	if d.repoList.Index() != 0 {
		t.Fatalf("expected cursor at 0, got %d", d.repoList.Index())
	}

	// Try to navigate up - should stay at first item
	d.Update(sendKey("k"))
	if d.repoList.Index() != 0 {
		t.Errorf("cursor should stay at 0 (first item), got %d", d.repoList.Index())
	}
}

// TestInitDialog_CursorClamp_MultipleFiltersInSequence verifies cursor clamping across multiple filter changes.
func TestInitDialog_CursorClamp_MultipleFiltersInSequence(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service", "beta-service", "gamma-other", "delta-service")

	// Filter "service" - matches 3 items (alpha, beta, delta)
	typeIntoFilter(d, "service")
	d.Update(sendSpecialKey(tea.KeyEnter))

	// Navigate to last visible item
	d.Update(sendKey("j")) // move to 1
	d.Update(sendKey("j")) // move to 2

	if d.repoList.Index() != 2 {
		t.Errorf("after navigating to last item, cursor should be at 2, got %d", d.repoList.Index())
	}

	// Clear filter and apply new filter "other" - matches 1 item (gamma)
	d.Update(sendSpecialKey(tea.KeyEsc)) // clear filter
	typeIntoFilter(d, "other")
	d.Update(sendSpecialKey(tea.KeyEnter))

	// Navigate down - should stay at 0 (only 1 item visible, clamped)
	d.Update(sendKey("j"))
	if d.repoList.Index() != 0 {
		t.Errorf("after 'other' filter with navigation, cursor should stay at 0, got %d", d.repoList.Index())
	}
}

// TestAddDialog_CursorClamp_MultipleFiltersInSequence verifies cursor clamping across multiple filter changes.
func TestAddDialog_CursorClamp_MultipleFiltersInSequence(t *testing.T) {
	d := newAddDialogWithRepos("alpha-service", "beta-service", "gamma-other", "delta-service")

	// Filter "service" - matches 3 items (alpha, beta, delta)
	typeIntoFilterAddDialog(d, "service")
	d.Update(sendSpecialKey(tea.KeyEnter))

	// Navigate to last visible item
	d.Update(sendKey("j")) // move to 1
	d.Update(sendKey("j")) // move to 2

	if d.repoList.Index() != 2 {
		t.Errorf("after navigating to last item, cursor should be at 2, got %d", d.repoList.Index())
	}

	// Clear filter and apply new filter "other" - matches 1 item (gamma)
	d.Update(sendSpecialKey(tea.KeyEsc)) // clear filter
	typeIntoFilterAddDialog(d, "other")
	d.Update(sendSpecialKey(tea.KeyEnter))

	// Navigate down - should stay at 0 (only 1 item visible, clamped)
	d.Update(sendKey("j"))
	if d.repoList.Index() != 0 {
		t.Errorf("after 'other' filter with navigation, cursor should stay at 0, got %d", d.repoList.Index())
	}
}

// ── Toggle checkbox with filter tests (Issue 1 fix) ─────────────────────────────────

// TestInitDialog_ToggleWithFilter_TogglesCorrectItem verifies that Space toggles the
// correct (visible) item when a filter is applied, not the item at the same index
// in the original unfiltered list.
func TestInitDialog_ToggleWithFilter_TogglesCorrectItem(t *testing.T) {
	// Original list: [repo-a, repo-b, repo-c, repo-d]
	// Filter "b" applied: shows [repo-b] at index 0
	// Cursor at index 0 in filtered view
	// Space should toggle repo-b (the visible item), NOT repo-a (item at index 0 in original list)
	d := newInitDialogWithRepos("repo-a", "repo-b", "repo-c", "repo-d")

	// Apply filter "b" - only repo-b should be visible
	typeIntoFilter(d, "b")
	d.Update(sendSpecialKey(tea.KeyEnter)) // exit filter mode, keep filter applied

	// Verify only one item is visible
	visible := visibleNames(d)
	if len(visible) != 1 {
		t.Fatalf("expected 1 visible item after filter, got %d: %v", len(visible), visible)
	}
	if visible[0] != "repo-b" {
		t.Errorf("expected visible item to be 'repo-b', got %q", visible[0])
	}

	// Cursor should be at index 0 (the only visible item)
	if d.repoList.Index() != 0 {
		t.Errorf("expected cursor at 0, got %d", d.repoList.Index())
	}

	// Press Space to toggle - should toggle repo-b (the visible item)
	d.Update(sendKey(" "))

	// Verify repo-b is checked, repo-a is NOT checked
	if !d.repoChecked["repo-b"] {
		t.Error("repo-b should be checked after Space on filtered view")
	}
	if d.repoChecked["repo-a"] {
		t.Error("repo-a should NOT be checked (it's not visible in filtered view)")
	}
}

// TestInitDialog_ToggleWithFilter_MultipleVisibleItems verifies toggle works correctly
// when multiple items are visible after filtering.
func TestInitDialog_ToggleWithFilter_MultipleVisibleItems(t *testing.T) {
	// Original list: [alpha, beta, gamma, delta]
	// Filter "gam" applied: shows [gamma] at index 0
	// Navigate to index 0 (gamma), Space should toggle gamma
	d := newInitDialogWithRepos("alpha", "beta", "gamma", "delta")

	// Apply filter "gam" - matches only gamma
	typeIntoFilter(d, "gam")
	d.Update(sendSpecialKey(tea.KeyEnter)) // exit filter mode, keep filter applied

	// Verify one item is visible
	visible := visibleNames(d)
	if len(visible) != 1 {
		t.Fatalf("expected 1 visible item after filter, got %d: %v", len(visible), visible)
	}
	if visible[0] != "gamma" {
		t.Errorf("expected visible item to be 'gamma', got %q", visible[0])
	}

	// Cursor should be at index 0 (the only visible item)
	if d.repoList.Index() != 0 {
		t.Errorf("expected cursor at 0, got %d", d.repoList.Index())
	}

	// Press Space to toggle - should toggle gamma (the visible item)
	d.Update(sendKey(" "))

	// Verify gamma is checked, alpha is NOT checked
	if !d.repoChecked["gamma"] {
		t.Error("gamma should be checked after Space on filtered view")
	}
	if d.repoChecked["alpha"] {
		t.Error("alpha should NOT be checked (it's not visible in filtered view)")
	}
}

// TestAddDialog_ToggleWithFilter_TogglesCorrectItem verifies that Space toggles the
// correct (visible) item when a filter is applied in AddDialog.
func TestAddDialog_ToggleWithFilter_TogglesCorrectItem(t *testing.T) {
	// Original list: [repo-a, repo-b, repo-c, repo-d]
	// Filter "c" applied: shows [repo-c] at index 0
	// Cursor at index 0 in filtered view
	// Space should toggle repo-c (the visible item), NOT repo-a (item at index 0 in original list)
	d := newAddDialogWithRepos("repo-a", "repo-b", "repo-c", "repo-d")

	// Apply filter "c" - only repo-c should be visible
	typeIntoFilterAddDialog(d, "c")
	d.Update(sendSpecialKey(tea.KeyEnter)) // exit filter mode, keep filter applied

	// Verify only one item is visible
	visible := visibleNamesAddDialog(d)
	if len(visible) != 1 {
		t.Fatalf("expected 1 visible item after filter, got %d: %v", len(visible), visible)
	}
	if visible[0] != "repo-c" {
		t.Errorf("expected visible item to be 'repo-c', got %q", visible[0])
	}

	// Cursor should be at index 0 (the only visible item)
	if d.repoList.Index() != 0 {
		t.Errorf("expected cursor at 0, got %d", d.repoList.Index())
	}

	// Press Space to toggle - should toggle repo-c (the visible item)
	d.Update(sendKey(" "))

	// Verify repo-c is checked, repo-a is NOT checked
	if !d.repoChecked["repo-c"] {
		t.Error("repo-c should be checked after Space on filtered view")
	}
	if d.repoChecked["repo-a"] {
		t.Error("repo-a should NOT be checked (it's not visible in filtered view)")
	}
}

// TestAddDialog_ToggleWithFilter_MultipleVisibleItems verifies toggle works correctly
// when multiple items are visible after filtering in AddDialog.
func TestAddDialog_ToggleWithFilter_MultipleVisibleItems(t *testing.T) {
	// Original list: [alpha, beta, gamma, delta]
	// Filter "e" applied: shows [beta, delta] at indices 0, 1
	// Navigate to index 1 (delta), Space should toggle delta
	d := newAddDialogWithRepos("alpha", "beta", "gamma", "delta")

	// Apply filter "e" - matches beta, delta (not alpha or gamma)
	typeIntoFilterAddDialog(d, "e")
	d.Update(sendSpecialKey(tea.KeyEnter)) // exit filter mode, keep filter applied

	// Verify two items are visible
	visible := visibleNamesAddDialog(d)
	if len(visible) != 2 {
		t.Fatalf("expected 2 visible items after filter, got %d: %v", len(visible), visible)
	}

	// Navigate to index 1 (delta in filtered view)
	d.Update(sendKey("j"))
	if d.repoList.Index() != 1 {
		t.Errorf("expected cursor at 1, got %d", d.repoList.Index())
	}

	// Press Space to toggle - should toggle delta (the visible item at index 1)
	d.Update(sendKey(" "))

	// Verify delta is checked, gamma is NOT checked
	if !d.repoChecked["delta"] {
		t.Error("delta should be checked after Space on filtered view")
	}
	if d.repoChecked["gamma"] {
		t.Error("gamma should NOT be checked (it's not visible in filtered view)")
	}
}

// TestInitDialog_ToggleWithFilter_NavigateAndToggle verifies toggle works correctly
// after navigating within a filtered list.
func TestInitDialog_ToggleWithFilter_NavigateAndToggle(t *testing.T) {
	d := newInitDialogWithRepos("apple", "banana", "cherry", "date", "elderberry")

	// Apply filter "rr" - matches cherry, elderberry
	typeIntoFilter(d, "rr")
	d.Update(sendSpecialKey(tea.KeyEnter))

	visible := visibleNames(d)
	if len(visible) != 2 {
		t.Fatalf("expected 2 visible items, got %d: %v", len(visible), visible)
	}

	// Navigate to index 1 (elderberry)
	d.Update(sendKey("j")) // index 1 (elderberry)

	if d.repoList.Index() != 1 {
		t.Errorf("expected cursor at 1, got %d", d.repoList.Index())
	}

	// Toggle - should toggle elderberry
	d.Update(sendKey(" "))

	if !d.repoChecked["elderberry"] {
		t.Error("elderberry should be checked")
	}
	if d.repoChecked["banana"] || d.repoChecked["date"] {
		t.Error("banana and date should NOT be checked (not visible)")
	}
}

// TestAddDialog_ToggleWithFilter_NavigateAndToggle verifies toggle works correctly
// after navigating within a filtered list in AddDialog.
func TestAddDialog_ToggleWithFilter_NavigateAndToggle(t *testing.T) {
	d := newAddDialogWithRepos("apple", "banana", "cherry", "date", "elderberry")

	// Apply filter "rr" - matches cherry, elderberry
	typeIntoFilterAddDialog(d, "rr")
	d.Update(sendSpecialKey(tea.KeyEnter))

	visible := visibleNamesAddDialog(d)
	if len(visible) != 2 {
		t.Fatalf("expected 2 visible items, got %d: %v", len(visible), visible)
	}

	// Navigate to index 1 (elderberry)
	d.Update(sendKey("j"))

	if d.repoList.Index() != 1 {
		t.Errorf("expected cursor at 1, got %d", d.repoList.Index())
	}

	// Toggle - should toggle elderberry
	d.Update(sendKey(" "))

	if !d.repoChecked["elderberry"] {
		t.Error("elderberry should be checked")
	}
	if d.repoChecked["banana"] || d.repoChecked["date"] {
		t.Error("banana and date should NOT be checked (not visible)")
	}
}
