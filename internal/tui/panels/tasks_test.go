package panels

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/paginator"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func makeTasks(ids ...string) []domain.Task {
	tasks := make([]domain.Task, len(ids))
	for i, id := range ids {
		tasks[i] = domain.Task{
			ID:  id,
			Dir: "/tmp/.tasks/" + id,
		}
	}
	return tasks
}

func makeTasksWithServices(id string, serviceNames ...string) domain.Task {
	svcs := make([]domain.Service, len(serviceNames))
	for i, n := range serviceNames {
		svcs[i] = domain.Service{Name: n}
	}
	return domain.Task{ID: id, Services: svcs}
}

func makeTaskWithMeta(id, parentID, phase, version string, serviceCount int) domain.Task {
	svcs := make([]domain.Service, serviceCount)
	for i := 0; i < serviceCount; i++ {
		svcs[i] = domain.Service{Name: "svc"}
	}

	return domain.Task{
		ID:       id,
		ParentID: parentID,
		Phase:    phase,
		Version:  version,
		Services: svcs,
		Dir:      "/tmp/.tasks/" + id,
	}
}

func makeFlow(types ...gitflow.BranchType) *gitflow.ResolvedGitFlow {
	bt := make(map[gitflow.BranchType]gitflow.BranchTypeRule, len(types))
	for _, t := range types {
		bt[t] = gitflow.BranchTypeRule{}
	}

	return &gitflow.ResolvedGitFlow{BranchTypes: bt}
}

func sendKey(key string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}

func sendSpecialKey(t tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: t}
}

func TestTasksPanel_NewEmpty(t *testing.T) {
	p := NewTasksPanel(40, 20)
	if p.SelectedTask() != nil {
		t.Error("expected nil SelectedTask on empty list")
	}
	if p.focused {
		t.Error("expected unfocused by default")
	}
}

func TestTasksPanel_SetTasks_PopulatesList(t *testing.T) {
	p := NewTasksPanel(40, 20)
	tasks := makeTasks("IN-001", "IN-002", "IN-003")
	p.SetTasks(tasks)

	if got := len(p.list.Items()); got != 3 {
		t.Errorf("expected 3 items, got %d", got)
	}
}

func TestTasksPanel_SetTasks_EmptyResetsSelection(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetTasks(makeTasks("IN-001", "IN-002", "IN-003"))

	p.list.Select(2)

	p.SetTasks(makeTasks("IN-001"))
	if got := len(p.list.Items()); got != 1 {
		t.Errorf("expected 1 item after reset, got %d", got)
	}
}

func TestTasksPanel_SetTasks_PreservesSelection_Flat(t *testing.T) {
	p := NewTasksPanel(40, 20)
	tasks := makeTasks("IN-001", "IN-002", "IN-003")
	p.SetTasks(tasks)
	p.list.Select(1)

	p.SetTasks(tasks)

	selected := p.SelectedTask()
	if selected == nil {
		t.Fatal("expected selected task after refresh")
	}
	if selected.ID != "IN-002" {
		t.Fatalf("expected selected task IN-002, got %q", selected.ID)
	}
}

func TestTasksPanel_SetTasks_FallsBackToFirstIfRemoved_Flat(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetTasks(makeTasks("IN-001", "IN-002", "IN-003"))
	p.list.Select(2)

	p.SetTasks(makeTasks("IN-001", "IN-002"))

	selected := p.SelectedTask()
	if selected == nil {
		t.Fatal("expected selected task after refresh")
	}
	if selected.ID != "IN-001" {
		t.Fatalf("expected fallback selection IN-001, got %q", selected.ID)
	}
}

func TestTasksPanel_SetTasks_FallsBackToFirstIfRemoved_Middle_Flat(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetTasks(makeTasks("IN-001", "IN-002", "IN-003"))
	p.list.Select(1)

	p.SetTasks(makeTasks("IN-001", "IN-003"))

	selected := p.SelectedTask()
	if selected == nil {
		t.Fatal("expected selected task after refresh")
	}
	if selected.ID != "IN-001" {
		t.Fatalf("expected fallback selection IN-001, got %q", selected.ID)
	}
}

func TestTasksPanel_SelectedTask_NilOnEmpty(t *testing.T) {
	p := NewTasksPanel(40, 20)
	if p.SelectedTask() != nil {
		t.Error("SelectedTask must return nil when list is empty")
	}
}

