package modal

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func makeTestRepos(names ...string) []domain.Repo {
	repos := make([]domain.Repo, len(names))
	for i, n := range names {
		repos[i] = domain.Repo{Name: n}
	}
	return repos
}

func sendKey(key string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}

func sendSpecialKey(t tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: t}
}

func execCmd(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

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

func TestInitDialog_TabCycles(t *testing.T) {
	d := NewInitDialog("feature/", nil, 80, 24)

	if d.focusIndex != 0 {
		t.Fatalf("expected focusIndex=0, got %d", d.focusIndex)
	}

	modal, _ := d.Update(sendSpecialKey(tea.KeyTab))
	d = modal.(*InitDialog)
	if d.focusIndex != 1 {
		t.Errorf("after Tab: expected focusIndex=1, got %d", d.focusIndex)
	}

	modal, _ = d.Update(sendSpecialKey(tea.KeyTab))
	d = modal.(*InitDialog)
	if d.focusIndex != 2 {
		t.Errorf("after Tab×2: expected focusIndex=2, got %d", d.focusIndex)
	}

	modal, _ = d.Update(sendSpecialKey(tea.KeyTab))
	d = modal.(*InitDialog)
	if d.focusIndex != 3 {
		t.Errorf("after Tab×3: expected focusIndex=3, got %d", d.focusIndex)
	}

	modal, _ = d.Update(sendSpecialKey(tea.KeyTab))
	d = modal.(*InitDialog)
	if d.focusIndex != 0 {
		t.Errorf("after Tab×4 (wrap): expected focusIndex=0, got %d", d.focusIndex)
	}
}

func TestInitDialog_Enter_LastField_Submits(t *testing.T) {
	d := NewInitDialog("feature/", nil, 80, 24)

	d.fields[0].SetValue("IN-9999")
	d.fields[1].SetValue("svc1, svc2 svc3")
	d.fields[2].SetValue("feature/")
	d.fields[3].SetValue("main")

	d.focusIndex = 3
	d.focusField(3)

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

func TestInitDialog_Enter_NonLastField_Advances(t *testing.T) {
	d := NewInitDialog("", nil, 80, 24)

	if d.focusIndex != 0 {
		t.Fatalf("expected focusIndex=0, got %d", d.focusIndex)
	}
	modal, _ := d.Update(sendSpecialKey(tea.KeyEnter))
	d = modal.(*InitDialog)
	if d.focusIndex != 1 {
		t.Errorf("Enter on field 0 should advance to field 1, got %d", d.focusIndex)
	}
}

func TestAddDialog_Enter_Submits(t *testing.T) {
	d := NewAddDialog("IN-6748", nil, nil, 80, 24)
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

	if len(sub.Services) != 3 {
		t.Fatalf("Services: expected 3, got %d: %v", len(sub.Services), sub.Services)
	}
	for i, want := range []string{"alpha", "beta", "gamma"} {
		if sub.Services[i] != want {
			t.Errorf("Services[%d]: expected %q, got %q", i, want, sub.Services[i])
		}
	}
}

func TestAddDialog_Esc_Closes(t *testing.T) {
	d := NewAddDialog("IN-0001", nil, nil, 80, 24)
	_, cmd := d.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("Esc must return a cmd")
	}
	msg := execCmd(cmd)
	if _, ok := msg.(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", msg)
	}
}

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

func TestHelpOverlay_ViewContainsKeyText(t *testing.T) {
	h := NewHelpOverlayWithOptions(false)
	view := stripAnsi(h.View())

	mustContain := []string{
		"Keyboard Shortcuts",
		"Tasks Panel",
		"Services Panel",
		"Output Panel",
		"Global",
		"Init new task group",
		"Clone selected task group",
		"Remove task group",
		"Plan close selected task",
		"Scan prunable tasks",
		"Validate selected task",
		"Browse task tags",
		"Add service to task",
		"Open <taskID>.sln in Rider",
		"Open <taskID>.code-workspace in VS Code",
		"Run shell command in selected task directory",
		"Open sync strategy selection",
		"Refresh tasks and repository cache",
		"Open forge action menu",
		"Show pipeline status",
		"Validate current task",
		"Push service",
		"Stash service changes",
		"Unstash service changes",
		"Remove service from task",
		"Scroll up/down",
		"Toggle this help",
		"Quit",
	}
	for _, want := range mustContain {
		if !strings.Contains(view, want) {
			t.Errorf("HelpOverlay.View() missing expected text %q", want)
		}
	}

	for _, forbidden := range []string{"1", "2", "3"} {
		if strings.Contains(view, forbidden+"  ") || strings.Contains(view, forbidden+"\t") {
			t.Errorf("HelpOverlay.View() should not advertise fast navigation key %q", forbidden)
		}
	}
}

func TestHelpOverlay_LazygitAvailableIncludesServiceRow(t *testing.T) {
	h := NewHelpOverlayWithOptions(true)
	view := stripAnsi(h.View())

	if !strings.Contains(view, "Open lazygit for selected service") {
		t.Fatalf("help overlay should include lazygit service row when available, got %q", view)
	}
}

func TestHelpOverlay_LazygitUnavailableExcludesServiceRow(t *testing.T) {
	h := NewHelpOverlayWithOptions(false)
	view := stripAnsi(h.View())

	if strings.Contains(view, "Open lazygit for selected service") || strings.Contains(view, "lazygit") {
		t.Fatalf("help overlay should omit lazygit service row when unavailable, got %q", view)
	}
}

func TestHelpOverlay_Esc_Closes(t *testing.T) {
	h := NewHelpOverlayWithOptions(false)
	_, cmd := h.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("Esc must return a cmd")
	}
	msg := execCmd(cmd)
	if _, ok := msg.(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", msg)
	}
}

func TestHelpOverlay_QuestionMark_Closes(t *testing.T) {
	h := NewHelpOverlayWithOptions(false)
	_, cmd := h.Update(sendKey("?"))
	if cmd == nil {
		t.Fatal("? must return a cmd")
	}
	msg := execCmd(cmd)
	if _, ok := msg.(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", msg)
	}
}

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

func TestInitDialog_BranchPrefixPreFilled(t *testing.T) {
	d := NewInitDialog("hotfix/", nil, 80, 24)
	if got := d.fields[2].Value(); got != "hotfix/" {
		t.Errorf("Branch Prefix field should be pre-filled with 'hotfix/', got %q", got)
	}
}

func TestInitDialogWithFlow_MultipleBranchTypes_ShowsSelectorAndOptions(t *testing.T) {
	flow := &gitflow.ResolvedGitFlow{
		ProductionBranch:  "master",
		IntegrationBranch: "develop",
		DefaultBranchType: gitflow.BranchTypeFeature,
		BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
			gitflow.BranchTypeFeature: {Prefixes: []string{"feature/"}, BaseBranch: "develop"},
			gitflow.BranchTypeHotfix:  {Prefixes: []string{"hotfix/"}, BaseBranch: "master"},
			gitflow.BranchTypeRelease: {Prefixes: []string{"release/"}, BaseBranch: "develop"},
			gitflow.BranchTypeBugfix:  {Prefixes: []string{"bugfix/"}, BaseBranch: "develop"},
			gitflow.BranchTypeChore:   {Prefixes: []string{"chore/"}, BaseBranch: "develop"},
		},
	}

	d := NewInitDialogWithFlow("feature/", flow, nil, 80, 24)

	if !d.showBranchTypeSelector {
		t.Fatal("expected branch type selector visible for multiple branch types")
	}
	want := []gitflow.BranchType{
		gitflow.BranchTypeFeature,
		gitflow.BranchTypeHotfix,
		gitflow.BranchTypeRelease,
		gitflow.BranchTypeBugfix,
		gitflow.BranchTypeChore,
	}
	if len(d.branchTypeOptions) != len(want) {
		t.Fatalf("expected %d branch type options, got %d", len(want), len(d.branchTypeOptions))
	}
	for i := range want {
		if d.branchTypeOptions[i] != want[i] {
			t.Fatalf("option[%d]: expected %q, got %q", i, want[i], d.branchTypeOptions[i])
		}
	}
}

