package panels

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
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

func testResolvedFlow() *gitflow.ResolvedGitFlow {
	return &gitflow.ResolvedGitFlow{
		DefaultBranchType: gitflow.BranchTypeFeature,
		BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
			gitflow.BranchTypeFeature: {Prefixes: []string{"feature/"}},
			gitflow.BranchTypeHotfix:  {Prefixes: []string{"hotfix/"}},
		},
	}
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

func TestServicesPanel_KeyV_EmitsValidateTaskMsg(t *testing.T) {
	p := NewServicesPanel(60, 20)
	tid, svcs := makeServices("IN-001", "collection")
	p.SetServices(tid, svcs)
	p.SetFocused(true)

	_, cmd := p.Update(sendKey("v"))
	if cmd == nil {
		t.Fatal("v key should return a cmd")
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

func TestServicesPanel_KeyM_EmitsOpenForgeMenuMsg(t *testing.T) {
	p := NewServicesPanel(60, 20)
	tid, svcs := makeServices("IN-001", "collection")
	p.SetServices(tid, svcs)
	p.SetFocused(true)

	_, cmd := p.Update(sendKey("m"))
	if cmd == nil {
		t.Fatal("m key should return a cmd")
	}
	msg := cmd()
	got, ok := msg.(OpenForgeMenuMsg)
	if !ok {
		t.Fatalf("expected OpenForgeMenuMsg, got %T", msg)
	}
	if got.TaskID != "IN-001" || got.ServiceName != "collection" {
		t.Fatalf("unexpected payload: %+v", got)
	}
	if got.Provider != forge.ForgeProviderUnknown {
		t.Fatalf("Provider=%q, want unknown", got.Provider)
	}
}

func TestServicesPanel_KeyP_EmitsForgePipelineStatusMsg(t *testing.T) {
	p := NewServicesPanel(60, 20)
	tid, svcs := makeServices("IN-001", "collection")
	p.SetServices(tid, svcs)
	p.SetFocused(true)

	_, cmd := p.Update(sendKey("p"))
	if cmd == nil {
		t.Fatal("p key should return a cmd")
	}
	msg := cmd()
	got, ok := msg.(ForgePipelineStatusMsg)
	if !ok {
		t.Fatalf("expected ForgePipelineStatusMsg, got %T", msg)
	}
	if got.TaskID != "IN-001" || got.ServiceName != "collection" {
		t.Fatalf("unexpected payload: %+v", got)
	}
}

func TestServicesPanel_KeyShiftP_EmitsPushServiceMsgWhenNoLazygit(t *testing.T) {
	p := NewServicesPanel(60, 20)
	tid, svcs := makeServices("IN-001", "collection")
	p.SetServices(tid, svcs)
	p.SetFocused(true)
	p.SetLazygitAvailable(false)

	_, cmd := p.Update(sendKey("P"))
	if cmd == nil {
		t.Fatal("P key should return push command")
	}
	msg := cmd()
	got, ok := msg.(PushServiceMsg)
	if !ok {
		t.Fatalf("expected PushServiceMsg, got %T", msg)
	}
	if got.TaskID != "IN-001" || got.ServiceName != "collection" {
		t.Fatalf("unexpected payload: %+v", got)
	}
}

func TestServicesPanel_KeyG_WhenLazygitAvailable_EmitsOpenLazygitServiceMsg(t *testing.T) {
	p := NewServicesPanel(60, 20)
	p.SetLazygitAvailable(true)
	tid, svcs := makeServices("IN-001", "collection", "databridge")
	svcs[0].Stale = true
	p.SetServices(tid, svcs)
	p.SetFocused(true)

	_, cmd := p.Update(sendKey("g"))
	if cmd == nil {
		t.Fatal("g key should return a cmd when lazygit is available")
	}

	msg := cmd()
	got, ok := msg.(OpenLazygitServiceMsg)
	if !ok {
		t.Fatalf("expected OpenLazygitServiceMsg, got %T", msg)
	}
	if got.TaskID != "IN-001" {
		t.Errorf("TaskID = %q, want IN-001", got.TaskID)
	}
	if got.ServiceName != "collection" {
		t.Errorf("ServiceName = %q, want collection", got.ServiceName)
	}
	if got.WorktreePath != "/tmp/.tasks/IN-001/collection" {
		t.Errorf("WorktreePath = %q, want /tmp/.tasks/IN-001/collection", got.WorktreePath)
	}
	if !got.Stale {
		t.Error("Stale = false, want true")
	}
}

func TestServicesPanel_KeyG_WhenLazygitAvailableNoServiceSelected_EmitsNoSelectionMessage(t *testing.T) {
	p := NewServicesPanel(60, 20)
	p.SetLazygitAvailable(true)
	p.SetServices("IN-001", nil)
	p.SetFocused(true)

	_, cmd := p.Update(sendKey("g"))
	if cmd == nil {
		t.Fatal("g key should return a cmd when lazygit is available but no service is selected")
	}

	msg := cmd()
	got, ok := msg.(OpenLazygitServiceMsg)
	if !ok {
		t.Fatalf("expected OpenLazygitServiceMsg, got %T", msg)
	}
	if got.TaskID != "IN-001" {
		t.Errorf("TaskID = %q, want IN-001", got.TaskID)
	}
	if got.ServiceName != "" {
		t.Errorf("ServiceName = %q, want empty", got.ServiceName)
	}
	if got.WorktreePath != "" {
		t.Errorf("WorktreePath = %q, want empty", got.WorktreePath)
	}
}

func TestServicesPanel_KeyG_WhenLazygitUnavailable_EmitsNoLazygitMessage(t *testing.T) {
	p := NewServicesPanel(60, 20)
	tid, svcs := makeServices("IN-001", "collection")
	p.SetServices(tid, svcs)
	p.SetFocused(true)

	_, cmd := p.Update(sendKey("g"))
	if cmd == nil {
		return
	}
	if _, ok := cmd().(OpenLazygitServiceMsg); ok {
		t.Fatal("g key emitted OpenLazygitServiceMsg when lazygit unavailable")
	}
}

func TestServicesPanel_KeyG_FilterMode_DoesNotEmitLazygitMessage(t *testing.T) {
	p := NewServicesPanel(60, 20)
	p.SetLazygitAvailable(true)
	tid, svcs := makeServices("IN-001", "collection")
	p.SetServices(tid, svcs)
	p.SetFocused(true)
	p, _ = p.Update(sendKey("f"))

	_, cmd := p.Update(sendKey("g"))
	if cmd == nil {
		return
	}
	if _, ok := cmd().(OpenLazygitServiceMsg); ok {
		t.Fatal("filter mode g emitted OpenLazygitServiceMsg")
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

func TestServiceDelegate_UsesTwoLines(t *testing.T) {
	if got := (serviceDelegate{}).Height(); got != 2 {
		t.Fatalf("Height() = %d, want 2", got)
	}
}

func TestServicesPanel_View_ShowsCompactGitStateWithoutBranchOrPath(t *testing.T) {
	p := NewServicesPanel(60, 20)
	p.SetServices("IN-001", []domain.Service{{
		Name:         "collection",
		Branch:       "feature/IN-001",
		BaseBranch:   "main",
		WorktreePath: "/tmp/.tasks/IN-001/collection",
		Ahead:        2,
		Behind:       1,
	}})

	view := stripAnsi(p.View())
	for _, want := range []string{"clean", "↑2", "↓1"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q: %q", want, view)
		}
	}
	for _, unwanted := range []string{"branch:", "path:", "feature/IN-001"} {
		if strings.Contains(view, unwanted) {
			t.Errorf("view contains obsolete %q: %q", unwanted, view)
		}
	}
}

func TestServicesPanel_View_ShowsModifiedAndStaleStates(t *testing.T) {
	tests := []struct {
		name string
		svc  domain.Service
		want string
	}{
		{name: "modified", svc: domain.Service{Name: "api", IsDirty: true}, want: "modified"},
		{name: "stale", svc: domain.Service{Name: "api", Stale: true}, want: "worktree missing"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := NewServicesPanel(60, 20)
			p.SetServices("IN-001", []domain.Service{tc.svc})
			view := stripAnsi(p.View())
			if !strings.Contains(view, tc.want) {
				t.Fatalf("view = %q, want state %q", view, tc.want)
			}
		})
	}
}

func TestServicesPanel_View_LongContentDoesNotExceedPanelWidth(t *testing.T) {
	const width = 30
	p := NewServicesPanel(width, 12)
	p.SetServices("IN-001-WITH-A-VERY-LONG-TASK-ID", []domain.Service{{
		Name:         "очень-длинное-название-сервиса-которое-не-помещается",
		Branch:       "feature/IN-001-WITH-A-VERY-LONG-TASK-ID",
		WorktreePath: "/tmp/an/extremely/long/worktree/path/service",
	}})

	view := p.View()
	for i, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > width {
			t.Errorf("line %d width = %d, want <= %d: %q", i, got, width, stripAnsi(line))
		}
	}
	if !strings.Contains(stripAnsi(view), "…") {
		t.Fatalf("view = %q, want truncation ellipsis", stripAnsi(view))
	}
}

func TestServicesPanel_View_NoPresetBadge(t *testing.T) {
	p := NewServicesPanel(100, 20)
	p.SetGitFlow(testResolvedFlow(), "git-flow", true)
	p.SetServices("IN-001", []domain.Service{{
		Name:         "collection",
		Branch:       "feature/ABC-123",
		BaseBranch:   "develop",
		WorktreePath: "/tmp/.tasks/IN-001/collection",
	}})

	view := stripAnsi(p.View())
	if strings.Contains(view, "[git-flow]") {
		t.Fatalf("unexpected git-flow preset badge, got: %q", view)
	}
	if strings.Contains(view, "[feature]") {
		t.Fatalf("unexpected feature branch type badge, got: %q", view)
	}
}

func TestServicesPanel_View_NoHotfixBadge(t *testing.T) {
	p := NewServicesPanel(100, 20)
	p.SetGitFlow(testResolvedFlow(), "git-flow", true)
	p.SetServices("IN-001", []domain.Service{{
		Name:         "collection",
		Branch:       "hotfix/1.2.1",
		BaseBranch:   "master",
		WorktreePath: "/tmp/.tasks/IN-001/collection",
	}})

	view := stripAnsi(p.View())
	if strings.Contains(view, "[hotfix]") {
		t.Fatalf("unexpected hotfix branch type badge, got: %q", view)
	}
}

func TestServicesPanel_View_NoForgeIndicator(t *testing.T) {
	p := NewServicesPanel(100, 20)
	p.SetForgeClients(
		map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitLab: nil},
		&config.ForgeConfig{GitLabHost: "gitlab.com", GitHubHost: "github.com"},
	)
	p.SetServices("IN-001", []domain.Service{{
		Name:         "collection",
		RemoteURL:    "git@gitlab.com:group/collection.git",
		Branch:       "feature/ABC-123",
		BaseBranch:   "develop",
		WorktreePath: "/tmp/.tasks/IN-001/collection",
	}})

	view := stripAnsi(p.View())
	if strings.Contains(view, "[forge]") {
		t.Fatalf("unexpected forge indicator, got: %q", view)
	}
}

func TestServicesPanel_View_NoGitFlowConfig_NoBadges(t *testing.T) {
	p := NewServicesPanel(100, 20)
	p.SetServices("IN-001", []domain.Service{{
		Name:         "collection",
		Branch:       "feature/ABC-123",
		BaseBranch:   "develop",
		WorktreePath: "/tmp/.tasks/IN-001/collection",
	}})

	view := stripAnsi(p.View())
	if strings.Contains(view, "[git-flow]") {
		t.Fatalf("unexpected preset badge without git_flow config: %q", view)
	}
	if strings.Contains(view, "[feature]") {
		t.Fatalf("unexpected branch badge without git_flow config: %q", view)
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