func TestTasksPanel_SelectedTask_ReturnsFirstByDefault(t *testing.T) {
	p := NewTasksPanel(40, 20)
	tasks := makeTasks("IN-001", "IN-002")
	p.SetTasks(tasks)

	got := p.SelectedTask()
	if got == nil {
		t.Fatal("expected non-nil SelectedTask")
	}
	if got.ID != "IN-001" {
		t.Errorf("expected IN-001, got %s", got.ID)
	}
}

func TestTasksPanel_SetFocused(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetFocused(true)
	if !p.focused {
		t.Error("SetFocused(true) should set focused=true")
	}
	p.SetFocused(false)
	if p.focused {
		t.Error("SetFocused(false) should set focused=false")
	}
}

func TestTasksPanel_SetSize(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetSize(80, 30)
	if p.width != 80 || p.height != 30 {
		t.Errorf("SetSize: expected 80×30, got %d×%d", p.width, p.height)
	}
}

func TestTasksPanel_KeyDown_MovesSelection(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetTasks(makeTasks("IN-001", "IN-002", "IN-003"))
	p.SetFocused(true)

	initialIdx := p.list.Index()
	p, _ = p.Update(sendKey("j"))
	if p.list.Index() == initialIdx && len(p.list.Items()) > 1 {
		t.Error("j key should move cursor down")
	}
}

func TestTasksPanel_KeyUp_MovesSelection(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetTasks(makeTasks("IN-001", "IN-002", "IN-003"))
	p.SetFocused(true)
	p.list.Select(2)

	p, _ = p.Update(sendKey("k"))
	if p.list.Index() == 2 {
		t.Error("k key should move cursor up")
	}
}

func TestTasksPanel_KeyG_JumpsToFirst(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetTasks(makeTasks("IN-001", "IN-002", "IN-003"))
	p.SetFocused(true)
	p.list.Select(2)

	p, _ = p.Update(sendKey("g"))
	if p.list.Index() != 0 {
		t.Errorf("g should jump to index 0, got %d", p.list.Index())
	}
}

func TestTasksPanel_KeyGUpper_JumpsToLast(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetTasks(makeTasks("IN-001", "IN-002", "IN-003"))
	p.SetFocused(true)

	p, _ = p.Update(sendKey("G"))
	want := len(p.tasks) - 1
	if p.list.Index() != want {
		t.Errorf("G should jump to index %d, got %d", want, p.list.Index())
	}
}

func TestTasksPanel_KeyEnter_EmitsFocusServicesMsg(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetTasks(makeTasks("IN-001"))
	p.SetFocused(true)

	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter should return a cmd")
	}
	msg := cmd()
	got, ok := msg.(FocusServicesMsg)
	if !ok {
		t.Fatalf("expected FocusServicesMsg, got %T", msg)
	}
	if got.TaskID != "IN-001" {
		t.Errorf("expected TaskID=IN-001, got %s", got.TaskID)
	}
}

func TestTasksPanel_KeyEnter_EmptyList_NoOp(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetFocused(true)

	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("Enter on empty list should return nil cmd")
	}
}

func TestTasksPanel_KeyI_EmitsOpenInitDialogMsg(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetFocused(true)

	_, cmd := p.Update(sendKey("i"))
	if cmd == nil {
		t.Fatal("i key should return a cmd")
	}
	msg := cmd()
	if _, ok := msg.(OpenInitDialogMsg); !ok {
		t.Fatalf("expected OpenInitDialogMsg, got %T", msg)
	}
}

func TestTasksPanel_KeyD_EmitsOpenRemoveDialogMsg(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetTasks(makeTasks("IN-001"))
	p.SetFocused(true)

	_, cmd := p.Update(sendKey("d"))
	if cmd == nil {
		t.Fatal("d key should return a cmd")
	}
	msg := cmd()
	got, ok := msg.(OpenRemoveDialogMsg)
	if !ok {
		t.Fatalf("expected OpenRemoveDialogMsg, got %T", msg)
	}
	if got.TaskID != "IN-001" {
		t.Errorf("expected TaskID=IN-001, got %s", got.TaskID)
	}
}

func TestTasksPanel_KeyC_EmitsOpenCloneDialogMsg(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetTasks(makeTasks("IN-001"))
	p.SetFocused(true)

	_, cmd := p.Update(sendKey("c"))
	if cmd == nil {
		t.Fatal("c key should return a cmd")
	}
	msg := cmd()
	got, ok := msg.(OpenCloneDialogMsg)
	if !ok {
		t.Fatalf("expected OpenCloneDialogMsg, got %T", msg)
	}
	if got.TaskID != "IN-001" {
		t.Errorf("expected TaskID=IN-001, got %s", got.TaskID)
	}
}

