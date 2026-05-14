package tui

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/diss0x/wtui/internal/config"
	"github.com/diss0x/wtui/internal/domain"
	"github.com/diss0x/wtui/internal/task"
	"github.com/diss0x/wtui/internal/tui/modal"
	"github.com/diss0x/wtui/internal/tui/panels"
)

type mockManager struct {
	listTasksResult    []domain.Task
	listTasksErr       error
	listServicesResult []domain.Service
	listServicesErr    error
	reposResult        []domain.Repo
	reposErr           error
	repoRefreshArgs    []bool
}

var _ task.Manager = (*mockManager)(nil)

func (m *mockManager) Init(_ context.Context, _ task.InitParams) error     { return nil }
func (m *mockManager) Add(_ context.Context, _ task.AddParams) error       { return nil }
func (m *mockManager) Remove(_ context.Context, _ string, _, _ bool) error { return nil }

func (m *mockManager) List(_ context.Context) ([]domain.Task, error) {
	return m.listTasksResult, m.listTasksErr
}

func (m *mockManager) ListServices(_ context.Context, _ string) ([]domain.Service, error) {
	return m.listServicesResult, m.listServicesErr
}

func (m *mockManager) Repos(_ context.Context, refresh bool) ([]domain.Repo, error) {
	m.repoRefreshArgs = append(m.repoRefreshArgs, refresh)
	return m.reposResult, m.reposErr
}

func (m *mockManager) SyncTask(_ context.Context, _ string, _ task.SyncStrategy, lineCh chan<- string) error {
	close(lineCh)
	return nil
}

func (m *mockManager) PushTask(_ context.Context, _ string, lineCh chan<- string) error {
	close(lineCh)
	return nil
}

func (m *mockManager) PushService(_ context.Context, _, _ string, _ chan<- string) error { return nil }

func (m *mockManager) StashService(_ context.Context, _, _ string, _ bool) error { return nil }

func (m *mockManager) RemoveService(_ context.Context, _, _ string, _ bool) error { return nil }

func newTestConfig() *config.Config {
	cfg := &config.Config{
		RootDir:          "/tmp/wtui-test",
		TasksRoot:        "/tmp/wtui-test/.tasks",
		BranchPrefix:     "feature/",
		Editor:           "code",
		DiscoveryDepth:   4,
		OutputPanelLines: 6,
		LogLevel:         "INFO",
	}
	return cfg.Effective()
}

func newTestModel(t *testing.T, mgr task.Manager) Model {
	t.Helper()
	m, err := New(newTestConfig(), mgr, slog.Default())
	if err != nil {
		t.Fatalf("tui.New: unexpected error: %v", err)
	}
	return m
}

func sendWindowSize(m Model, w, h int) Model {
	updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return updated.(Model)
}

func TestModelInit(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	if m.focus != FocusTasks {
		t.Errorf("initial focus: expected FocusTasks (%d), got %d", FocusTasks, m.focus)
	}
	if m.mgr == nil {
		t.Error("mgr must not be nil after construction")
	}
	if m.cfg == nil {
		t.Error("cfg must not be nil after construction")
	}
	if m.logger == nil {
		t.Error("logger must not be nil after construction")
	}
	if m.ready {
		t.Error("model must not be ready before receiving the first WindowSizeMsg")
	}
}

func TestNew_NilCfg_ReturnsError(t *testing.T) {
	_, err := New(nil, &mockManager{}, slog.Default())
	if err == nil {
		t.Fatal("expected error when cfg is nil, got nil")
	}
}

func TestNew_NilMgr_ReturnsError(t *testing.T) {
	_, err := New(newTestConfig(), nil, slog.Default())
	if err == nil {
		t.Fatal("expected error when mgr is nil, got nil")
	}
}

func TestNew_NilLogger_ReturnsError(t *testing.T) {
	_, err := New(newTestConfig(), &mockManager{}, nil)
	if err == nil {
		t.Fatal("expected error when logger is nil, got nil")
	}
}

func TestView_BeforeWindowSize_ReturnsLoading(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	view := m.View()
	if view != "Loading..." {
		t.Errorf("View() before WindowSizeMsg: expected %q, got %q", "Loading...", view)
	}
}

func TestUpdate_WindowSizeMsg_SetsReady(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)

	if !m.ready {
		t.Error("model should be ready after receiving WindowSizeMsg")
	}
	if m.width != 120 {
		t.Errorf("width: expected 120, got %d", m.width)
	}
	if m.height != 40 {
		t.Errorf("height: expected 40, got %d", m.height)
	}
}

