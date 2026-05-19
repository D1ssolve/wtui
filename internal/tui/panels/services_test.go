package panels

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/diss0x/wtui/internal/domain"
)

func makeServices(taskID string, names ...string) (string, []domain.Service) {
	svcs := make([]domain.Service, len(names))
	for i, n := range names {
		svcs[i] = domain.Service{
			Name:         n,
			Branch:       "feature/" + taskID,
			BaseBranch:   "main",
			WorktreePath: "/tmp/.tasks/" + taskID + "/" + n,
		}
	}
	return taskID, svcs
}

func TestServicesPanel_New_EmptyByDefault(t *testing.T) {
	p := NewServicesPanel(60, 20)
	if p.taskID != "" {
		t.Errorf("expected empty taskID, got %q", p.taskID)
	}
	if len(p.list.Items()) != 0 {
		t.Errorf("expected 0 items, got %d", len(p.list.Items()))
	}
	if p.focused {
		t.Error("expected unfocused by default")
	}
}

func TestServicesPanel_SetServices_PopulatesList(t *testing.T) {
	p := NewServicesPanel(60, 20)
	tid, svcs := makeServices("IN-001", "collection", "databridge", "reporting")
	p.SetServices(tid, svcs)

	if p.taskID != "IN-001" {
		t.Errorf("expected taskID=IN-001, got %q", p.taskID)
	}
	if got := len(p.list.Items()); got != 3 {
		t.Errorf("expected 3 items, got %d", got)
	}
}

func TestServicesPanel_SetServices_ClearsOnEmpty(t *testing.T) {
	p := NewServicesPanel(60, 20)
	tid, svcs := makeServices("IN-001", "collection")
	p.SetServices(tid, svcs)

	p.SetServices("IN-001", nil)
	if len(p.list.Items()) != 0 {
		t.Error("SetServices with nil should clear the list")
	}
}

func TestServicesPanel_SetSize(t *testing.T) {
	p := NewServicesPanel(60, 20)
	p.SetSize(100, 30)
	if p.width != 100 || p.height != 30 {
		t.Errorf("SetSize: expected 100×30, got %d×%d", p.width, p.height)
	}
}

func TestServicesPanel_SetFocused(t *testing.T) {
	p := NewServicesPanel(60, 20)
	p.SetFocused(true)
	if !p.focused {
		t.Error("SetFocused(true) should set focused")
	}
	p.SetFocused(false)
	if p.focused {
		t.Error("SetFocused(false) should clear focused")
	}
}

func TestServicesPanel_KeyA_EmitsOpenAddServiceMsg(t *testing.T) {
	p := NewServicesPanel(60, 20)
	tid, svcs := makeServices("IN-001", "collection", "databridge")
	p.SetServices(tid, svcs)
	p.SetFocused(true)

	_, cmd := p.Update(sendKey("a"))
	if cmd == nil {
		t.Fatal("a key should return a cmd")
	}
	msg := cmd()
	got, ok := msg.(OpenAddServiceMsg)
	if !ok {
		t.Fatalf("expected OpenAddServiceMsg, got %T", msg)
	}
	if got.TaskID != "IN-001" {
		t.Errorf("expected TaskID=IN-001, got %s", got.TaskID)
	}
	if len(got.ExistingServices) != 2 {
		t.Errorf("expected 2 existing services, got %d", len(got.ExistingServices))
	}
	if got.ExistingServices[0] != "collection" || got.ExistingServices[1] != "databridge" {
		t.Errorf("expected ExistingServices=[collection, databridge], got %v", got.ExistingServices)
	}
}

func TestServicesPanel_KeyEsc_EmitsFocusTasksMsg(t *testing.T) {
	p := NewServicesPanel(60, 20)
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

func TestServicesPanel_KeyJ_MovesDown(t *testing.T) {
	p := NewServicesPanel(60, 20)
	tid, svcs := makeServices("IN-001", "collection", "databridge")
	p.SetServices(tid, svcs)
	p.SetFocused(true)

	initial := p.list.Index()
	p, _ = p.Update(sendKey("j"))
	if p.list.Index() == initial && len(p.list.Items()) > 1 {
		t.Error("j key should move cursor down")
	}
}

func TestServicesPanel_KeyK_MovesUp(t *testing.T) {
	p := NewServicesPanel(60, 20)
	tid, svcs := makeServices("IN-001", "collection", "databridge")
	p.SetServices(tid, svcs)
	p.SetFocused(true)
	p.list.Select(1)

	p, _ = p.Update(sendKey("k"))
	if p.list.Index() == 1 {
		t.Error("k key should move cursor up")
	}
}

func TestServicesPanel_Unfocused_EscNoOp(t *testing.T) {
	p := NewServicesPanel(60, 20)

	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(FocusTasksMsg); ok {
			t.Error("unfocused panel should not emit FocusTasksMsg on Esc")
		}
	}
}