func TestTasksPanel_KeyCUpper_EmitsPlanCloseTaskMsg(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetTasks(makeTasks("IN-001"))
	p.SetFocused(true)

	_, cmd := p.Update(sendKey("C"))
	if cmd == nil {
		t.Fatal("C key should return a cmd")
	}
	msg := cmd()
	got, ok := msg.(PlanCloseTaskMsg)
	if !ok {
		t.Fatalf("expected PlanCloseTaskMsg, got %T", msg)
	}
	if got.TaskID != "IN-001" {
		t.Errorf("expected TaskID=IN-001, got %s", got.TaskID)
	}
}

func TestTasksPanel_KeyC_EmptyList_NoOp(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetFocused(true)

	_, cmd := p.Update(sendKey("c"))
	if cmd != nil {
		t.Error("c on empty list should be a no-op")
	}
}

func TestTasksPanel_KeyP_EmitsScanPrunableTasksMsg(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetTasks(makeTasks("IN-001"))
	p.SetFocused(true)

	_, cmd := p.Update(sendKey("P"))
	if cmd == nil {
		t.Fatal("P key should return a cmd")
	}
	if _, ok := cmd().(ScanPrunableTasksMsg); !ok {
		t.Fatalf("expected ScanPrunableTasksMsg, got %T", cmd())
	}
}

func TestTasksPanel_KeyV_EmitsValidateTaskMsg(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetTasks(makeTasks("IN-001"))
	p.SetFocused(true)

	_, cmd := p.Update(sendKey("V"))
	if cmd == nil {
		t.Fatal("V key should return a cmd")
	}
	msg := cmd()
	got, ok := msg.(ValidateTaskMsg)
	if !ok {
		t.Fatalf("expected ValidateTaskMsg, got %T", msg)
	}
	if got.TaskID != "IN-001" {
		t.Fatalf("TaskID=%q, want IN-001", got.TaskID)
	}
}

func TestTasksPanel_KeyT_EmitsOpenTagBrowserMsg(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetTasks(makeTasks("IN-001"))
	p.SetFocused(true)

	_, cmd := p.Update(sendKey("T"))
	if cmd == nil {
		t.Fatal("T key should return a cmd")
	}
	msg := cmd()
	got, ok := msg.(OpenTagBrowserMsg)
	if !ok {
		t.Fatalf("expected OpenTagBrowserMsg, got %T", msg)
	}
	if got.TaskID != "IN-001" {
		t.Fatalf("TaskID=%q, want IN-001", got.TaskID)
	}
}

func TestTasksPanel_KeyD_EmptyList_NoOp(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetFocused(true)

	_, cmd := p.Update(sendKey("d"))
	if cmd != nil {
		t.Error("d on empty list should be a no-op (nil cmd)")
	}
}

func TestTasksPanel_KeyR_EmitsRiderTaskMsg(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetTasks(makeTasks("IN-001"))
	p.SetFocused(true)

	_, cmd := p.Update(sendKey("R"))
	if cmd == nil {
		t.Fatal("R key should return a cmd")
	}
	msg := cmd()
	got, ok := msg.(RiderTaskMsg)
	if !ok {
		t.Fatalf("expected RiderTaskMsg, got %T", msg)
	}
	if got.TaskDir != "/tmp/.tasks/IN-001" {
		t.Errorf("expected TaskDir=/tmp/.tasks/IN-001, got %s", got.TaskDir)
	}
	if got.TaskID != "IN-001" {
		t.Errorf("expected TaskID=IN-001, got %s", got.TaskID)
	}
}

func TestTasksPanel_KeyComma_EmitsOpenConfigModalMsg(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetTasks(makeTasks("IN-001"))
	p.SetFocused(true)

	_, cmd := p.Update(sendKey(","))
	if cmd == nil {
		t.Fatal(", key should return a cmd")
	}

	msg := cmd()
	if _, ok := msg.(OpenConfigModalMsg); !ok {
		t.Fatalf("expected OpenConfigModalMsg, got %T", msg)
	}
}

func TestTasksPanel_KeyR_EmptyList_NoOp(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetFocused(true)

	_, cmd := p.Update(sendKey("R"))
	if cmd != nil {
		t.Error("R on empty list should be a no-op")
	}
}