func TestInitDialogWithFlow_SelectHotfix_UpdatesPrefixAndBaseAndSubmitBranchType(t *testing.T) {
	flow := &gitflow.ResolvedGitFlow{
		ProductionBranch:  "master",
		IntegrationBranch: "develop",
		DefaultBranchType: gitflow.BranchTypeFeature,
		BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
			gitflow.BranchTypeFeature: {Prefixes: []string{"feature/"}, BaseBranch: "develop"},
			gitflow.BranchTypeHotfix:  {Prefixes: []string{"hotfix/"}, BaseBranch: "develop"},
		},
	}

	d := NewInitDialogWithFlow("feature/", flow, nil, 80, 24)
	d.fields[0].SetValue("IN-4242")
	d.fields[1].SetValue("svc1")
	d.focusField(2)

	modal, _ := d.Update(sendKey("l"))
	d = modal.(*InitDialog)

	if got := d.fields[2].Value(); got != "hotfix/" {
		t.Fatalf("prefix should update from selected hotfix rule, got %q", got)
	}
	if got := d.fields[3].Value(); got != "master" {
		t.Fatalf("base branch should use production branch for hotfix, got %q", got)
	}

	d.focusField(d.lastFieldIndex())
	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter on last field should submit")
	}
	msg := execCmd(cmd)
	sub, ok := msg.(SubmitInitMsg)
	if !ok {
		t.Fatalf("expected SubmitInitMsg, got %T", msg)
	}
	if sub.BranchType != "hotfix" {
		t.Fatalf("expected BranchType hotfix, got %q", sub.BranchType)
	}
}

func TestInitDialogWithFlow_SingleBranchType_HidesSelector(t *testing.T) {
	flow := &gitflow.ResolvedGitFlow{
		ProductionBranch:  "master",
		IntegrationBranch: "develop",
		DefaultBranchType: gitflow.BranchTypeFeature,
		BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
			gitflow.BranchTypeFeature: {Prefixes: []string{"feature/"}, BaseBranch: "develop"},
		},
	}

	d := NewInitDialogWithFlow("", flow, nil, 80, 24)
	if d.showBranchTypeSelector {
		t.Fatal("selector must be hidden for single branch type")
	}
	if got := d.selectedBranchType(); got != "feature" {
		t.Fatalf("expected default selected feature branch type, got %q", got)
	}
}

func TestInitDialog_ShiftTab_MovesBack(t *testing.T) {
	d := NewInitDialog("", nil, 80, 24)

	d.focusField(2)

	modal, _ := d.Update(sendSpecialKey(tea.KeyShiftTab))
	d = modal.(*InitDialog)
	if d.focusIndex != 1 {
		t.Errorf("Shift+Tab from field 2 should go to field 1, got %d", d.focusIndex)
	}

	d.focusField(0)
	modal, _ = d.Update(sendSpecialKey(tea.KeyShiftTab))
	d = modal.(*InitDialog)
	if d.focusIndex != 3 {
		t.Errorf("Shift+Tab from field 0 should wrap to field 3, got %d", d.focusIndex)
	}
}

func TestInitDialog_Tab_CyclesFields_WithRepoPicker(t *testing.T) {
	repos := makeTestRepos("alpha", "beta", "gamma")
	d := NewInitDialog("feature/", repos, 80, 24)

	d.focusField(1)
	if !d.repoPickerFocused {
		t.Fatal("expected repoPickerFocused=true")
	}
	if d.focusIndex != 1 {
		t.Fatalf("expected focusIndex=1, got %d", d.focusIndex)
	}

	modal, _ := d.Update(sendSpecialKey(tea.KeyTab))
	d = modal.(*InitDialog)
	if d.focusIndex != 2 {
		t.Errorf("Tab from repo picker should advance to field 2 (Branch Prefix), got focusIndex=%d", d.focusIndex)
	}

	modal, _ = d.Update(sendSpecialKey(tea.KeyTab))
	d = modal.(*InitDialog)
	if d.focusIndex != 3 {
		t.Errorf("Tab should advance to field 3 (Base Branch), got focusIndex=%d", d.focusIndex)
	}

	modal, _ = d.Update(sendSpecialKey(tea.KeyTab))
	d = modal.(*InitDialog)
	if d.focusIndex != 0 {
		t.Errorf("Tab from last field should wrap to field 0 (Task ID), got focusIndex=%d", d.focusIndex)
	}
}

func TestInitDialog_ShiftTab_CyclesFieldsBackward_WithRepoPicker(t *testing.T) {
	repos := makeTestRepos("alpha", "beta", "gamma")
	d := NewInitDialog("feature/", repos, 80, 24)

	d.focusField(1)

	modal, _ := d.Update(sendSpecialKey(tea.KeyShiftTab))
	d = modal.(*InitDialog)
	if d.focusIndex != 0 {
		t.Errorf("Shift+Tab from repo picker should move to field 0 (Task ID), got focusIndex=%d", d.focusIndex)
	}

	modal, _ = d.Update(sendSpecialKey(tea.KeyShiftTab))
	d = modal.(*InitDialog)
	if d.focusIndex != 3 {
		t.Errorf("Shift+Tab from field 0 should wrap to field 3 (Base Branch), got focusIndex=%d", d.focusIndex)
	}
}

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

func TestAddDialog_TitleIncludesTaskID(t *testing.T) {
	d := NewAddDialog("IN-5555", nil, nil, 80, 24)
	if !strings.Contains(d.Title(), "IN-5555") {
		t.Errorf("Title() should contain taskID, got %q", d.Title())
	}
}

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

func TestConfigModal_ScrollDown_IncreasesOffset(t *testing.T) {
	d := NewConfigModal(&config.Config{RootDir: "/tmp/root", TasksRoot: "/tmp/root/.tasks"})
	d.SetTerminalSize(80, 5)

	_, _ = d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if d.scrollOffset != 1 {
		t.Errorf("scrollOffset = %d, want 1", d.scrollOffset)
	}
}

func TestConfigModal_ScrollUp_DecreasesOffset(t *testing.T) {
	d := NewConfigModal(&config.Config{RootDir: "/tmp/root", TasksRoot: "/tmp/root/.tasks"})
	d.SetTerminalSize(80, 5)
	d.scrollOffset = 2

	_, _ = d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if d.scrollOffset != 1 {
		t.Errorf("scrollOffset = %d, want 1", d.scrollOffset)
	}
}

func TestConfigModal_ScrollUp_StopsAtZero(t *testing.T) {
	d := NewConfigModal(&config.Config{RootDir: "/tmp/root"})
	_, _ = d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if d.scrollOffset != 0 {
		t.Errorf("scrollOffset = %d, want 0", d.scrollOffset)
	}
}

func TestConfigModal_Home_GoesToTop(t *testing.T) {
	d := NewConfigModal(&config.Config{RootDir: "/tmp/root", TasksRoot: "/tmp/root/.tasks"})
	d.SetTerminalSize(80, 5)
	d.scrollOffset = 5

	_, _ = d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if d.scrollOffset != 0 {
		t.Errorf("scrollOffset = %d, want 0", d.scrollOffset)
	}
}

func TestConfigModal_End_GoesToBottom(t *testing.T) {
	d := NewConfigModal(&config.Config{RootDir: "/tmp/root", TasksRoot: "/tmp/root/.tasks"})
	d.SetTerminalSize(80, 5)

	_, _ = d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if d.scrollOffset != d.maxScrollOffset() {
		t.Errorf("scrollOffset = %d, want %d", d.scrollOffset, d.maxScrollOffset())
	}
}

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