func TestServicesPanel_View_NoTaskSelected_ShowsPlaceholder(t *testing.T) {
	p := NewServicesPanel(60, 20)
	view := stripAnsi(p.View())
	if !strings.Contains(view, "Select a task to view services") {
		t.Errorf("expected placeholder when no task selected, got: %q", view)
	}
}

func TestServicesPanel_View_TaskSelectedNoServices_ShowsAddHint(t *testing.T) {
	p := NewServicesPanel(60, 20)
	p.SetServices("IN-001", nil)
	view := stripAnsi(p.View())
	if !strings.Contains(view, "No services in this task") {
		t.Errorf("expected no-services placeholder, got: %q", view)
	}
	if !strings.Contains(view, "[a]") {
		t.Errorf("expected [a] hint in placeholder, got: %q", view)
	}
}

func TestServicesPanel_View_ContainsTitle(t *testing.T) {
	p := NewServicesPanel(60, 20)
	tid, svcs := makeServices("IN-001", "collection")
	p.SetServices(tid, svcs)
	view := stripAnsi(p.View())
	if !strings.Contains(view, "Services") {
		t.Errorf("expected 'Services' in title, got: %q", view)
	}
	if !strings.Contains(view, "IN-001") {
		t.Errorf("expected task ID in title, got: %q", view)
	}
}

func TestServicesPanel_View_ItemCount(t *testing.T) {
	p := NewServicesPanel(60, 20)
	tid, svcs := makeServices("IN-001", "collection", "databridge")
	p.SetServices(tid, svcs)
	view := stripAnsi(p.View())

	if !strings.Contains(view, "/2]") {
		t.Errorf("expected total count in title, got: %q", view)
	}
}

func TestServicesPanel_View_DirtyService_ShowsWarningIcon(t *testing.T) {
	p := NewServicesPanel(80, 20)
	p.SetFocused(false)
	svcs := []domain.Service{
		{
			Name:         "collection",
			Branch:       "feature/IN-001",
			BaseBranch:   "main",
			WorktreePath: "/tmp/.tasks/IN-001/collection",
			IsDirty:      true,
		},
	}
	p.SetServices("IN-001", svcs)
	view := p.View()
	if !strings.Contains(view, "⚠") {
		t.Errorf("dirty service should show ⚠ icon, got: %q", view)
	}
}

func TestServicesPanel_View_CleanService_ShowsCheckIcon(t *testing.T) {
	p := NewServicesPanel(80, 20)
	svcs := []domain.Service{
		{
			Name:         "collection",
			Branch:       "feature/IN-001",
			BaseBranch:   "main",
			WorktreePath: "/tmp/.tasks/IN-001/collection",
			IsDirty:      false,
		},
	}
	p.SetServices("IN-001", svcs)
	view := p.View()
	if !strings.Contains(view, "✓") {
		t.Errorf("clean service should show ✓ icon, got: %q", view)
	}
}

func TestServicesPanel_SelectedService_NilOnEmpty(t *testing.T) {
	p := NewServicesPanel(60, 20)
	if p.SelectedService() != nil {
		t.Error("expected nil SelectedService on empty list")
	}
}

func TestServicesPanel_SelectedService_ReturnsFirstByDefault(t *testing.T) {
	p := NewServicesPanel(60, 20)
	tid, svcs := makeServices("IN-001", "collection", "databridge")
	p.SetServices(tid, svcs)

	svc := p.SelectedService()
	if svc == nil {
		t.Fatal("expected non-nil SelectedService")
	}
	if svc.Name != "collection" {
		t.Errorf("expected first service 'collection', got %q", svc.Name)
	}
}

func TestServicesPanel_SelectedService_ReturnsSecondAfterMove(t *testing.T) {
	p := NewServicesPanel(60, 20)
	tid, svcs := makeServices("IN-001", "collection", "databridge")
	p.SetServices(tid, svcs)
	p.SetFocused(true)

	p, _ = p.Update(sendKey("j"))
	svc := p.SelectedService()
	if svc == nil {
		t.Fatal("expected non-nil SelectedService")
	}
	if svc.Name != "databridge" {
		t.Errorf("expected second service 'databridge', got %q", svc.Name)
	}
}