func TestTasksPanel_CursorMoveEmitsSelectionChanged(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetTasks(makeTasks("IN-001", "IN-002"))
	p.SetFocused(true)

	_, cmd := p.Update(sendKey("j"))
	if cmd == nil {
		t.Fatal("j on a 2-task focused panel should return a non-nil cmd")
	}

	msg := cmd()
	got, ok := msg.(TaskSelectionChangedMsg)
	if !ok {
		t.Fatalf("expected TaskSelectionChangedMsg, got %T", msg)
	}
	if got.TaskID != "IN-002" {
		t.Errorf("expected TaskID=IN-002, got %q", got.TaskID)
	}
}

func TestTasksPanel_EmptyList_NoCrash(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetFocused(true)

	for _, key := range []string{"j", "k", "g", "G"} {
		_, cmd := p.Update(sendKey(key))
		if cmd != nil {
			t.Errorf("key %q on empty panel should return nil cmd, got non-nil", key)
		}
	}
}

func TestTasksPanel_View_ContainsTitle(t *testing.T) {
	p := NewTasksPanel(60, 20)
	p.SetTasks(makeTasks("IN-001", "IN-002"))
	view := p.View()
	if !strings.Contains(stripAnsi(view), "Tasks") {
		t.Error("View should contain 'Tasks' in title")
	}
}

func TestTasksPanel_View_ContainsItemCount(t *testing.T) {
	p := NewTasksPanel(60, 20)
	p.SetTasks(makeTasks("IN-001", "IN-002", "IN-003"))
	view := stripAnsi(p.View())

	if !strings.Contains(view, "/3]") {
		t.Errorf("View should contain total count, got: %q", view)
	}
}

func TestTasksPanel_View_ServiceCount(t *testing.T) {
	p := NewTasksPanel(60, 20)
	task := makeTasksWithServices("IN-001", "collection", "databridge")
	p.SetTasks([]domain.Task{task})
	view := stripAnsi(p.View())

	if !strings.Contains(view, "2 service") {
		t.Errorf("View should render service count, got: %q", view)
	}
}

func TestTasksPanel_Unfocused_KeysIgnored(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetTasks(makeTasks("IN-001"))

	_, cmd := p.Update(sendKey("i"))
	if cmd != nil {

		msg := cmd()
		if _, ok := msg.(OpenInitDialogMsg); ok {
			t.Error("unfocused panel should not emit OpenInitDialogMsg")
		}
	}
}

func TestTasksPanel_FilterMode_ShowsFilterInput(t *testing.T) {
	p := NewTasksPanel(60, 20)
	p.SetTasks(makeTasks("IN-001", "IN-002", "IN-003"))
	p.SetFocused(true)

	p, _ = p.Update(sendKey("f"))

	if !p.FilterActive() {
		t.Error("Panel should be in filter mode after pressing 'f'")
	}
}

func TestTasksPanel_FilterMode_NoFilterIndicatorWhenNotFiltering(t *testing.T) {
	p := NewTasksPanel(60, 20)
	p.SetTasks(makeTasks("IN-001", "IN-002", "IN-003"))
	p.SetFocused(true)

	if p.FilterActive() {
		t.Error("Panel should not be in filter mode initially")
	}
}

func TestTasksPanel_FilterMode_EscClearsFilter(t *testing.T) {
	p := NewTasksPanel(60, 20)
	p.SetTasks(makeTasks("IN-001", "IN-002", "IN-003"))
	p.SetFocused(true)

	p, _ = p.Update(sendKey("f"))
	p, _ = p.Update(sendKey("I"))
	p, _ = p.Update(sendKey("N"))

	if !p.FilterActive() {
		t.Error("Panel should be in filter mode after typing")
	}

	p, _ = p.Update(sendSpecialKey(tea.KeyEsc))

	if p.FilterActive() {
		t.Error("Panel should NOT be in filter mode after ESC clears filter")
	}
}

func TestTasksPanel_FilterMode_EnterExitsFilterMode(t *testing.T) {
	p := NewTasksPanel(60, 20)
	p.SetTasks(makeTasks("IN-001", "IN-002", "IN-003"))
	p.SetFocused(true)

	p, _ = p.Update(sendKey("f"))
	p, _ = p.Update(sendKey("I"))
	p, _ = p.Update(sendKey("N"))

	if !p.FilterActive() {
		t.Error("Panel should be in filter mode after typing")
	}

	p, _ = p.Update(sendSpecialKey(tea.KeyEnter))

	if p.FilterActive() {
		t.Error("Panel should NOT be in filter mode after ENTER exits filter mode")
	}

	if p.list.FilterState() != list.FilterApplied {
		t.Error("Filter should still be applied after ENTER")
	}
}