func TestView_AfterWindowSize_NotLoading(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)

	view := m.View()
	if view == "Loading..." {
		t.Error("View() after WindowSizeMsg must not return 'Loading...'")
	}
}

func TestInit_ReturnsCmd(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() must return a non-nil tea.Cmd")
	}
}

func TestLoadReposCmdUsesCachedReposByDefault(t *testing.T) {
	mgr := &mockManager{reposResult: []domain.Repo{{Name: "api", Path: "/repo/api"}}}
	msg := loadReposCmd(mgr, false)()
	loaded, ok := msg.(ReposLoadedMsg)
	if !ok {
		t.Fatalf("expected ReposLoadedMsg, got %T", msg)
	}
	if loaded.Err != nil {
		t.Fatalf("ReposLoadedMsg err = %v", loaded.Err)
	}
	if len(mgr.repoRefreshArgs) != 1 || mgr.repoRefreshArgs[0] {
		t.Fatalf("repo refresh args = %v, want [false]", mgr.repoRefreshArgs)
	}
}

func TestLoadReposCmdForceRefreshesRepos(t *testing.T) {
	mgr := &mockManager{reposResult: []domain.Repo{{Name: "fresh", Path: "/repo/fresh"}}}
	msg := loadReposCmd(mgr, true)()
	loaded, ok := msg.(ReposLoadedMsg)
	if !ok {
		t.Fatalf("expected ReposLoadedMsg, got %T", msg)
	}
	if loaded.Err != nil {
		t.Fatalf("ReposLoadedMsg err = %v", loaded.Err)
	}
	if len(mgr.repoRefreshArgs) != 1 || !mgr.repoRefreshArgs[0] {
		t.Fatalf("repo refresh args = %v, want [true]", mgr.repoRefreshArgs)
	}
}

func TestUpdate_QuitKey_ReturnsQuit(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("Ctrl+C must return a cmd")
	}

	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("Ctrl+C cmd must produce tea.QuitMsg, got %T", msg)
	}
}

func TestUpdate_Tab_CyclesFocusForward(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	if m.focus != FocusTasks {
		t.Fatalf("expected FocusTasks initially, got %v", m.focus)
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.focus != FocusOutput {
		t.Errorf("after Tab: expected FocusOutput, got %v", m.focus)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.focus != FocusTasks {
		t.Errorf("after Tab×2 (wrap): expected FocusTasks, got %v", m.focus)
	}
}

func TestUpdate_ShiftTab_CyclesFocusBackward(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = updated.(Model)
	if m.focus != FocusOutput {
		t.Errorf("Shift+Tab from FocusTasks: expected FocusOutput, got %v", m.focus)
	}
}

func TestUpdate_HelpKey_OpensHelpModal(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)
	if m.modal == nil {
		t.Fatal("'?' must open a modal")
	}
	if _, ok := m.modal.(*modal.HelpOverlay); !ok {
		t.Errorf("expected *modal.HelpOverlay, got %T", m.modal)
	}
}

func TestUpdate_CloseModalMsg_NilsModal(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)
	if m.modal == nil {
		t.Fatal("precondition failed: modal should be open")
	}

	updated, _ = m.Update(modal.CloseModalMsg{})
	m = updated.(Model)
	if m.modal != nil {
		t.Error("CloseModalMsg must set modal to nil")
	}
}

func TestUpdate_OpenInitDialogMsg_OpensInitDialog(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	m.repos = []domain.Repo{{Name: "svc1", Path: "/tmp/svc1"}}

	updated, _ := m.Update(panels.OpenInitDialogMsg{})
	m = updated.(Model)
	if m.modal == nil {
		t.Fatal("OpenInitDialogMsg must open a modal")
	}
	if _, ok := m.modal.(*modal.InitDialog); !ok {
		t.Errorf("expected *modal.InitDialog, got %T", m.modal)
	}
}

func TestUpdate_OpenInitDialogMsg_NoRepos_Pending(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)

	updated, cmd := m.Update(panels.OpenInitDialogMsg{})
	m = updated.(Model)
	if m.modal != nil {
		t.Fatal("modal must be nil when repos not loaded yet")
	}
	if !m.initDialogPending {
		t.Fatal("initDialogPending must be true")
	}
	if cmd == nil {
		t.Fatal("must return a cmd to load repos")
	}
	if !strings.Contains(m.outputPanel.View(), "Loading repository cache for init dialog...") {
		t.Fatalf("output should mention deferred init repo load, got %q", m.outputPanel.View())
	}
}

