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

// ─── Mock task.Manager ────────────────────────────────────────────────────────

// mockManager is a no-op implementation of task.Manager for unit-testing the
// root TUI model without any real filesystem or git operations.
type mockManager struct {
	listTasksResult    []domain.Task
	listTasksErr       error
	listServicesResult []domain.Service
	listServicesErr    error
	listReposResult    []domain.Repo
	listReposErr       error
}

var _ task.Manager = (*mockManager)(nil)

func (m *mockManager) Init(_ context.Context, _ task.InitParams) error     { return nil }
func (m *mockManager) Add(_ context.Context, _ task.AddParams) error       { return nil }
func (m *mockManager) Remove(_ context.Context, _ string, _, _ bool) error { return nil }
func (m *mockManager) GenerateSln(_ context.Context, _ string) error       { return nil }

func (m *mockManager) List(_ context.Context) ([]domain.Task, error) {
	return m.listTasksResult, m.listTasksErr
}

func (m *mockManager) ListServices(_ context.Context, _ string) ([]domain.Service, error) {
	return m.listServicesResult, m.listServicesErr
}

func (m *mockManager) DiscoverRepos(_ context.Context) ([]domain.Repo, error) {
	return m.listReposResult, m.listReposErr
}

func (m *mockManager) SyncTask(_ context.Context, _ string, lineCh chan<- string) error {
	close(lineCh)
	return nil
}

func (m *mockManager) PushTask(_ context.Context, _ string, lineCh chan<- string) error {
	close(lineCh)
	return nil
}

func (m *mockManager) PushService(_ context.Context, _, _ string, _ chan<- string) error { return nil }

func (m *mockManager) CloneTask(_ context.Context, _, _ string) error { return nil }

func (m *mockManager) StashService(_ context.Context, _, _ string, _ bool) error { return nil }

func (m *mockManager) RemoveService(_ context.Context, _, _ string, _ bool) error { return nil }

// ─── helpers ──────────────────────────────────────────────────────────────────

// newTestConfig returns a minimal *config.Config with Effective() applied,
// suitable for unit tests that do not touch the filesystem.
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

// newTestModel is a convenience constructor that wires a mockManager and a
// default config into a fresh Model.  It calls t.Fatal on construction failure.
func newTestModel(t *testing.T, mgr task.Manager) Model {
	t.Helper()
	m, err := New(newTestConfig(), mgr, slog.Default())
	if err != nil {
		t.Fatalf("tui.New: unexpected error: %v", err)
	}
	return m
}

// sendWindowSize delivers a tea.WindowSizeMsg to the model and returns the
// updated model, marking it as ready.
func sendWindowSize(m Model, w, h int) Model {
	updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return updated.(Model)
}

// ─── 1. Construction smoke test ───────────────────────────────────────────────

// TestModelInit verifies that tui.New succeeds with valid dependencies and that
// the initial focus is FocusTasks.
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

// ─── 2. New rejects nil arguments ────────────────────────────────────────────

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

// ─── 3. View before WindowSizeMsg returns "Loading..." ───────────────────────

func TestView_BeforeWindowSize_ReturnsLoading(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	view := m.View()
	if view != "Loading..." {
		t.Errorf("View() before WindowSizeMsg: expected %q, got %q", "Loading...", view)
	}
}

// ─── 4. WindowSizeMsg marks model as ready ───────────────────────────────────

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

// ─── 5. View after WindowSizeMsg does not return "Loading..." ────────────────

func TestView_AfterWindowSize_NotLoading(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)

	view := m.View()
	if view == "Loading..." {
		t.Error("View() after WindowSizeMsg must not return 'Loading...'")
	}
}

// ─── 6. Init returns non-nil Cmd batch ───────────────────────────────────────

func TestInit_ReturnsCmd(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() must return a non-nil tea.Cmd")
	}
}

// ─── 7. Quit key returns tea.Quit ─────────────────────────────────────────────

func TestUpdate_QuitKey_ReturnsQuit(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("Ctrl+C must return a cmd")
	}
	// Execute the cmd to verify it is tea.Quit.
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("Ctrl+C cmd must produce tea.QuitMsg, got %T", msg)
	}
}

// ─── 8. Tab cycles focus forward ─────────────────────────────────────────────

func TestUpdate_Tab_CyclesFocusForward(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	// Initial state: FocusTasks.
	if m.focus != FocusTasks {
		t.Fatalf("expected FocusTasks initially, got %v", m.focus)
	}

	// Tab from FocusTasks → FocusOutput (FocusServices is excluded from cycle).
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.focus != FocusOutput {
		t.Errorf("after Tab: expected FocusOutput, got %v", m.focus)
	}

	// Tab from FocusOutput → wraps back to FocusTasks.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.focus != FocusTasks {
		t.Errorf("after Tab×2 (wrap): expected FocusTasks, got %v", m.focus)
	}
}

// ─── 9. ShiftTab cycles focus backward ───────────────────────────────────────

func TestUpdate_ShiftTab_CyclesFocusBackward(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	// Shift+Tab from FocusTasks → FocusOutput (wrap).
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = updated.(Model)
	if m.focus != FocusOutput {
		t.Errorf("Shift+Tab from FocusTasks: expected FocusOutput, got %v", m.focus)
	}
}

// ─── 10. Help key opens HelpOverlay modal ────────────────────────────────────

func TestUpdate_HelpKey_OpensHelpModal(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	// '?' opens help.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)
	if m.modal == nil {
		t.Fatal("'?' must open a modal")
	}
	if _, ok := m.modal.(*modal.HelpOverlay); !ok {
		t.Errorf("expected *modal.HelpOverlay, got %T", m.modal)
	}
}

// ─── 11. CloseModalMsg nils the modal ────────────────────────────────────────