func TestTasksPanel_FilterMode_FKeyEntersFilterMode(t *testing.T) {
	p := NewTasksPanel(60, 20)
	p.SetTasks(makeTasks("IN-001", "IN-002", "IN-003"))
	p.SetFocused(true)

	if p.FilterActive() {
		t.Error("Panel should not be in filter mode initially")
	}

	p, _ = p.Update(sendKey("f"))

	if !p.FilterActive() {
		t.Error("Panel should be in filter mode after pressing 'f'")
	}
}

func TestTasksPanel_TreeMode_ViewRendersGroupAndChildren(t *testing.T) {
	p := NewTasksPanel(80, 20)
	p.SetFlow(makeFlow(gitflow.BranchTypeRelease))
	p.SetTasks([]domain.Task{
		makeTaskWithMeta("ZA-553", "", "feature", "", 3),
		makeTaskWithMeta("ZA-553-release", "ZA-553", "release", "1.2.0", 3),
	})

	view := stripAnsi(p.View())
	if !strings.Contains(view, "▼ ZA-553") {
		t.Fatalf("expected group header in view, got: %q", view)
	}
	if !strings.Contains(view, "├─ release/1.2.0") {
		t.Fatalf("expected child row in view, got: %q", view)
	}
}

func TestTasksPanel_TreeMode_NavigationSkipsGroupHeaders(t *testing.T) {
	p := NewTasksPanel(80, 20)
	p.SetFlow(makeFlow(gitflow.BranchTypeRelease))
	p.SetFocused(true)
	p.SetTasks([]domain.Task{
		makeTaskWithMeta("ZA-553", "", "feature", "", 1),
		makeTaskWithMeta("ZA-553-release", "ZA-553", "release", "1.2.0", 1),
		makeTaskWithMeta("ZA-554", "", "feature", "", 1),
		makeTaskWithMeta("ZA-554-release", "ZA-554", "release", "2.0.0", 1),
	})

	if p.rows[p.selectedIdx].kind != treeRowKindTask {
		t.Fatal("initial selection must be task row")
	}

	p, _ = p.Update(sendKey("j"))
	if got := p.SelectedTask(); got == nil || got.ID != "ZA-553-release" {
		t.Fatalf("after first j expected ZA-553-release, got %+v", got)
	}

	p, _ = p.Update(sendKey("j"))
	if got := p.SelectedTask(); got == nil || got.ID != "ZA-554" {
		t.Fatalf("after second j expected ZA-554 (group header skipped), got %+v", got)
	}
}

func TestTasksPanel_TreeMode_QNoOp_ForFeatureRoot(t *testing.T) {
	p := NewTasksPanel(80, 20)
	p.SetFlow(makeFlow(gitflow.BranchTypeRelease))
	p.SetFocused(true)
	p.SetTasks([]domain.Task{
		makeTaskWithMeta("ZA-553", "", "feature", "", 1),
		makeTaskWithMeta("ZA-553-release", "ZA-553", "release", "1.2.0", 1),
	})

	_, cmd := p.Update(sendKey("Q"))
	if cmd != nil {
		t.Fatal("Q must be no-op")
	}
}

func TestTasksPanel_TreeMode_QNoOp_ForReleaseChild(t *testing.T) {
	p := NewTasksPanel(80, 20)
	p.SetFlow(makeFlow(gitflow.BranchTypeRelease))
	p.SetFocused(true)
	p.SetTasks([]domain.Task{
		makeTaskWithMeta("ZA-553", "", "feature", "", 1),
		makeTaskWithMeta("ZA-553-release", "ZA-553", "release", "1.2.0", 1),
	})

	p, _ = p.Update(sendKey("j"))
	if got := p.SelectedTask(); got == nil || got.Phase != "release" {
		t.Fatalf("expected release child selected, got %+v", got)
	}

	_, cmd := p.Update(sendKey("Q"))
	if cmd != nil {
		t.Fatal("Q must be no-op for release child")
	}
}

func TestTasksPanel_TreeMode_QNoOp_WithoutReleaseConfig(t *testing.T) {
	p := NewTasksPanel(80, 20)
	p.SetFlow(makeFlow(gitflow.BranchTypeHotfix))
	p.SetFocused(true)
	p.SetTasks([]domain.Task{makeTaskWithMeta("ZA-553", "", "feature", "", 1)})

	_, cmd := p.Update(sendKey("Q"))
	if cmd != nil {
		t.Fatal("Q must be no-op when release branch type not configured")
	}
}

