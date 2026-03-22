package modal

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
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
	d := NewInitDialog("feature/", nil)

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
	d := NewInitDialog("feature/", nil)

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
	d := NewInitDialog("feature/", nil)

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
	d := NewInitDialog("", nil)

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
	d := NewInitDialog("hotfix/", nil)
	if got := d.fields[2].Value(); got != "hotfix/" {
		t.Errorf("Branch Prefix field should be pre-filled with 'hotfix/', got %q", got)
	}
}

// ── 18. InitDialog ShiftTab moves backwards ──────────────────────────────────

func TestInitDialog_ShiftTab_MovesBack(t *testing.T) {
	d := NewInitDialog("", nil)
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

// ── 19. InitDialog Tab navigates within repo picker ──────────────────────────

func TestInitDialog_Tab_NavigatesRepoPicker(t *testing.T) {
	repos := makeTestRepos("alpha", "beta", "gamma")
	d := NewInitDialog("feature/", repos)

	// Focus field 1 (repo picker).
	d.focusField(1)
	if !d.repoPickerFocused {
		t.Fatal("expected repoPickerFocused=true")
	}
	if d.repoCursor != 0 {
		t.Fatalf("expected repoCursor=0, got %d", d.repoCursor)
	}

	// Tab should move cursor to next item, NOT advance to next field.
	modal, _ := d.Update(sendSpecialKey(tea.KeyTab))
	d = modal.(*InitDialog)
	if d.focusIndex != 1 {
		t.Errorf("Tab in repo picker should stay on field 1, got focusIndex=%d", d.focusIndex)
	}
	if d.repoCursor != 1 {
		t.Errorf("Tab in repo picker should move cursor to 1, got %d", d.repoCursor)
	}

	// Tab wraps: after last item goes back to 0.
	d.repoCursor = 2
	modal, _ = d.Update(sendSpecialKey(tea.KeyTab))
	d = modal.(*InitDialog)
	if d.repoCursor != 0 {
		t.Errorf("Tab at last item should wrap cursor to 0, got %d", d.repoCursor)
	}

	// Shift+Tab moves cursor backward.
	modal, _ = d.Update(sendSpecialKey(tea.KeyShiftTab))
	d = modal.(*InitDialog)
	if d.repoCursor != 2 {
		t.Errorf("Shift+Tab at first item should wrap cursor to last (2), got %d", d.repoCursor)
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
	d := NewAddDialog("IN-5555")
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

// ── 24. InitDialog repo picker filter — short query shows all repos ───────────

func TestInitDialog_RepoPicker_ShortFilter_ShowsAll(t *testing.T) {
	repos := []repoPickerItem{
		{name: "alpha", checked: true},
		{name: "beta", checked: true},
		{name: "gamma", checked: true},
	}
	d := newInitDialogWithRepos(repos)

	// Type 3 characters — filter must NOT be applied (threshold is > 3).
	typeIntoFilter(d, "alp")

	visible := d.visibleRepos()
	if len(visible) != 3 {
		t.Errorf("filter len=3: expected all 3 repos visible, got %d", len(visible))
	}
}

// ── 25. InitDialog repo picker filter — long query filters list ───────────────

func TestInitDialog_RepoPicker_LongFilter_Filters(t *testing.T) {
	repos := []repoPickerItem{
		{name: "alpha-service", checked: true},
		{name: "beta-service", checked: true},
		{name: "gamma-service", checked: true},
	}
	d := newInitDialogWithRepos(repos)

	// Type 4 characters matching only "alpha-service".
	typeIntoFilter(d, "alph")

	visible := d.visibleRepos()
	if len(visible) != 1 {
		t.Errorf("filter 'alph': expected 1 repo visible, got %d: %v", len(visible), visibleNames(d, visible))
	}
	if len(visible) == 1 && d.repoPicker[visible[0]].name != "alpha-service" {
		t.Errorf("filter 'alph': expected alpha-service, got %q", d.repoPicker[visible[0]].name)
	}
}

// ── 26. InitDialog repo picker filter — case-insensitive matching ─────────────

func TestInitDialog_RepoPicker_Filter_CaseInsensitive(t *testing.T) {
	repos := []repoPickerItem{
		{name: "MyFancyRepo", checked: true},
		{name: "other-repo", checked: true},
	}
	d := newInitDialogWithRepos(repos)

	typeIntoFilter(d, "fanc")

	visible := d.visibleRepos()
	if len(visible) != 1 {
		t.Errorf("case-insensitive filter 'fanc': expected 1 visible, got %d", len(visible))
	}
}

// ── 27. InitDialog repo picker filter — Esc clears filter, second Esc closes ──

func TestInitDialog_RepoPicker_Esc_ClearsThenCloses(t *testing.T) {
	repos := []repoPickerItem{
		{name: "alpha-service", checked: true},
		{name: "beta-service", checked: true},
	}
	d := newInitDialogWithRepos(repos)
	typeIntoFilter(d, "alph")

	// First Esc — clear filter.
	m, cmd := d.Update(sendSpecialKey(tea.KeyEsc))
	d = m.(*InitDialog)
	if cmd != nil && execCmd(cmd) != nil {
		if _, ok := execCmd(cmd).(CloseModalMsg); ok {
			t.Error("first Esc with active filter should clear it, not close the modal")
		}
	}
	if d.repoFilter != "" {
		t.Errorf("after first Esc, filter should be empty, got %q", d.repoFilter)
	}

	// Second Esc — close modal.
	_, cmd = d.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("second Esc must return a cmd")
	}
	if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
		t.Fatal("second Esc must emit CloseModalMsg")
	}
}

// ── 28. InitDialog repo picker filter — Backspace removes last char ───────────

func TestInitDialog_RepoPicker_Backspace_RemovesChar(t *testing.T) {
	repos := []repoPickerItem{
		{name: "alpha-service", checked: true},
	}
	d := newInitDialogWithRepos(repos)
	typeIntoFilter(d, "alph")

	if d.repoFilter != "alph" {
		t.Fatalf("expected filter 'alph', got %q", d.repoFilter)
	}

	m, _ := d.Update(sendSpecialKey(tea.KeyBackspace))
	d = m.(*InitDialog)
	if d.repoFilter != "alp" {
		t.Errorf("after Backspace, expected filter 'alp', got %q", d.repoFilter)
	}
}

// ── 29. InitDialog repo picker filter — navigation stays in visible bounds ────

func TestInitDialog_RepoPicker_Filter_CursorClamped(t *testing.T) {
	repos := []repoPickerItem{
		{name: "aaa-service", checked: true},
		{name: "bbb-service", checked: true},
		{name: "ccc-service", checked: true},
	}
	d := newInitDialogWithRepos(repos)

	// Move cursor to last item.
	d.repoCursor = 2

	// Apply a filter that leaves only 1 item — cursor must be clamped.
	typeIntoFilter(d, "aaa-")

	if d.repoCursor != 0 {
		t.Errorf("cursor should be clamped to 0 after filter narrows to 1 item, got %d", d.repoCursor)
	}
}

// ── 30. InitDialog repo picker filter — submit includes all checked repos ──────

func TestInitDialog_RepoPicker_Filter_SubmitIncludesAllChecked(t *testing.T) {
	repos := []repoPickerItem{
		{name: "alpha-service", checked: true},
		{name: "beta-service", checked: true},
		{name: "gamma-service", checked: false},
	}
	d := newInitDialogWithRepos(repos)
	d.fields[0].SetValue("IN-0001")
	d.fields[2].SetValue("feature/")

	// Apply a filter so only "alpha-service" is visible.
	typeIntoFilter(d, "alph")

	// Navigate to last field and submit.
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

	// alpha and beta are checked; gamma is not — submit must include both regardless of filter.
	if len(sub.Services) != 2 {
		t.Errorf("expected 2 services (alpha+beta), got %d: %v", len(sub.Services), sub.Services)
	}
}

// ── test helpers ─────────────────────────────────────────────────────────────

// newInitDialogWithRepos builds an InitDialog directly from a []repoPickerItem
// without going through domain.Repo, making tests self-contained.
func newInitDialogWithRepos(items []repoPickerItem) *InitDialog {
	d := &InitDialog{
		defaultBranchPrefix: "feature/",
		hasRepos:            len(items) > 0,
		repoPicker:          items,
	}

	for i := range d.fields {
		ti := textinput.New()
		ti.Prompt = ""
		ti.Width = 40
		d.fields[i] = ti
	}
	d.focusField(1) // focus the repo picker
	return d
}

// typeIntoFilter simulates typing each rune of s into the dialog's filter.
func typeIntoFilter(d *InitDialog, s string) {
	for _, r := range s {
		d.Update(sendKey(string(r))) //nolint:errcheck
	}
}

// visibleNames returns the repo names for the given index slice — useful in
// test error messages.
func visibleNames(d *InitDialog, indices []int) []string {
	names := make([]string, len(indices))
	for i, idx := range indices {
		names[i] = d.repoPicker[idx].name
	}
	return names
}