func TestUpdate_OpenAddServiceMsg_NoRepos_Pending(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)

	updated, cmd := m.Update(panels.OpenAddServiceMsg{TaskID: "IN-1", ExistingServices: []string{"api"}})
	m = updated.(Model)
	if m.modal != nil {
		t.Fatal("modal must be nil when repos are not loaded yet")
	}
	if m.addDialogPending == nil {
		t.Fatal("addDialogPending must be set")
	}
	if cmd == nil {
		t.Fatal("must return command to load repos")
	}
	if !strings.Contains(m.outputPanel.View(), "Loading repository cache for add service dialog...") {
		t.Fatalf("output should mention deferred add service repo load, got %q", m.outputPanel.View())
	}
}

func TestUpdate_ReposLoadedMsg_OpensPendingAddDialog(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.addDialogPending = &panels.OpenAddServiceMsg{TaskID: "IN-1"}

	updated, _ := m.Update(ReposLoadedMsg{Repos: []domain.Repo{{Name: "api", Path: "/repo/api"}}})
	m = updated.(Model)
	if m.addDialogPending != nil {
		t.Fatal("addDialogPending should be cleared")
	}
	if _, ok := m.modal.(*modal.AddDialog); !ok {
		t.Fatalf("expected AddDialog, got %T", m.modal)
	}
}

func TestUpdate_ReposLoadedMsg_ErrorKeepsExistingRepos(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)
	m.repos = []domain.Repo{{Name: "old", Path: "/repo/old"}}

	updated, _ := m.Update(ReposLoadedMsg{Err: &mockError{msg: "scan failed"}})
	m = updated.(Model)
	if len(m.repos) != 1 || m.repos[0].Name != "old" {
		t.Fatalf("repos should be preserved on error, got %#v", m.repos)
	}
	if !strings.Contains(m.outputPanel.View(), "scan failed") {
		t.Fatalf("output should contain refresh error, got %q", m.outputPanel.View())
	}
}

func TestUpdate_RefreshKeyStartsTaskAndRepoRefresh(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("refresh key must return command")
	}
	if !strings.Contains(m.outputPanel.View(), "Refreshing tasks and repository cache...") {
		t.Fatalf("output should mention tasks and repository cache refresh, got %q", m.outputPanel.View())
	}
}

func TestUpdate_OpenConfigModalMsg_OpensConfigModal(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	updated, _ := m.Update(panels.OpenConfigModalMsg{})
	m = updated.(Model)
	if m.modal == nil {
		t.Fatal("OpenConfigModalMsg must open a modal")
	}

	cm, ok := m.modal.(*modal.ConfigModal)
	if !ok {
		t.Fatalf("expected *modal.ConfigModal, got %T", m.modal)
	}

	view := cm.View()
	if !strings.Contains(view, m.cfg.Editor) {
		t.Errorf("config modal view should contain editor %q", m.cfg.Editor)
	}
	if !strings.Contains(view, m.cfg.BranchPrefix) {
		t.Errorf("config modal view should contain branch prefix %q", m.cfg.BranchPrefix)
	}
}

func TestUpdate_FocusServicesMsg_SwitchesFocus(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	updated, _ := m.Update(panels.FocusServicesMsg{TaskID: "IN-0001"})
	m = updated.(Model)
	if m.focus != FocusServices {
		t.Errorf("FocusServicesMsg: expected FocusServices, got %v", m.focus)
	}
}

func TestUpdate_FocusTasksMsg_SwitchesFocus(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	updated, _ := m.Update(panels.FocusServicesMsg{TaskID: "IN-0001"})
	m = updated.(Model)

	updated, _ = m.Update(panels.FocusTasksMsg{})
	m = updated.(Model)
	if m.focus != FocusTasks {
		t.Errorf("FocusTasksMsg: expected FocusTasks, got %v", m.focus)
	}
}

func TestUpdate_TasksLoadedMsg_UpdatesPanel(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)

	tasks := []domain.Task{{ID: "IN-1111"}, {ID: "IN-2222"}}
	updated, _ := m.Update(TasksLoadedMsg{Tasks: tasks})
	m = updated.(Model)

	view := m.tasksPanel.View()
	if !strings.Contains(view, "IN-1111") {
		t.Errorf("tasks panel view should contain IN-1111 after TasksLoadedMsg")
	}
	if !strings.Contains(view, "IN-2222") {
		t.Errorf("tasks panel view should contain IN-2222 after TasksLoadedMsg")
	}
}