func TestUpdate_CloseModalMsg_NilsModal(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	// Open a modal first.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)
	if m.modal == nil {
		t.Fatal("precondition failed: modal should be open")
	}

	// Close it.
	updated, _ = m.Update(modal.CloseModalMsg{})
	m = updated.(Model)
	if m.modal != nil {
		t.Error("CloseModalMsg must set modal to nil")
	}
}

// ─── 12. OpenInitDialogMsg opens InitDialog ──────────────────────────────────

func TestUpdate_OpenInitDialogMsg_OpensInitDialog(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	// Pre-load repos so the dialog opens immediately (no pending state).
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

	// No repos loaded → dialog is deferred until ReposLoadedMsg.
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

// ─── 13. FocusServicesMsg switches focus to services panel ───────────────────

func TestUpdate_FocusServicesMsg_SwitchesFocus(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	updated, _ := m.Update(panels.FocusServicesMsg{TaskID: "IN-0001"})
	m = updated.(Model)
	if m.focus != FocusServices {
		t.Errorf("FocusServicesMsg: expected FocusServices, got %v", m.focus)
	}
}

// ─── 14. FocusTasksMsg switches focus to tasks panel ─────────────────────────

func TestUpdate_FocusTasksMsg_SwitchesFocus(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	// Move to services first.
	updated, _ := m.Update(panels.FocusServicesMsg{TaskID: "IN-0001"})
	m = updated.(Model)

	// Then return to tasks.
	updated, _ = m.Update(panels.FocusTasksMsg{})
	m = updated.(Model)
	if m.focus != FocusTasks {
		t.Errorf("FocusTasksMsg: expected FocusTasks, got %v", m.focus)
	}
}

// ─── 15. TasksLoadedMsg updates the tasks panel ──────────────────────────────

func TestUpdate_TasksLoadedMsg_UpdatesPanel(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)

	tasks := []domain.Task{{ID: "IN-1111"}, {ID: "IN-2222"}}
	updated, _ := m.Update(TasksLoadedMsg{Tasks: tasks})
	m = updated.(Model)

	// After receiving TasksLoadedMsg the tasks panel should render the task IDs.
	view := m.tasksPanel.View()
	if !strings.Contains(view, "IN-1111") {
		t.Errorf("tasks panel view should contain IN-1111 after TasksLoadedMsg")
	}
	if !strings.Contains(view, "IN-2222") {
		t.Errorf("tasks panel view should contain IN-2222 after TasksLoadedMsg")
	}
}

// ─── 16. CommandDoneMsg with error appends error to output panel ──────────────

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

// ─── 17. CommandDoneMsg without error appends "Done." ─────────────────────────

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

// ─── 18. OutputLineMsg appends line to output panel ───────────────────────────

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

// ─── 19. SubmitInitMsg starts init operation ─────────────────────────────────

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

// ─── 20. SubmitRemoveMsg starts remove operation ──────────────────────────────

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

// ─── 21. FocusPanel.String() returns correct strings ─────────────────────────

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

// ─── 22. FocusPanel.Next() and .Prev() cycle correctly ───────────────────────

func TestFocusPanel_NextPrev(t *testing.T) {
	// Forward cycle: Tasks ↔ Output (FocusServices excluded from Tab cycle).
	if got := FocusTasks.Next(); got != FocusOutput {
		t.Errorf("FocusTasks.Next(): expected FocusOutput, got %v", got)
	}
	if got := FocusOutput.Next(); got != FocusTasks {
		t.Errorf("FocusOutput.Next(): expected FocusTasks, got %v", got)
	}
	// FocusServices is a safe default — Next() returns FocusTasks.
	if got := FocusServices.Next(); got != FocusTasks {
		t.Errorf("FocusServices.Next(): expected FocusTasks (safe default), got %v", got)
	}

	// Backward cycle: Tasks ↔ Output (symmetric with Next).
	if got := FocusTasks.Prev(); got != FocusOutput {
		t.Errorf("FocusTasks.Prev(): expected FocusOutput, got %v", got)
	}
	if got := FocusOutput.Prev(); got != FocusTasks {
		t.Errorf("FocusOutput.Prev(): expected FocusTasks, got %v", got)
	}
	// FocusServices is a safe default — Prev() returns FocusTasks.
	if got := FocusServices.Prev(); got != FocusTasks {
		t.Errorf("FocusServices.Prev(): expected FocusTasks (safe default), got %v", got)
	}
}

// ─── internal test helpers ────────────────────────────────────────────────────

type mockError struct{ msg string }

func (e *mockError) Error() string { return e.msg }

// ─── 25. SyncTaskMsg starts sync operation ────────────────────────────────────

func TestUpdate_SyncTaskMsg_StartsOperation(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)

	updated, cmd := m.Update(panels.SyncTaskMsg{TaskID: "IN-001"})
	m = updated.(Model)

	if !m.opRunning {
		t.Error("opRunning must be true after SyncTaskMsg")
	}
	if cmd == nil {
		t.Fatal("SyncTaskMsg must return a non-nil cmd")
	}

	view := m.outputPanel.View()
	if !strings.Contains(view, "Syncing task IN-001") {
		t.Errorf("output panel should contain sync message, got:\n%s", view)
	}
}

// ─── 26. PushServiceMsg starts push operation ────────────────────────────────

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
	if !strings.Contains(view, "Pushing svc-a") {
		t.Errorf("output panel should contain push message, got:\n%s", view)
	}
}

// ─── 27. StashServiceMsg starts stash operation ──────────────────────────────

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
	if !strings.Contains(view, "Stashing svc-a") {
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
	if !strings.Contains(view, "Unstashing svc-a") {
		t.Errorf("output panel should contain unstash message, got:\n%s", view)
	}
}