func newInitDialogWithRepos(names ...string) *InitDialog {
	repos := make([]domain.Repo, len(names))
	for i, name := range names {
		repos[i] = domain.Repo{Name: name}
	}
	d := NewInitDialog("feature/", repos, 80, 24)
	d.focusField(1)
	return d
}

func newAddDialogWithRepos(names ...string) *AddDialog {
	repos := make([]domain.Repo, len(names))
	for i, name := range names {
		repos[i] = domain.Repo{Name: name}
	}
	d := NewAddDialog("IN-1234", repos, nil, 80, 24)
	d.focusField(1)
	return d
}

func getRepoNames(d *InitDialog) []string {
	items := d.repoList.Items()
	names := make([]string, len(items))
	for i, item := range items {
		ri := item.(repoPickerItem)
		names[i] = ri.name
	}
	return names
}

func isInitRepoChecked(d *InitDialog, name string) bool {
	for _, it := range d.repoList.Items() {
		if ri, ok := it.(repoPickerItem); ok && ri.name == name {
			return ri.checked
		}
	}
	return false
}

func isAddRepoChecked(d *AddDialog, name string) bool {
	for _, it := range d.repoList.Items() {
		if ri, ok := it.(repoPickerItem); ok && ri.name == name {
			return ri.checked
		}
	}
	return false
}

func TestInitDialog_RepoPicker_Filter_Filters(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service", "beta-service", "gamma-service")

	m, _ := d.Update(sendKey("f"))
	d = m.(*InitDialog)

	d.Update(sendKey("b"))
	d.Update(sendKey("e"))

	if d.repoList.FilterState() != list.Filtering {
		t.Error("List should be in filtering state after typing filter")
	}

	if d.repoList.FilterValue() != "be" {
		t.Errorf("Filter value should be 'be', got %q", d.repoList.FilterValue())
	}
}

func TestInitDialog_RepoPicker_Filter_CaseInsensitive(t *testing.T) {
	d := newInitDialogWithRepos("MyFancyRepo", "other-repo")

	m, _ := d.Update(sendKey("f"))
	d = m.(*InitDialog)
	d.Update(sendKey("f"))
	d.Update(sendKey("a"))
	d.Update(sendKey("n"))
	d.Update(sendKey("c"))

	if d.repoList.FilterState() != list.Filtering {
		t.Error("List should be in filtering state")
	}
}

func TestInitDialog_RepoPicker_Esc_ClearsFilter(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service", "beta-service")

	m, _ := d.Update(sendKey("f"))
	d = m.(*InitDialog)
	d.Update(sendKey("a"))
	d.Update(sendKey("l"))

	m, _ = d.Update(sendSpecialKey(tea.KeyEsc))
	d = m.(*InitDialog)

	if d.repoList.FilterState() != list.Unfiltered {
		t.Errorf("ESC should clear filter, got state %v", d.repoList.FilterState())
	}
}

func TestInitDialog_RepoPicker_Space_TogglesCheckbox(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service", "beta-service")

	d.Update(sendKey(" "))

	if !isInitRepoChecked(d, "alpha-service") {
		t.Error("First item should be checked after Space")
	}

	d.Update(sendKey(" "))
	if isInitRepoChecked(d, "alpha-service") {
		t.Error("First item should be unchecked after second Space")
	}
}

func TestInitDialog_RepoPicker_JK_Navigation(t *testing.T) {
	d := newInitDialogWithRepos("alpha", "beta", "gamma")

	if d.repoList.Index() != 0 {
		t.Errorf("Initial index should be 0, got %d", d.repoList.Index())
	}

	m, _ := d.Update(sendKey("j"))
	d = m.(*InitDialog)
	if d.repoList.Index() != 1 {
		t.Errorf("'j' should move cursor down, got index %d", d.repoList.Index())
	}

	m, _ = d.Update(sendKey("k"))
	d = m.(*InitDialog)
	if d.repoList.Index() != 0 {
		t.Errorf("'k' should move cursor up, got index %d", d.repoList.Index())
	}
}

func TestInitDialog_RepoPicker_HL_PageNavigation(t *testing.T) {

	repos := make([]domain.Repo, 20)
	for i := range repos {
		repos[i] = domain.Repo{Name: fmt.Sprintf("service-%d", i)}
	}
	d := NewInitDialog("feature/", repos, 80, 24)
	d.focusField(1)

	if d.repoList.Paginator.Page != 0 {
		t.Errorf("Initial page should be 0, got %d", d.repoList.Paginator.Page)
	}

	m, _ := d.Update(sendKey("l"))
	d = m.(*InitDialog)
	if d.repoList.Paginator.Page != 1 {
		t.Errorf("'l' should go to next page, got page %d", d.repoList.Paginator.Page)
	}

	m, _ = d.Update(sendKey("h"))
	d = m.(*InitDialog)
	if d.repoList.Paginator.Page != 0 {
		t.Errorf("'h' should go to previous page, got page %d", d.repoList.Paginator.Page)
	}
}

func TestInitDialog_RepoPicker_Filter_SubmitIncludesAllChecked(t *testing.T) {
	d := newInitDialogWithRepos("api-gateway", "backend-app", "frontend-ui")
	d.fields[0].SetValue("IN-0001")
	d.fields[2].SetValue("feature/")

	d.repoList.Select(0)
	d.toggleSelectedRepo()

	d.repoList.Select(1)
	d.toggleSelectedRepo()

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

	if len(sub.Services) != 2 {
		t.Errorf("expected 2 services (api-gateway+backend-app), got %d: %v", len(sub.Services), sub.Services)
	}
}

func TestCloneInitDialog_SubmitUsesSourceBranchAsBaseBranch(t *testing.T) {
	services := []domain.Service{
		{Name: "api", Branch: "feature/SOURCE-1"},
		{Name: "worker", Branch: "feature/SOURCE-1"},
	}
	d := NewCloneInitDialog("SOURCE-1", "feature/", services, 80, 24)
	d.fields[0].SetValue("TARGET-1")
	d.focusField(3)

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("Enter on base branch field must submit in clone mode")
	}

	sub, ok := execCmd(cmd).(SubmitInitMsg)
	if !ok {
		t.Fatalf("expected SubmitInitMsg, got %T", execCmd(cmd))
	}
	if sub.TaskID != "TARGET-1" {
		t.Errorf("TaskID: expected TARGET-1, got %q", sub.TaskID)
	}
	if sub.BaseBranch != "feature/SOURCE-1" {
		t.Errorf("BaseBranch: expected source branch, got %q", sub.BaseBranch)
	}
	if len(sub.Services) != 2 {
		t.Fatalf("expected 2 selected services, got %d: %v", len(sub.Services), sub.Services)
	}
}

func TestCloneInitDialog_MismatchedSelectedBranchesBlocksSubmit(t *testing.T) {
	services := []domain.Service{
		{Name: "api", Branch: "feature/SOURCE-1"},
		{Name: "worker", Branch: "feature/OTHER"},
	}
	d := NewCloneInitDialog("SOURCE-1", "feature/", services, 80, 24)
	d.fields[0].SetValue("TARGET-1")
	d.focusField(3)

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd != nil {
		t.Fatal("mismatched selected branches must not submit")
	}
	if !strings.Contains(d.errorMsg, "selected source services must share one branch") {
		t.Fatalf("expected clear mismatch error, got %q", d.errorMsg)
	}
}

func TestCloneInitDialog_AllowsSubsetWithUniformBranch(t *testing.T) {
	services := []domain.Service{
		{Name: "api", Branch: "feature/SOURCE-1"},
		{Name: "worker", Branch: "feature/OTHER"},
	}
	d := NewCloneInitDialog("SOURCE-1", "feature/", services, 80, 24)
	d.fields[0].SetValue("TARGET-1")
	d.repoList.Select(1)
	d.toggleSelectedRepo()
	d.focusField(3)

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("uniform selected subset should submit")
	}
	sub := execCmd(cmd).(SubmitInitMsg)
	if sub.BaseBranch != "feature/SOURCE-1" {
		t.Errorf("BaseBranch: expected selected source branch feature/SOURCE-1, got %q", sub.BaseBranch)
	}
	if len(sub.Services) != 1 || sub.Services[0] != "api" {
		t.Fatalf("expected only api selected, got %v", sub.Services)
	}
}