func TestUpdate_CommandDoneMsg_WithError_AppendsError(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)
	m.opRunning = true

	updated, _ := m.Update(CommandDoneMsg{Err: &mockError{"something went wrong"}})
	m = updated.(Model)

	if m.opRunning {
		t.Error("opRunning must be false after CommandDoneMsg")
	}

	view := m.outputPanel.View()
	if !strings.Contains(view, "something went wrong") {
		t.Errorf("output panel should contain error message after CommandDoneMsg with error")
	}
}

func TestUpdate_CommandDoneMsg_NoError_AppendsDone(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)
	m.opRunning = true

	updated, _ := m.Update(CommandDoneMsg{Err: nil})
	m = updated.(Model)

	if m.opRunning {
		t.Error("opRunning must be false after CommandDoneMsg")
	}

	view := m.outputPanel.View()
	if !strings.Contains(view, "Done.") {
		t.Errorf("output panel should contain 'Done.' after successful CommandDoneMsg")
	}
}

func TestUpdate_OutputLineMsg_AppendLine(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)

	updated, _ := m.Update(OutputLineMsg{Line: "build succeeded"})
	m = updated.(Model)

	view := m.outputPanel.View()
	if !strings.Contains(view, "build succeeded") {
		t.Errorf("output panel should contain the line from OutputLineMsg")
	}
}

func TestUpdate_SubmitInitMsg_StartsOperation(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	updated, cmd := m.Update(modal.SubmitInitMsg{
		TaskID:       "IN-3333",
		Services:     []string{"svc-a"},
		BranchPrefix: "feature/",
		BaseBranch:   "main",
	})
	m = updated.(Model)

	if !m.opRunning {
		t.Error("opRunning must be true after SubmitInitMsg")
	}
	if m.modal != nil {
		t.Error("modal must be closed after SubmitInitMsg")
	}
	if cmd == nil {
		t.Error("SubmitInitMsg must return a non-nil cmd")
	}
}

func TestUpdate_SubmitRemoveMsg_StartsOperation(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	updated, cmd := m.Update(modal.SubmitRemoveTaskMsg{
		TaskID: "IN-4444",
		Force:  false,
	})
	m = updated.(Model)

	if !m.opRunning {
		t.Error("opRunning must be true after SubmitRemoveMsg")
	}
	if m.modal != nil {
		t.Error("modal must be closed after SubmitRemoveMsg")
	}
	if cmd == nil {
		t.Error("SubmitRemoveMsg must return a non-nil cmd")
	}
}

func TestFocusPanel_String(t *testing.T) {
	tests := []struct {
		panel FocusPanel
		want  string
	}{
		{FocusTasks, "tasks"},
		{FocusServices, "services"},
		{FocusOutput, "output"},
		{FocusPanel(99), "unknown"},
	}
	for _, tc := range tests {
		if got := tc.panel.String(); got != tc.want {
			t.Errorf("FocusPanel(%d).String(): expected %q, got %q", tc.panel, tc.want, got)
		}
	}
}

func TestFocusPanel_NextPrev(t *testing.T) {

	if got := FocusTasks.Next(); got != FocusOutput {
		t.Errorf("FocusTasks.Next(): expected FocusOutput, got %v", got)
	}
	if got := FocusOutput.Next(); got != FocusTasks {
		t.Errorf("FocusOutput.Next(): expected FocusTasks, got %v", got)
	}

	if got := FocusServices.Next(); got != FocusTasks {
		t.Errorf("FocusServices.Next(): expected FocusTasks (safe default), got %v", got)
	}

	if got := FocusTasks.Prev(); got != FocusOutput {
		t.Errorf("FocusTasks.Prev(): expected FocusOutput, got %v", got)
	}
	if got := FocusOutput.Prev(); got != FocusTasks {
		t.Errorf("FocusOutput.Prev(): expected FocusTasks, got %v", got)
	}

	if got := FocusServices.Prev(); got != FocusTasks {
		t.Errorf("FocusServices.Prev(): expected FocusTasks (safe default), got %v", got)
	}
}

type mockError struct{ msg string }

func (e *mockError) Error() string { return e.msg }

func TestUpdate_PushServiceMsg_StartsOperation(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)

	updated, cmd := m.Update(panels.PushServiceMsg{TaskID: "IN-001", ServiceName: "svc-a"})
	m = updated.(Model)

	if !m.opRunning {
		t.Error("opRunning must be true after PushServiceMsg")
	}
	if cmd == nil {
		t.Fatal("PushServiceMsg must return a non-nil cmd")
	}

	view := m.outputPanel.View()
	if !strings.Contains(view, "Pushing service svc-a for task IN-001...") {
		t.Errorf("output panel should contain push message, got:\n%s", view)
	}
}