func TestServicesPanel_CtrlS_EmitsOpenStashDialogMsg(t *testing.T) {
	p := NewServicesPanel(60, 20)
	tid, svcs := makeServices("IN-001", "collection")
	p.SetServices(tid, svcs)
	p.SetFocused(true)

	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("ctrl+s should return a cmd")
	}
	msg := cmd()
	got, ok := msg.(OpenStashDialogMsg)
	if !ok {
		t.Fatalf("expected OpenStashDialogMsg, got %T", msg)
	}
	if got.TaskID != "IN-001" {
		t.Errorf("expected TaskID=IN-001, got %s", got.TaskID)
	}
	if got.ServiceName != "collection" {
		t.Errorf("expected ServiceName=collection, got %s", got.ServiceName)
	}
	if got.Pop {
		t.Error("expected Pop=false for ctrl+s (stash)")
	}
}

func TestServicesPanel_CtrlU_EmitsOpenStashDialogMsgPop(t *testing.T) {
	p := NewServicesPanel(60, 20)
	tid, svcs := makeServices("IN-001", "collection")
	p.SetServices(tid, svcs)
	p.SetFocused(true)

	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if cmd == nil {
		t.Fatal("ctrl+u should return a cmd")
	}
	msg := cmd()
	got, ok := msg.(OpenStashDialogMsg)
	if !ok {
		t.Fatalf("expected OpenStashDialogMsg, got %T", msg)
	}
	if got.TaskID != "IN-001" {
		t.Errorf("expected TaskID=IN-001, got %s", got.TaskID)
	}
	if got.ServiceName != "collection" {
		t.Errorf("expected ServiceName=collection, got %s", got.ServiceName)
	}
	if !got.Pop {
		t.Error("expected Pop=true for ctrl+u (unstash)")
	}
}

func TestServicesPanel_CtrlS_NoServiceSelected_ReturnsNil(t *testing.T) {
	p := NewServicesPanel(60, 20)
	p.SetFocused(true)

	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(OpenStashDialogMsg); ok {
			t.Error("ctrl+s with no service should not emit OpenStashDialogMsg")
		}
	}
}

func TestServicesPanel_CtrlU_NoServiceSelected_ReturnsNil(t *testing.T) {
	p := NewServicesPanel(60, 20)
	p.SetFocused(true)

	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(OpenStashDialogMsg); ok {
			t.Error("ctrl+u with no service should not emit OpenStashDialogMsg")
		}
	}
}

func TestServicesPanel_FilterMode_EntersFilterMode(t *testing.T) {
	p := NewServicesPanel(60, 20)
	tid, svcs := makeServices("IN-001", "collection", "databridge", "reporting")
	p.SetServices(tid, svcs)
	p.SetFocused(true)

	if p.FilterActive() {
		t.Error("Panel should not be in filter mode initially")
	}

	p, _ = p.Update(sendKey("f"))

	if !p.FilterActive() {
		t.Error("Panel should be in filter mode after pressing 'f'")
	}
}

func TestServicesPanel_FilterMode_NoFilterIndicatorWhenNotFiltering(t *testing.T) {
	p := NewServicesPanel(60, 20)
	tid, svcs := makeServices("IN-001", "collection", "databridge")
	p.SetServices(tid, svcs)
	p.SetFocused(true)

	if p.FilterActive() {
		t.Error("Panel should not be in filter mode initially")
	}
}

func TestServicesPanel_FilterMode_EscClearsFilter(t *testing.T) {
	p := NewServicesPanel(60, 20)
	tid, svcs := makeServices("IN-001", "collection", "databridge", "reporting")
	p.SetServices(tid, svcs)
	p.SetFocused(true)

	p, _ = p.Update(sendKey("f"))
	p, _ = p.Update(sendKey("c"))
	p, _ = p.Update(sendKey("o"))

	if !p.FilterActive() {
		t.Error("Panel should be in filter mode after typing")
	}

	p, _ = p.Update(sendSpecialKey(tea.KeyEsc))

	if p.FilterActive() {
		t.Error("Panel should NOT be in filter mode after ESC clears filter")
	}
}

func TestServicesPanel_FilterMode_EnterExitsFilterMode(t *testing.T) {
	p := NewServicesPanel(60, 20)
	tid, svcs := makeServices("IN-001", "collection", "databridge", "reporting")
	p.SetServices(tid, svcs)
	p.SetFocused(true)

	p, _ = p.Update(sendKey("f"))
	p, _ = p.Update(sendKey("c"))
	p, _ = p.Update(sendKey("o"))

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
