package panels

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
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