func TestTasksPanel_SetTasks_PreservesSelection_Tree(t *testing.T) {
	p := NewTasksPanel(80, 20)
	p.SetFlow(makeFlow(gitflow.BranchTypeRelease))
	p.SetFocused(true)
	tasks := []domain.Task{
		makeTaskWithMeta("ZA-553", "", "feature", "", 1),
		makeTaskWithMeta("ZA-553-release", "ZA-553", "release", "1.2.0", 1),
		makeTaskWithMeta("ZA-554", "", "feature", "", 1),
		makeTaskWithMeta("ZA-554-release", "ZA-554", "release", "2.0.0", 1),
	}
	p.SetTasks(tasks)

	p, _ = p.Update(sendKey("j"))
	p, _ = p.Update(sendKey("j"))
	p, _ = p.Update(sendKey("j"))
	if got := p.SelectedTask(); got == nil || got.ID != "ZA-554-release" {
		t.Fatalf("expected ZA-554-release selected before refresh, got %+v", got)
	}

	p.SetTasks(tasks)

	selected := p.SelectedTask()
	if selected == nil {
		t.Fatal("expected selected task after refresh")
	}
	if selected.ID != "ZA-554-release" {
		t.Fatalf("expected selected task ZA-554-release, got %q", selected.ID)
	}
}

func TestTasksPanel_SetTasks_FallsBackToFirstIfRemoved_Tree(t *testing.T) {
	p := NewTasksPanel(80, 20)
	p.SetFlow(makeFlow(gitflow.BranchTypeRelease))
	p.SetFocused(true)
	p.SetTasks([]domain.Task{
		makeTaskWithMeta("ZA-553", "", "feature", "", 1),
		makeTaskWithMeta("ZA-553-release", "ZA-553", "release", "1.2.0", 1),
		makeTaskWithMeta("ZA-554", "", "feature", "", 1),
		makeTaskWithMeta("ZA-554-release", "ZA-554", "release", "2.0.0", 1),
	})

	p, _ = p.Update(sendKey("j"))
	if got := p.SelectedTask(); got == nil || got.ID != "ZA-553-release" {
		t.Fatalf("expected ZA-553-release selected before refresh, got %+v", got)
	}

	p.SetTasks([]domain.Task{
		makeTaskWithMeta("ZA-553", "", "feature", "", 1),
		makeTaskWithMeta("ZA-554", "", "feature", "", 1),
		makeTaskWithMeta("ZA-554-release", "ZA-554", "release", "2.0.0", 1),
	})

	selected := p.SelectedTask()
	if selected == nil {
		t.Fatal("expected selected task after refresh")
	}
	if selected.ID != "ZA-553" {
		t.Fatalf("expected fallback selection ZA-553, got %q", selected.ID)
	}
}

func TestTasksPanel_FlatMode_UnchangedWithoutReleaseOrHotfix(t *testing.T) {
	p := NewTasksPanel(40, 20)
	p.SetFlow(makeFlow(gitflow.BranchTypeFeature))
	p.SetTasks(makeTasks("IN-001", "IN-002", "IN-003"))

	if p.treeMode {
		t.Fatal("expected flat mode when no release/hotfix in flow")
	}
	if got := len(p.list.Items()); got != 3 {
		t.Fatalf("expected 3 flat list items, got %d", got)
	}
}

func TestTasksPanel_FlatMode_UsesDotPaginator(t *testing.T) {
	p := NewTasksPanel(60, 8)
	tasks := make([]domain.Task, 20)
	for i := range tasks {
		id := fmt.Sprintf("IN-%03d", i+1)
		tasks[i] = domain.Task{ID: id, Dir: "/tmp/.tasks/" + id}
	}
	p.SetTasks(tasks)

	if p.list.Paginator.Type != paginator.Dots {
		t.Fatalf("expected flat paginator type dots, got %v", p.list.Paginator.Type)
	}

	view := stripAnsi(p.View())
	if !strings.Contains(view, "•") && !strings.Contains(view, "○") {
		t.Fatalf("expected dot paginator in flat view, got: %q", view)
	}
}

func TestTasksPanel_TreeMode_ViewRendersPaginationDots(t *testing.T) {
	p := NewTasksPanel(80, 10)
	p.SetFlow(makeFlow(gitflow.BranchTypeRelease))

	tasks := make([]domain.Task, 0, 8)
	for i := 1; i <= 8; i++ {
		id := fmt.Sprintf("ZA-%03d", i)
		tasks = append(tasks, makeTaskWithMeta(id, "", "feature", "", 1))
	}
	p.SetTasks(tasks)

	view := stripAnsi(p.View())
	if !strings.Contains(view, "•") && !strings.Contains(view, "○") {
		t.Fatalf("expected dot paginator in tree view, got: %q", view)
	}
}