func TestInitDialog_FilterMode_UsingListFilterState(t *testing.T) {
	d := newInitDialogWithRepos("alpha")

	if d.repoList.FilterState() != list.Unfiltered {
		t.Error("FilterState should be Unfiltered by default")
	}
}

func TestInitDialog_F_Key_EntersFilterMode(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service")

	m, _ := d.Update(sendKey("f"))
	d = m.(*InitDialog)

	if d.repoList.FilterState() != list.Filtering {
		t.Error("'f' key should enter filter mode when repo picker is focused")
	}
}

func TestInitDialog_F_Key_NotInRepoPicker_TypesIntoTextInput(t *testing.T) {
	d := NewInitDialog("feature/", nil, 80, 24)

	m, _ := d.Update(sendKey("f"))
	d = m.(*InitDialog)

	if d.repoList.FilterState() == list.Filtering {
		t.Error("'f' key should not enter filter mode when text input is focused")
	}
	if d.fields[0].Value() != "f" {
		t.Errorf("'f' key should type into text input, got %q", d.fields[0].Value())
	}
}

func TestInitDialog_Esc_InFilterMode_ClearsFilter(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service")

	m, _ := d.Update(sendKey("f"))
	d = m.(*InitDialog)
	d.Update(sendKey("a"))
	d.Update(sendKey("l"))

	m, _ = d.Update(sendSpecialKey(tea.KeyEsc))
	d = m.(*InitDialog)

	if d.repoList.FilterState() != list.Unfiltered {
		t.Error("ESC in filter mode should clear filter and exit filter mode")
	}
}

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

func TestAddDialog_Tab_TogglesToRepoPicker(t *testing.T) {
	repos := []domain.Repo{{Name: "alpha-service"}, {Name: "beta-service"}}
	d := NewAddDialog("IN-1234", repos, nil, 80, 24)

	if !d.repoPickerFocused {
		t.Fatal("expected repoPickerFocused=true initially when repos are available")
	}

	m, _ := d.Update(sendSpecialKey(tea.KeyTab))
	d = m.(*AddDialog)

	if !d.repoPickerFocused {
		t.Error("Tab should keep focus on repo picker when it is the only field")
	}
}

func TestAddDialog_Tab_FromRepoPicker_TogglesToTextInput(t *testing.T) {
	d := newAddDialogWithRepos("alpha-service", "beta-service")

	if !d.repoPickerFocused {
		t.Fatal("expected repoPickerFocused=true initially")
	}

	m, _ := d.Update(sendSpecialKey(tea.KeyTab))
	d = m.(*AddDialog)

	if !d.repoPickerFocused {
		t.Error("Tab should keep focus on repo picker when it is the only field")
	}
}

func TestAddDialog_ShiftTab_TogglesToRepoPicker(t *testing.T) {
	repos := []domain.Repo{{Name: "alpha-service"}, {Name: "beta-service"}}
	d := NewAddDialog("IN-1234", repos, nil, 80, 24)

	if !d.repoPickerFocused {
		t.Fatal("expected repoPickerFocused=true initially when repos are available")
	}

	m, _ := d.Update(sendSpecialKey(tea.KeyShiftTab))
	d = m.(*AddDialog)

	if !d.repoPickerFocused {
		t.Error("Shift+Tab should keep focus on repo picker when it is the only field")
	}
}

func TestAddDialog_ShiftTab_FromRepoPicker_TogglesToTextInput(t *testing.T) {
	d := newAddDialogWithRepos("alpha-service", "beta-service")

	if !d.repoPickerFocused {
		t.Fatal("expected repoPickerFocused=true initially")
	}

	m, _ := d.Update(sendSpecialKey(tea.KeyShiftTab))
	d = m.(*AddDialog)

	if !d.repoPickerFocused {
		t.Error("Shift+Tab should keep focus on repo picker when it is the only field")
	}
}

func TestInitDialog_Tab_DoesNotNavigateWithinRepoPicker(t *testing.T) {
	d := newInitDialogWithRepos("alpha", "beta", "gamma")

	initialIndex := d.repoList.Index()

	m, _ := d.Update(sendSpecialKey(tea.KeyTab))
	d = m.(*InitDialog)

	if d.repoList.Index() != initialIndex {
		t.Errorf("Tab should not change list index, got %d (was %d)", d.repoList.Index(), initialIndex)
	}
	if d.focusIndex == 1 {
		t.Error("Tab should move focus away from repo picker (field 1)")
	}
}

func TestAddDialog_Tab_DoesNotNavigateWithinRepoPicker(t *testing.T) {
	d := newAddDialogWithRepos("alpha-service", "beta-service", "gamma-service")

	initialIndex := d.repoList.Index()

	m, _ := d.Update(sendSpecialKey(tea.KeyTab))
	d = m.(*AddDialog)

	if d.repoList.Index() != initialIndex {
		t.Errorf("Tab should not change list index, got %d (was %d)", d.repoList.Index(), initialIndex)
	}
	if !d.repoPickerFocused {
		t.Error("Tab should keep focus on repo picker when it is the only field")
	}
}

func TestInitDialog_Enter_InFilterMode_ExitsAndKeepsFilter(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service")

	m, _ := d.Update(sendKey("f"))
	d = m.(*InitDialog)
	d.Update(sendKey("a"))
	d.Update(sendKey("l"))

	m, _ = d.Update(sendSpecialKey(tea.KeyEnter))
	d = m.(*InitDialog)

	if d.repoList.FilterState() == list.Filtering {
		t.Error("ENTER in filter mode should exit filter mode")
	}
}

func TestInitDialog_J_InFilterMode_TypesIntoFilter(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service")

	m, _ := d.Update(sendKey("f"))
	d = m.(*InitDialog)

	d.Update(sendKey("j"))

	if d.repoList.FilterValue() != "j" {
		t.Errorf("'j' in filter mode should append to filter, got %q", d.repoList.FilterValue())
	}
}

func TestInitDialog_K_InFilterMode_TypesIntoFilter(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service")

	m, _ := d.Update(sendKey("f"))
	d = m.(*InitDialog)

	d.Update(sendKey("k"))

	if d.repoList.FilterValue() != "k" {
		t.Errorf("'k' in filter mode should append to filter, got %q", d.repoList.FilterValue())
	}
}

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

func TestInitDialog_K_InNormalMode_NavigatesUp(t *testing.T) {
	d := newInitDialogWithRepos("alpha", "beta", "gamma")
	d.repoList.Select(1)

	m, _ := d.Update(sendKey("k"))
	d = m.(*InitDialog)

	if d.repoList.Index() != 0 {
		t.Errorf("'k' in normal mode should move cursor up, got %d", d.repoList.Index())
	}
}