func TestUpdate_PushTaskMsg_StartsOperationWithTaskID(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)

	updated, cmd := m.Update(panels.PushTaskMsg{TaskID: "IN-777"})
	m = updated.(Model)

	if !m.opRunning {
		t.Error("opRunning must be true after PushTaskMsg")
	}
	if cmd == nil {
		t.Fatal("PushTaskMsg must return a non-nil cmd")
	}
	if !strings.Contains(m.outputPanel.View(), "Pushing task IN-777...") {
		t.Errorf("output panel should contain task push message, got:\n%s", m.outputPanel.View())
	}
}

func TestUpdate_StashServiceMsg_Stash_StartsOperation(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)

	updated, cmd := m.Update(panels.StashServiceMsg{
		TaskID:      "IN-001",
		ServiceName: "svc-a",
		Pop:         false,
	})
	m = updated.(Model)

	if !m.opRunning {
		t.Error("opRunning must be true after StashServiceMsg (stash)")
	}
	if cmd == nil {
		t.Fatal("StashServiceMsg must return a non-nil cmd")
	}

	view := m.outputPanel.View()
	if !strings.Contains(view, "Stashing service svc-a for task IN-001...") {
		t.Errorf("output panel should contain stash message, got:\n%s", view)
	}
}

func TestUpdate_StashServiceMsg_Unstash_StartsOperation(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)

	updated, cmd := m.Update(panels.StashServiceMsg{
		TaskID:      "IN-001",
		ServiceName: "svc-a",
		Pop:         true,
	})
	m = updated.(Model)

	if !m.opRunning {
		t.Error("opRunning must be true after StashServiceMsg (unstash)")
	}
	if cmd == nil {
		t.Fatal("StashServiceMsg must return a non-nil cmd")
	}

	view := m.outputPanel.View()
	if !strings.Contains(view, "Unstashing service svc-a for task IN-001...") {
		t.Errorf("output panel should contain unstash message, got:\n%s", view)
	}
}

func TestUpdate_SubmitSyncStrategyMsg_StartsOperationWithStrategy(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)

	updated, cmd := m.Update(modal.SubmitSyncStrategyMsg{TaskID: "IN-010", Strategy: task.SyncStrategyRebase})
	m = updated.(Model)

	if !m.opRunning {
		t.Error("opRunning must be true after SubmitSyncStrategyMsg")
	}
	if cmd == nil {
		t.Fatal("SubmitSyncStrategyMsg must return a non-nil cmd")
	}
	if !strings.Contains(m.outputPanel.View(), "Syncing task IN-010 with rebase strategy...") {
		t.Errorf("output panel should contain sync strategy message, got:\n%s", m.outputPanel.View())
	}
}

func TestUpdate_SubmitSyncStrategyMsg_NoopClosesWithoutStartingOperation(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)
	m.modal = modal.NewSyncStrategyDialog("IN-010")

	updated, cmd := m.Update(modal.SubmitSyncStrategyMsg{TaskID: "IN-010", Strategy: task.SyncStrategyNoop})
	m = updated.(Model)

	if m.opRunning {
		t.Error("opRunning must be false after noop sync strategy")
	}
	if cmd != nil {
		t.Fatal("noop sync strategy must not return a command")
	}
	if m.modal != nil {
		t.Fatal("noop sync strategy must close the modal")
	}
	if !strings.Contains(m.outputPanel.View(), "Sync cancelled for task IN-010.") {
		t.Errorf("output panel should contain sync cancellation message, got:\n%s", m.outputPanel.View())
	}
}

func TestUpdate_RiderTaskMsg_StartsOperationWithSolutionName(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)

	updated, cmd := m.Update(panels.RiderTaskMsg{TaskID: "IN-222", TaskDir: "/tmp/IN-222"})
	m = updated.(Model)

	if !m.opRunning {
		t.Error("opRunning must be true after RiderTaskMsg")
	}
	if cmd == nil {
		t.Fatal("RiderTaskMsg must return a non-nil cmd")
	}
	view := m.outputPanel.View()
	if !strings.Contains(view, "Opening IN-222.sln in Rider from /tmp/IN-222...") {
		t.Errorf("output panel should contain Rider task solution message, got:\n%s", view)
	}
}