func TestTasksPanel_TreeMode_PageDown_DoesNotShowPreviousPageItems(t *testing.T) {
	p := NewTasksPanel(80, 10)
	p.SetFlow(makeFlow(gitflow.BranchTypeRelease))
	p.SetFocused(true)

	// With height=10: inner.h=8, rowsHeight=6 → 6 rows per page
	// Each task = group header (1) + task row (1) = 2 rows
	// 4 tasks = 8 rows → spans 2 pages (0-5 and 6-7)
	p.SetTasks([]domain.Task{
		makeTaskWithMeta("ZA-001", "", "feature", "", 1),
		makeTaskWithMeta("ZA-002", "", "feature", "", 1),
		makeTaskWithMeta("ZA-003", "", "feature", "", 1),
		makeTaskWithMeta("ZA-004", "", "feature", "", 1),
	})

	// Initial view should show first page items
	view0 := stripAnsi(p.View())
	if !strings.Contains(view0, "ZA-001") {
		t.Fatalf("page 0 must show ZA-001, got: %q", view0)
	}
	if !strings.Contains(view0, "ZA-002") {
		t.Fatalf("page 0 must show ZA-002, got: %q", view0)
	}

	// Page down (l key moves selection by ~page size)
	p, _ = p.Update(sendKey("l"))

	// After page down we should be on page 1 and NOT see ZA-001/002/003
	view1 := stripAnsi(p.View())
	if !strings.Contains(view1, "ZA-004") {
		t.Fatalf("page 1 must show ZA-004, got: %q", view1)
	}
	if strings.Contains(view1, "ZA-001") {
		t.Fatalf("page 1 must NOT show ZA-001, got: %q", view1)
	}
	if strings.Contains(view1, "ZA-002") {
		t.Fatalf("page 1 must NOT show ZA-002, got: %q", view1)
	}
	if strings.Contains(view1, "ZA-003") {
		t.Fatalf("page 1 must NOT show ZA-003, got: %q", view1)
	}
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

// TestTasksPanel_TreeMode_PageJump_WithChildren verifies that h/l jump exactly
// one visual page at a time even when tasks have child elements (release
// branches). The old code moved by inner.h-1 *selectable* items which skips
// more than one page when groups have multiple children.
//
// Layout with height=10 (inner.h=8, rowsHeight=6):
//
//	rows:
//	  0  group(ZA-001)
//	  1  task(ZA-001)         ← page 0
//	  2  task(ZA-001-release) ←
//	  3  group(ZA-002)        ←
//	  4  task(ZA-002)         ←
//	  5  task(ZA-002-release) ←
//	  6  group(ZA-003)           page 1
//	  7  task(ZA-003)         ←
//	  8  task(ZA-003-release) ←
//	  9  group(ZA-004)        ←
//	  10 task(ZA-004)         ←
//	  11 task(ZA-004-release) ←
//
// pageStarts with maxLines=6: [0, 6]
// page 0 selectable tasks: rows 1,2,4,5
// page 1 selectable tasks: rows 7,8,10,11
func TestTasksPanel_TreeMode_PageJump_WithChildren(t *testing.T) {
	p := NewTasksPanel(80, 10)
	p.SetFlow(makeFlow(gitflow.BranchTypeRelease))
	p.SetFocused(true)

	p.SetTasks([]domain.Task{
		makeTaskWithMeta("ZA-001", "", "feature", "", 1),
		makeTaskWithMeta("ZA-001-release", "ZA-001", "release", "1.0.0", 1),
		makeTaskWithMeta("ZA-002", "", "feature", "", 1),
		makeTaskWithMeta("ZA-002-release", "ZA-002", "release", "1.0.0", 1),
		makeTaskWithMeta("ZA-003", "", "feature", "", 1),
		makeTaskWithMeta("ZA-003-release", "ZA-003", "release", "1.0.0", 1),
		makeTaskWithMeta("ZA-004", "", "feature", "", 1),
		makeTaskWithMeta("ZA-004-release", "ZA-004", "release", "1.0.0", 1),
	})

	// page 0 must show ZA-001 and ZA-002.
	view0 := stripAnsi(p.View())
	if !strings.Contains(view0, "ZA-001") {
		t.Fatalf("page 0 must show ZA-001, got: %q", view0)
	}
	if !strings.Contains(view0, "ZA-002") {
		t.Fatalf("page 0 must show ZA-002, got: %q", view0)
	}

	// l → page 1.
	p, _ = p.Update(sendKey("l"))

	view1 := stripAnsi(p.View())
	if !strings.Contains(view1, "ZA-003") {
		t.Fatalf("page 1 must show ZA-003, got: %q", view1)
	}
	if !strings.Contains(view1, "ZA-004") {
		t.Fatalf("page 1 must show ZA-004, got: %q", view1)
	}
	if strings.Contains(view1, "ZA-001") {
		t.Fatalf("page 1 must NOT show ZA-001, got: %q", view1)
	}
	if strings.Contains(view1, "ZA-002") {
		t.Fatalf("page 1 must NOT show ZA-002, got: %q", view1)
	}

	// l on last page → no change.
	p2, _ := p.Update(sendKey("l"))
	view2 := stripAnsi(p2.View())
	if !strings.Contains(view2, "ZA-003") {
		t.Fatalf("extra l must stay on page 1, got: %q", view2)
	}

	// h → back to page 0.
	p, _ = p.Update(sendKey("h"))
	view3 := stripAnsi(p.View())
	if !strings.Contains(view3, "ZA-001") {
		t.Fatalf("h must return to page 0, got: %q", view3)
	}
	if strings.Contains(view3, "ZA-003") {
		t.Fatalf("page 0 after h must NOT show ZA-003, got: %q", view3)
	}
}

func TestTasksPanel_FlatMode_PageJump_NoBoundaryWrap(t *testing.T) {
	// Flat mode h on page 0 must not wrap to last page.
	p := NewTasksPanel(60, 8)
	tasks := make([]domain.Task, 20)
	for i := range tasks {
		tasks[i] = domain.Task{ID: fmt.Sprintf("IN-%03d", i+1), Dir: "/tmp/.tasks/" + fmt.Sprintf("IN-%03d", i+1)}
	}
	p.SetTasks(tasks)
	p.SetFocused(true)

	// h on page 0 → must stay on page 0.
	p, _ = p.Update(sendKey("h"))
	view := stripAnsi(p.View())
	if !strings.Contains(view, "IN-001") {
		t.Fatalf("h on first page must stay on page 0, got: %q", view)
	}
}

func TestTreePageStarts_NoOrphanedGroupHeader(t *testing.T) {
	// rows: group, task, task, task, group, task
	// With maxLines=4 the naive boundary lands at index 4 (a group header),
	// meaning that header appears at the end of page 0 and its child on page 1.
	// treePageStarts must shift the boundary to index 3 instead.
	rows := []treeRow{
		{kind: treeRowKindGroup},  // 0
		{kind: treeRowKindTask},   // 1
		{kind: treeRowKindTask},   // 2
		{kind: treeRowKindTask},   // 3
		{kind: treeRowKindGroup},  // 4 ← would be last on page 0 with naive split
		{kind: treeRowKindTask},   // 5
	}

	starts := treePageStarts(rows, 4)

	if starts[0] != 0 {
		t.Fatalf("first start must be 0, got %d", starts[0])
	}
	// Boundary must not fall on a group row.
	for i := 1; i < len(starts); i++ {
		boundary := starts[i]
		if rows[boundary].kind == treeRowKindGroup && boundary > 0 && rows[boundary-1].kind != treeRowKindGroup {
			// Group header at page start is fine; what we forbid is ending the
			// *previous* page with a group header.
		}
		if boundary > 0 && rows[boundary-1].kind == treeRowKindGroup {
			t.Errorf("page boundary at %d ends previous page on a group header (row %d)", boundary, boundary-1)
		}
	}
}

func TestTreePageStarts_GroupHeaderAlwaysWithChild(t *testing.T) {
	// rows: task, task, task, group, task  (maxLines=3)
	// Naive page 0 = rows 0-2, page 1 = rows 3-4.
	// Row 3 is a group header — that's fine as the FIRST row of page 1.
	// No adjustment needed here; the fix should not break this case.
	rows := []treeRow{
		{kind: treeRowKindTask},  // 0
		{kind: treeRowKindTask},  // 1
		{kind: treeRowKindTask},  // 2
		{kind: treeRowKindGroup}, // 3
		{kind: treeRowKindTask},  // 4
	}

	starts := treePageStarts(rows, 3)

	// Expect: [0, 3]
	expected := []int{0, 3}
	if len(starts) != len(expected) {
		t.Fatalf("expected starts %v, got %v", expected, starts)
	}
	for i, v := range expected {
		if starts[i] != v {
			t.Errorf("starts[%d]: want %d got %d", i, v, starts[i])
		}
	}
}