func TestInitDialog_H_InNormalMode_GoToPrevPage(t *testing.T) {

	repos := make([]domain.Repo, 20)
	for i := range repos {
		repos[i] = domain.Repo{Name: fmt.Sprintf("service-%d", i)}
	}
	d := NewInitDialog("feature/", repos, 80, 24)
	d.focusField(1)

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

func TestInitDialog_L_InNormalMode_GoToNextPage(t *testing.T) {

	repos := make([]domain.Repo, 20)
	for i := range repos {
		repos[i] = domain.Repo{Name: fmt.Sprintf("service-%d", i)}
	}
	d := NewInitDialog("feature/", repos, 80, 24)
	d.focusField(1)

	if d.repoList.Paginator.Page != 0 {
		t.Fatalf("expected to start on page 0, got page %d", d.repoList.Paginator.Page)
	}

	m, _ := d.Update(sendKey("l"))
	d = m.(*InitDialog)

	if d.repoList.Paginator.Page != 1 {
		t.Errorf("'l' should go to next page, got page %d", d.repoList.Paginator.Page)
	}
}

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

func TestAddDialog_K_InNormalMode_NavigatesUp(t *testing.T) {
	d := newAddDialogWithRepos("alpha", "beta", "gamma")
	d.repoList.Select(1)

	m, _ := d.Update(sendKey("k"))
	d = m.(*AddDialog)

	if d.repoList.Index() != 0 {
		t.Errorf("'k' in normal mode should move cursor up, got %d", d.repoList.Index())
	}
}

func TestAddDialog_H_InNormalMode_GoToPrevPage(t *testing.T) {

	repos := make([]domain.Repo, 20)
	for i := range repos {
		repos[i] = domain.Repo{Name: fmt.Sprintf("service-%d", i)}
	}
	d := NewAddDialog("IN-1234", repos, nil, 80, 24)
	d.focusField(1)

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

func TestAddDialog_L_InNormalMode_GoToNextPage(t *testing.T) {

	repos := make([]domain.Repo, 20)
	for i := range repos {
		repos[i] = domain.Repo{Name: fmt.Sprintf("service-%d", i)}
	}
	d := NewAddDialog("IN-1234", repos, nil, 80, 24)
	d.focusField(1)

	if d.repoList.Paginator.Page != 0 {
		t.Fatalf("expected to start on page 0, got page %d", d.repoList.Paginator.Page)
	}

	m, _ := d.Update(sendKey("l"))
	d = m.(*AddDialog)

	if d.repoList.Paginator.Page != 1 {
		t.Errorf("'l' should go to next page, got page %d", d.repoList.Paginator.Page)
	}
}

func TestAddDialog_F_Key_EntersFilterMode(t *testing.T) {
	d := newAddDialogWithRepos("alpha-service")

	m, _ := d.Update(sendKey("f"))
	d = m.(*AddDialog)

	if d.repoList.FilterState() != list.Filtering {
		t.Error("'f' key should enter filter mode when repo picker is focused")
	}
}

func TestAddDialog_F_Key_NotInRepoPicker_TypesIntoTextInput(t *testing.T) {
	d := NewAddDialog("IN-1234", nil, nil, 80, 24)

	m, _ := d.Update(sendKey("f"))
	d = m.(*AddDialog)

	if d.repoList.FilterState() == list.Filtering {
		t.Error("'f' key should not enter filter mode when text input is focused")
	}
	if d.input.Value() != "f" {
		t.Errorf("'f' key should type into text input, got %q", d.input.Value())
	}
}

func TestAddDialog_Esc_InFilterMode_ClearsFilter(t *testing.T) {
	d := newAddDialogWithRepos("alpha-service")

	m, _ := d.Update(sendKey("f"))
	d = m.(*AddDialog)
	d.Update(sendKey("a"))
	d.Update(sendKey("l"))

	m, _ = d.Update(sendSpecialKey(tea.KeyEsc))
	d = m.(*AddDialog)

	if d.repoList.FilterState() != list.Unfiltered {
		t.Error("ESC in filter mode should clear filter and exit filter mode")
	}
}

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

func TestInitDialog_View_ShowsFilterModeIndicator(t *testing.T) {
	d := newInitDialogWithRepos("alpha", "beta")

	m, _ := d.Update(sendKey("f"))
	d = m.(*InitDialog)

	if d.repoList.FilterState() != list.Filtering {
		t.Error("After 'f' key, list should be in Filtering state")
	}
}

func TestInitDialog_View_NoFilterModeIndicatorWhenNotInFilterMode(t *testing.T) {
	d := newInitDialogWithRepos("alpha", "beta")

	if d.repoList.FilterState() != list.Unfiltered {
		t.Error("List should be Unfiltered when not in filter mode")
	}
}

func TestInitDialog_View_ShowsFilterModeHints(t *testing.T) {
	d := newInitDialogWithRepos("alpha", "beta")

	m, _ := d.Update(sendKey("f"))
	d = m.(*InitDialog)

	view := d.View()
	if !strings.Contains(view, "[Space] toggle") {
		t.Error("View should contain '[Space] toggle' hint (normal hint always shown)")
	}
	if !strings.Contains(view, "[f] filter") {
		t.Error("View should contain '[f] filter' hint (normal hint always shown)")
	}
}

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

func TestAddDialog_View_ShowsFilterModeIndicator(t *testing.T) {
	d := newAddDialogWithRepos("alpha", "beta")

	m, _ := d.Update(sendKey("f"))
	d = m.(*AddDialog)

	if d.repoList.FilterState() != list.Filtering {
		t.Error("After 'f' key, list should be in Filtering state")
	}
}

func TestAddDialog_View_NoFilterModeIndicatorWhenNotInFilterMode(t *testing.T) {
	d := newAddDialogWithRepos("alpha", "beta")

	if d.repoList.FilterState() != list.Unfiltered {
		t.Error("List should be Unfiltered when not in filter mode")
	}
}

func TestAddDialog_View_ShowsFilterModeHints(t *testing.T) {
	d := newAddDialogWithRepos("alpha", "beta")

	m, _ := d.Update(sendKey("f"))
	d = m.(*AddDialog)

	view := d.View()
	if !strings.Contains(view, "[Space] toggle") {
		t.Error("View should contain '[Space] toggle' hint (normal hint always shown)")
	}
	if !strings.Contains(view, "[f] filter") {
		t.Error("View should contain '[f] filter' hint (normal hint always shown)")
	}
}

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

func TestInitDialog_RepoPicker_PaginationShowsDots(t *testing.T) {

	repos := make([]domain.Repo, 20)
	for i := range repos {
		repos[i] = domain.Repo{Name: fmt.Sprintf("service-%d", i)}
	}
	d := NewInitDialog("feature/", repos, 80, 24)
	d.focusField(1)

	view := d.View()

	if !strings.Contains(view, "•") && !strings.Contains(view, "○") {
		t.Error("View should contain pagination dots when multiple pages")
	}
}

func TestAddDialog_RepoPicker_PaginationShowsDots(t *testing.T) {

	repos := make([]domain.Repo, 20)
	for i := range repos {
		repos[i] = domain.Repo{Name: fmt.Sprintf("service-%d", i)}
	}
	d := NewAddDialog("IN-1234", repos, nil, 80, 24)
	d.focusField(1)

	view := d.View()

	if !strings.Contains(view, "•") && !strings.Contains(view, "○") {
		t.Error("View should contain pagination dots when multiple pages")
	}
}

func TestInitDialog_RepoPicker_CheckboxRenders(t *testing.T) {
	d := newInitDialogWithRepos("alpha", "beta")

	view := d.View()

	if !strings.Contains(view, "[ ]") {
		t.Error("View should contain unchecked checkbox '[ ]'")
	}
}

func TestInitDialog_RepoPicker_CheckboxToggles(t *testing.T) {
	d := newInitDialogWithRepos("alpha", "beta")

	d.Update(sendKey(" "))

	if !isInitRepoChecked(d, "alpha") {
		t.Error("First item should be checked after Space")
	}

	d.Update(sendKey(" "))

	if isInitRepoChecked(d, "alpha") {
		t.Error("First item should be unchecked after second Space")
	}
}

func TestAddDialog_RepoPicker_CheckboxRenders(t *testing.T) {
	d := newAddDialogWithRepos("alpha", "beta")

	view := d.View()

	if !strings.Contains(view, "[ ]") {
		t.Error("View should contain unchecked checkbox '[ ]'")
	}
}

func TestAddDialog_RepoPicker_CheckboxToggles(t *testing.T) {
	d := newAddDialogWithRepos("alpha", "beta")

	d.Update(sendKey(" "))

	if !isAddRepoChecked(d, "alpha") {
		t.Error("First item should be checked after Space")
	}

	d.Update(sendKey(" "))

	if isAddRepoChecked(d, "alpha") {
		t.Error("First item should be unchecked after second Space")
	}
}

func visibleNames(d *InitDialog) []string {
	var names []string
	for _, item := range d.repoList.VisibleItems() {
		ri, ok := item.(repoPickerItem)
		if !ok {
			continue
		}
		names = append(names, ri.name)
	}
	return names
}

func visibleNamesAddDialog(d *AddDialog) []string {
	var names []string
	for _, item := range d.repoList.VisibleItems() {
		ri, ok := item.(repoPickerItem)
		if !ok {
			continue
		}
		names = append(names, ri.name)
	}
	return names
}

func drainFilterCmdsInit(d *InitDialog, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if msg == nil {
		return
	}
	switch m := msg.(type) {
	case tea.BatchMsg:
		type result struct{ msg tea.Msg }
		ch := make(chan result, len(m))
		for _, subCmd := range m {
			subCmd := subCmd
			go func() { ch <- result{subCmd()} }()
		}
		deadline := time.After(50 * time.Millisecond)
		for i := 0; i < len(m); i++ {
			select {
			case r := <-ch:
				if fm, ok := r.msg.(list.FilterMatchesMsg); ok {
					_, nextCmd := d.Update(fm)
					drainFilterCmdsInit(d, nextCmd)
				}
			case <-deadline:
				return
			}
		}
	case list.FilterMatchesMsg:
		_, nextCmd := d.Update(m)
		drainFilterCmdsInit(d, nextCmd)
	}
}

func drainFilterCmdsAdd(d *AddDialog, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if msg == nil {
		return
	}
	switch m := msg.(type) {
	case tea.BatchMsg:
		type result struct{ msg tea.Msg }
		ch := make(chan result, len(m))
		for _, subCmd := range m {
			subCmd := subCmd
			go func() { ch <- result{subCmd()} }()
		}
		deadline := time.After(50 * time.Millisecond)
		for i := 0; i < len(m); i++ {
			select {
			case r := <-ch:
				if fm, ok := r.msg.(list.FilterMatchesMsg); ok {
					_, nextCmd := d.Update(fm)
					drainFilterCmdsAdd(d, nextCmd)
				}
			case <-deadline:
				return
			}
		}
	case list.FilterMatchesMsg:
		_, nextCmd := d.Update(m)
		drainFilterCmdsAdd(d, nextCmd)
	}
}

func typeIntoFilter(d *InitDialog, chars string) {
	_, cmd := d.Update(sendKey("f"))
	drainFilterCmdsInit(d, cmd)
	for _, c := range chars {
		_, cmd = d.Update(sendKey(string(c)))
		drainFilterCmdsInit(d, cmd)
	}
}

func typeIntoFilterAddDialog(d *AddDialog, chars string) {
	_, cmd := d.Update(sendKey("f"))
	drainFilterCmdsAdd(d, cmd)
	for _, c := range chars {
		_, cmd = d.Update(sendKey(string(c)))
		drainFilterCmdsAdd(d, cmd)
	}
}

func TestInitDialog_CursorClamp_NavigationWithEmptyFilter(t *testing.T) {
	d := newInitDialogWithRepos("alpha", "beta", "gamma", "delta")

	if len(visibleNames(d)) != 4 {
		t.Errorf("expected 4 visible items, got %d", len(visibleNames(d)))
	}

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

	d.Update(sendKey("k"))
	if d.repoList.Index() != 2 {
		t.Errorf("expected cursor at 2, got %d", d.repoList.Index())
	}
}

func TestInitDialog_CursorClamp_FilterReducesVisibleItems(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service", "beta-service", "gamma-service", "delta-other")

	typeIntoFilter(d, "service")
	d.Update(sendSpecialKey(tea.KeyEnter))

	d.Update(sendKey("j"))
	d.Update(sendKey("j"))

	if d.repoList.Index() != 2 {
		t.Fatalf("expected cursor at 2 (last visible), got %d", d.repoList.Index())
	}

	d.Update(sendKey("j"))
	if d.repoList.Index() != 2 {
		t.Errorf("cursor should stay at 2 (clamped), got %d", d.repoList.Index())
	}

	d.Update(sendKey("k"))
	if d.repoList.Index() != 1 {
		t.Errorf("cursor should be at 1, got %d", d.repoList.Index())
	}
}

func TestInitDialog_CursorClamp_FilterResultsInSingleItem(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service", "beta-service", "gamma-service")

	d.repoList.Select(2)
	if d.repoList.Index() != 2 {
		t.Fatalf("expected cursor at 2, got %d", d.repoList.Index())
	}

	typeIntoFilter(d, "alpha")
	d.Update(sendSpecialKey(tea.KeyEnter))

	if d.repoList.Index() != 0 {
		t.Errorf("cursor should be clamped to 0 (only visible item), got %d", d.repoList.Index())
	}

	d.Update(sendKey("j"))
	if d.repoList.Index() != 0 {
		t.Errorf("cursor should stay at 0, got %d", d.repoList.Index())
	}

	d.Update(sendKey("k"))
	if d.repoList.Index() != 0 {
		t.Errorf("cursor should stay at 0, got %d", d.repoList.Index())
	}
}

func TestInitDialog_CursorClamp_FilterResultsInNoItems(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service", "beta-service", "gamma-service")

	d.repoList.Select(1)
	if d.repoList.Index() != 1 {
		t.Fatalf("expected cursor at 1, got %d", d.repoList.Index())
	}

	typeIntoFilter(d, "xyz")
	d.Update(sendSpecialKey(tea.KeyEnter))

	d.Update(sendKey("j"))

	d.Update(sendKey("k"))

}

func TestInitDialog_CursorClamp_NavigationAfterClearingFilter(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service", "beta-service", "gamma-service", "delta-other")

	typeIntoFilter(d, "service")
	d.Update(sendSpecialKey(tea.KeyEnter))

	if d.repoList.Index() > 2 {
		t.Errorf("cursor should be clamped to <= 2, got %d", d.repoList.Index())
	}

	d.Update(sendSpecialKey(tea.KeyEsc))

	if d.repoList.Index() > 2 {
		t.Errorf("cursor should still be <= 2 after clearing filter, got %d", d.repoList.Index())
	}

	d.Update(sendKey("j"))

}

func TestInitDialog_CursorClamp_DownFromLastFilteredItem(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service", "beta-service", "gamma-service")

	typeIntoFilter(d, "service")
	d.Update(sendSpecialKey(tea.KeyEnter))

	d.repoList.Select(2)

	d.Update(sendKey("j"))
	if d.repoList.Index() != 2 {
		t.Errorf("cursor should stay at 2 (last item), got %d", d.repoList.Index())
	}
}

func TestInitDialog_CursorClamp_UpFromFirstItem(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service", "beta-service", "gamma-service")

	typeIntoFilter(d, "service")
	d.Update(sendSpecialKey(tea.KeyEnter))

	if d.repoList.Index() != 0 {
		t.Fatalf("expected cursor at 0, got %d", d.repoList.Index())
	}

	d.Update(sendKey("k"))
	if d.repoList.Index() != 0 {
		t.Errorf("cursor should stay at 0 (first item), got %d", d.repoList.Index())
	}
}

func TestAddDialog_CursorClamp_NavigationWithEmptyFilter(t *testing.T) {
	d := newAddDialogWithRepos("alpha", "beta", "gamma", "delta")

	if len(visibleNamesAddDialog(d)) != 4 {
		t.Errorf("expected 4 visible items, got %d", len(visibleNamesAddDialog(d)))
	}

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

	d.Update(sendKey("k"))
	if d.repoList.Index() != 2 {
		t.Errorf("expected cursor at 2, got %d", d.repoList.Index())
	}
}

func TestAddDialog_CursorClamp_FilterReducesVisibleItems(t *testing.T) {
	d := newAddDialogWithRepos("alpha-service", "beta-service", "gamma-service", "delta-other")

	typeIntoFilterAddDialog(d, "service")
	d.Update(sendSpecialKey(tea.KeyEnter))

	d.Update(sendKey("j"))
	d.Update(sendKey("j"))

	if d.repoList.Index() != 2 {
		t.Fatalf("expected cursor at 2 (last visible), got %d", d.repoList.Index())
	}

	d.Update(sendKey("j"))
	if d.repoList.Index() != 2 {
		t.Errorf("cursor should stay at 2 (clamped), got %d", d.repoList.Index())
	}

	d.Update(sendKey("k"))
	if d.repoList.Index() != 1 {
		t.Errorf("cursor should be at 1, got %d", d.repoList.Index())
	}
}

func TestAddDialog_CursorClamp_FilterResultsInSingleItem(t *testing.T) {
	d := newAddDialogWithRepos("alpha-service", "beta-service", "gamma-service")

	d.repoList.Select(2)
	if d.repoList.Index() != 2 {
		t.Fatalf("expected cursor at 2, got %d", d.repoList.Index())
	}

	typeIntoFilterAddDialog(d, "alpha")
	d.Update(sendSpecialKey(tea.KeyEnter))

	if d.repoList.Index() != 0 {
		t.Errorf("cursor should be clamped to 0 (only visible item), got %d", d.repoList.Index())
	}

	d.Update(sendKey("j"))
	if d.repoList.Index() != 0 {
		t.Errorf("cursor should stay at 0, got %d", d.repoList.Index())
	}

	d.Update(sendKey("k"))
	if d.repoList.Index() != 0 {
		t.Errorf("cursor should stay at 0, got %d", d.repoList.Index())
	}
}

func TestAddDialog_CursorClamp_FilterResultsInNoItems(t *testing.T) {
	d := newAddDialogWithRepos("alpha-service", "beta-service", "gamma-service")

	d.repoList.Select(1)
	if d.repoList.Index() != 1 {
		t.Fatalf("expected cursor at 1, got %d", d.repoList.Index())
	}

	typeIntoFilterAddDialog(d, "xyz")
	d.Update(sendSpecialKey(tea.KeyEnter))

	d.Update(sendKey("j"))

	d.Update(sendKey("k"))
}

func TestAddDialog_CursorClamp_NavigationAfterClearingFilter(t *testing.T) {
	d := newAddDialogWithRepos("alpha-service", "beta-service", "gamma-service", "delta-other")

	typeIntoFilterAddDialog(d, "service")
	d.Update(sendSpecialKey(tea.KeyEnter))

	if d.repoList.Index() > 2 {
		t.Errorf("cursor should be clamped to <= 2, got %d", d.repoList.Index())
	}

	d.Update(sendSpecialKey(tea.KeyEsc))

	if d.repoList.Index() > 2 {
		t.Errorf("cursor should still be <= 2 after clearing filter, got %d", d.repoList.Index())
	}
}

func TestAddDialog_CursorClamp_DownFromLastFilteredItem(t *testing.T) {
	d := newAddDialogWithRepos("alpha-service", "beta-service", "gamma-service")

	typeIntoFilterAddDialog(d, "service")
	d.Update(sendSpecialKey(tea.KeyEnter))

	d.repoList.Select(2)

	d.Update(sendKey("j"))
	if d.repoList.Index() != 2 {
		t.Errorf("cursor should stay at 2 (last item), got %d", d.repoList.Index())
	}
}

func TestAddDialog_CursorClamp_UpFromFirstItem(t *testing.T) {
	d := newAddDialogWithRepos("alpha-service", "beta-service", "gamma-service")

	typeIntoFilterAddDialog(d, "service")
	d.Update(sendSpecialKey(tea.KeyEnter))

	if d.repoList.Index() != 0 {
		t.Fatalf("expected cursor at 0, got %d", d.repoList.Index())
	}

	d.Update(sendKey("k"))
	if d.repoList.Index() != 0 {
		t.Errorf("cursor should stay at 0 (first item), got %d", d.repoList.Index())
	}
}

func TestInitDialog_CursorClamp_MultipleFiltersInSequence(t *testing.T) {
	d := newInitDialogWithRepos("alpha-service", "beta-service", "gamma-other", "delta-service")

	typeIntoFilter(d, "service")
	d.Update(sendSpecialKey(tea.KeyEnter))

	d.Update(sendKey("j"))
	d.Update(sendKey("j"))

	if d.repoList.Index() != 2 {
		t.Errorf("after navigating to last item, cursor should be at 2, got %d", d.repoList.Index())
	}

	d.Update(sendSpecialKey(tea.KeyEsc))
	typeIntoFilter(d, "other")
	d.Update(sendSpecialKey(tea.KeyEnter))

	d.Update(sendKey("j"))
	if d.repoList.Index() != 0 {
		t.Errorf("after 'other' filter with navigation, cursor should stay at 0, got %d", d.repoList.Index())
	}
}

func TestAddDialog_CursorClamp_MultipleFiltersInSequence(t *testing.T) {
	d := newAddDialogWithRepos("alpha-service", "beta-service", "gamma-other", "delta-service")

	typeIntoFilterAddDialog(d, "service")
	d.Update(sendSpecialKey(tea.KeyEnter))

	d.Update(sendKey("j"))
	d.Update(sendKey("j"))

	if d.repoList.Index() != 2 {
		t.Errorf("after navigating to last item, cursor should be at 2, got %d", d.repoList.Index())
	}

	d.Update(sendSpecialKey(tea.KeyEsc))
	typeIntoFilterAddDialog(d, "other")
	d.Update(sendSpecialKey(tea.KeyEnter))

	d.Update(sendKey("j"))
	if d.repoList.Index() != 0 {
		t.Errorf("after 'other' filter with navigation, cursor should stay at 0, got %d", d.repoList.Index())
	}
}

func TestInitDialog_ToggleWithFilter_TogglesCorrectItem(t *testing.T) {

	d := newInitDialogWithRepos("repo-a", "repo-b", "repo-c", "repo-d")

	typeIntoFilter(d, "b")
	d.Update(sendSpecialKey(tea.KeyEnter))

	visible := visibleNames(d)
	if len(visible) != 1 {
		t.Fatalf("expected 1 visible item after filter, got %d: %v", len(visible), visible)
	}
	if visible[0] != "repo-b" {
		t.Errorf("expected visible item to be 'repo-b', got %q", visible[0])
	}

	if d.repoList.Index() != 0 {
		t.Errorf("expected cursor at 0, got %d", d.repoList.Index())
	}

	d.Update(sendKey(" "))

	if !isInitRepoChecked(d, "repo-b") {
		t.Error("repo-b should be checked after Space on filtered view")
	}
	if isInitRepoChecked(d, "repo-a") {
		t.Error("repo-a should NOT be checked (it's not visible in filtered view)")
	}
}

func TestInitDialog_ToggleWithFilter_MultipleVisibleItems(t *testing.T) {

	d := newInitDialogWithRepos("alpha", "beta", "gamma", "delta")

	typeIntoFilter(d, "gam")
	d.Update(sendSpecialKey(tea.KeyEnter))

	visible := visibleNames(d)
	if len(visible) != 1 {
		t.Fatalf("expected 1 visible item after filter, got %d: %v", len(visible), visible)
	}
	if visible[0] != "gamma" {
		t.Errorf("expected visible item to be 'gamma', got %q", visible[0])
	}

	if d.repoList.Index() != 0 {
		t.Errorf("expected cursor at 0, got %d", d.repoList.Index())
	}

	d.Update(sendKey(" "))

	if !isInitRepoChecked(d, "gamma") {
		t.Error("gamma should be checked after Space on filtered view")
	}
	if isInitRepoChecked(d, "alpha") {
		t.Error("alpha should NOT be checked (it's not visible in filtered view)")
	}
}

func TestAddDialog_ToggleWithFilter_TogglesCorrectItem(t *testing.T) {

	d := newAddDialogWithRepos("repo-a", "repo-b", "repo-c", "repo-d")

	typeIntoFilterAddDialog(d, "c")
	d.Update(sendSpecialKey(tea.KeyEnter))

	visible := visibleNamesAddDialog(d)
	if len(visible) != 1 {
		t.Fatalf("expected 1 visible item after filter, got %d: %v", len(visible), visible)
	}
	if visible[0] != "repo-c" {
		t.Errorf("expected visible item to be 'repo-c', got %q", visible[0])
	}

	if d.repoList.Index() != 0 {
		t.Errorf("expected cursor at 0, got %d", d.repoList.Index())
	}

	d.Update(sendKey(" "))

	if !isAddRepoChecked(d, "repo-c") {
		t.Error("repo-c should be checked after Space on filtered view")
	}
	if isAddRepoChecked(d, "repo-a") {
		t.Error("repo-a should NOT be checked (it's not visible in filtered view)")
	}
}

func TestAddDialog_ToggleWithFilter_MultipleVisibleItems(t *testing.T) {

	d := newAddDialogWithRepos("alpha", "beta", "gamma", "delta")

	typeIntoFilterAddDialog(d, "e")
	d.Update(sendSpecialKey(tea.KeyEnter))

	visible := visibleNamesAddDialog(d)
	if len(visible) != 2 {
		t.Fatalf("expected 2 visible items after filter, got %d: %v", len(visible), visible)
	}

	d.Update(sendKey("j"))
	if d.repoList.Index() != 1 {
		t.Errorf("expected cursor at 1, got %d", d.repoList.Index())
	}

	d.Update(sendKey(" "))

	if !isAddRepoChecked(d, "delta") {
		t.Error("delta should be checked after Space on filtered view")
	}
	if isAddRepoChecked(d, "gamma") {
		t.Error("gamma should NOT be checked (it's not visible in filtered view)")
	}
}

func TestInitDialog_ToggleWithFilter_NavigateAndToggle(t *testing.T) {
	d := newInitDialogWithRepos("apple", "banana", "cherry", "date", "elderberry")

	typeIntoFilter(d, "rr")
	d.Update(sendSpecialKey(tea.KeyEnter))

	visible := visibleNames(d)
	if len(visible) != 2 {
		t.Fatalf("expected 2 visible items, got %d: %v", len(visible), visible)
	}

	d.Update(sendKey("j"))

	if d.repoList.Index() != 1 {
		t.Errorf("expected cursor at 1, got %d", d.repoList.Index())
	}

	d.Update(sendKey(" "))

	if !isInitRepoChecked(d, "elderberry") {
		t.Error("elderberry should be checked")
	}
	if isInitRepoChecked(d, "banana") || isInitRepoChecked(d, "date") {
		t.Error("banana and date should NOT be checked (not visible)")
	}
}

func TestAddDialog_ToggleWithFilter_NavigateAndToggle(t *testing.T) {
	d := newAddDialogWithRepos("apple", "banana", "cherry", "date", "elderberry")

	typeIntoFilterAddDialog(d, "rr")
	d.Update(sendSpecialKey(tea.KeyEnter))

	visible := visibleNamesAddDialog(d)
	if len(visible) != 2 {
		t.Fatalf("expected 2 visible items, got %d: %v", len(visible), visible)
	}

	d.Update(sendKey("j"))

	if d.repoList.Index() != 1 {
		t.Errorf("expected cursor at 1, got %d", d.repoList.Index())
	}

	d.Update(sendKey(" "))

	if !isAddRepoChecked(d, "elderberry") {
		t.Error("elderberry should be checked")
	}
	if isAddRepoChecked(d, "banana") || isAddRepoChecked(d, "date") {
		t.Error("banana and date should NOT be checked (not visible)")
	}
}

func TestAddDialog_ExistingServices_FiltersRepos(t *testing.T) {
	repos := makeTestRepos("alpha", "beta", "gamma", "delta")
	existing := []string{"beta", "delta"}

	d := NewAddDialog("IN-1234", repos, existing, 80, 24)

	visible := visibleNamesAddDialog(d)
	if len(visible) != 2 {
		t.Fatalf("expected 2 visible repos (alpha, gamma), got %d: %v", len(visible), visible)
	}

	if visible[0] != "alpha" || visible[1] != "gamma" {
		t.Errorf("expected visible repos [alpha, gamma], got %v", visible)
	}

	for _, name := range visible {
		if name == "beta" || name == "delta" {
			t.Errorf("repo %s should have been filtered out (in existingServices)", name)
		}
	}
}

func TestAddDialog_ExistingServices_Empty_ShowsAllRepos(t *testing.T) {
	repos := makeTestRepos("alpha", "beta", "gamma")

	d := NewAddDialog("IN-1234", repos, nil, 80, 24)
	visible := visibleNamesAddDialog(d)
	if len(visible) != 3 {
		t.Errorf("expected 3 visible repos with nil existingServices, got %d", len(visible))
	}

	d = NewAddDialog("IN-1234", repos, []string{}, 80, 24)
	visible = visibleNamesAddDialog(d)
	if len(visible) != 3 {
		t.Errorf("expected 3 visible repos with empty existingServices, got %d", len(visible))
	}
}

func TestAddDialog_ExistingServices_AllFiltered_ShowsNoRepos(t *testing.T) {
	repos := makeTestRepos("alpha", "beta", "gamma")
	existing := []string{"alpha", "beta", "gamma"}

	d := NewAddDialog("IN-1234", repos, existing, 80, 24)

	visible := visibleNamesAddDialog(d)
	if len(visible) != 0 {
		t.Errorf("expected 0 visible repos when all are filtered, got %d: %v", len(visible), visible)
	}

	if d.hasRepos {
		t.Error("hasRepos should be false when all repos are filtered out")
	}
}

func TestAddDialog_ExistingServices_PartialMatch(t *testing.T) {
	repos := makeTestRepos("api-gateway", "api-service", "backend-app")
	existing := []string{"api-service"}

	d := NewAddDialog("IN-1234", repos, existing, 80, 24)

	visible := visibleNamesAddDialog(d)
	if len(visible) != 2 {
		t.Fatalf("expected 2 visible repos, got %d: %v", len(visible), visible)
	}

	for _, name := range visible {
		if name == "api-service" {
			t.Error("api-service should have been filtered out")
		}
	}

	found := false
	for _, name := range visible {
		if name == "api-gateway" {
			found = true
			break
		}
	}
	if !found {
		t.Error("api-gateway should be visible (partial match should not filter)")
	}
}

func TestAddDialog_ExistingServices_CaseSensitive(t *testing.T) {
	repos := makeTestRepos("Alpha", "BETA", "gamma")
	existing := []string{"alpha"}

	d := NewAddDialog("IN-1234", repos, existing, 80, 24)

	visible := visibleNamesAddDialog(d)

	if len(visible) != 3 {
		t.Errorf("filtering should be case-sensitive, expected 3 repos, got %d", len(visible))
	}
}

func TestAddDialog_ExistingServices_WithFiltering(t *testing.T) {
	repos := makeTestRepos("alpha-service", "beta-service", "gamma-other", "delta-service")
	existing := []string{"beta-service"}

	d := NewAddDialog("IN-1234", repos, existing, 80, 24)

	visible := visibleNamesAddDialog(d)
	if len(visible) != 3 {
		t.Fatalf("expected 3 visible repos initially, got %d: %v", len(visible), visible)
	}

	typeIntoFilterAddDialog(d, "service")
	d.Update(sendSpecialKey(tea.KeyEnter))

	visible = visibleNamesAddDialog(d)
	if len(visible) != 2 {
		t.Errorf("expected 2 visible repos after filter, got %d: %v", len(visible), visible)
	}

	for _, name := range visible {
		if name == "beta-service" {
			t.Error("beta-service should not be visible (filtered by existingServices)")
		}
		if name == "gamma-other" {
			t.Error("gamma-other should not be visible (filtered by user filter)")
		}
	}
}
