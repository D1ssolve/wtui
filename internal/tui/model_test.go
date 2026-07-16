package tui

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	"github.com/D1ssolve/wtui/internal/tui/panels"
)

type mockManager struct {
	mu sync.Mutex

	listTasksResult    []domain.Task
	listTasksErr       error
	listTasksCalls     int
	listServicesResult []domain.Service
	listServicesErr    error
	listServicesTaskID string
	listServicesCalls  int
	reposResult        []domain.Repo
	reposErr           error
	repoRefreshArgs    []bool

	validateTaskID string
	validateResult domain.TaskValidation
	validateErr    error

	syncTaskCalls    int
	syncTaskTaskID   string
	syncTaskStrategy task.SyncStrategy

	listReleasesResult []domain.Release
	listReleasesErr    error
	listReleasesCalls  int

	createReleaseParams task.CreateReleaseParams
	createReleaseResult domain.Release
	createReleaseErr    error
	createReleaseCalls  int
	createReleaseDone   chan struct{}
	finishReleaseResult domain.Release
	finishReleaseErr    error
	finishReleaseID     string
	finishReleaseCalls  int
	finishReleaseDone   chan struct{}

	pushTaskCalls    int
	pushTaskID       string
	pushServiceCalls int
	pushServiceTask  string
	pushServiceName  string

	isProtectedBranchResult map[string]bool
}

var _ task.Manager = (*mockManager)(nil)

func (m *mockManager) Init(_ context.Context, _ task.InitParams) (task.PartialFailureResult, error) {
	return task.PartialFailureResult{}, nil
}
func (m *mockManager) Add(_ context.Context, _ task.AddParams) (task.PartialFailureResult, error) {
	return task.PartialFailureResult{}, nil
}
func (m *mockManager) Remove(_ context.Context, _ string, _, _ bool) error { return nil }

func (m *mockManager) List(_ context.Context) ([]domain.Task, error) {
	m.listTasksCalls++
	return m.listTasksResult, m.listTasksErr
}

func (m *mockManager) ListServices(_ context.Context, taskID string) ([]domain.Service, error) {
	m.listServicesCalls++
	m.listServicesTaskID = taskID
	return m.listServicesResult, m.listServicesErr
}

func (m *mockManager) Repos(_ context.Context, refresh bool) ([]domain.Repo, error) {
	m.repoRefreshArgs = append(m.repoRefreshArgs, refresh)
	return m.reposResult, m.reposErr
}

func (m *mockManager) SyncTask(_ context.Context, taskID string, strategy task.SyncStrategy, lineCh chan<- string) error {
	m.syncTaskCalls++
	m.syncTaskTaskID = taskID
	m.syncTaskStrategy = strategy
	close(lineCh)
	return nil
}

func (m *mockManager) SyncService(_ context.Context, _, _ string, _ task.SyncStrategy, lineCh chan<- string) error {
	close(lineCh)
	return nil
}

func (m *mockManager) PushTask(_ context.Context, taskID string, lineCh chan<- string) error {
	m.pushTaskCalls++
	m.pushTaskID = taskID
	close(lineCh)
	return nil
}

func (m *mockManager) PushService(_ context.Context, taskID, serviceName string, _ chan<- string) error {
	m.pushServiceCalls++
	m.pushServiceTask = taskID
	m.pushServiceName = serviceName
	return nil
}

func (m *mockManager) StashService(_ context.Context, _, _ string, _, _ bool) error { return nil }

func (m *mockManager) RemoveService(_ context.Context, _, _ string, _ bool) error { return nil }

func (m *mockManager) ValidateTask(_ context.Context, taskID string) (domain.TaskValidation, error) {
	m.validateTaskID = taskID
	return m.validateResult, m.validateErr
}

func (m *mockManager) PlanCloseTask(_ context.Context, _ string) (task.ClosePlan, error) {
	return task.ClosePlan{}, nil
}

func (m *mockManager) CloseTask(_ context.Context, _ task.CloseTaskParams) (task.CloseTaskResult, error) {
	return task.CloseTaskResult{}, nil
}

func (m *mockManager) ScanPrunableTasks(_ context.Context) ([]domain.PruneCandidate, error) {
	return nil, nil
}

func (m *mockManager) ListTags(_ context.Context, _ string) ([]domain.TagInfo, error) {
	return nil, nil
}

func (m *mockManager) ForgeCreateMR(_ context.Context, _, _ string, _ forge.CreateMRParams) (forge.MRInfo, error) {
	return forge.MRInfo{}, nil
}

func (m *mockManager) ForgePipelineStatus(_ context.Context, _, _ string, _ string) ([]forge.PipelineStatus, error) {
	return nil, nil
}

func (m *mockManager) ForgeListIssues(_ context.Context, _, _ string, _ forge.ListIssuesParams) ([]forge.IssueInfo, error) {
	return nil, nil
}

func (m *mockManager) ListReleases(_ context.Context) ([]domain.Release, error) {
	m.listReleasesCalls++
	return m.listReleasesResult, m.listReleasesErr
}

func (m *mockManager) GetRelease(_ context.Context, _ string) (domain.Release, error) {
	return domain.Release{}, nil
}

func (m *mockManager) CreateRelease(_ context.Context, params task.CreateReleaseParams) (domain.Release, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createReleaseCalls++
	m.createReleaseParams = params
	if m.createReleaseDone != nil {
		close(m.createReleaseDone)
	}
	return m.createReleaseResult, m.createReleaseErr
}

func (m *mockManager) FinishRelease(_ context.Context, params task.FinishReleaseParams) (domain.Release, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.finishReleaseCalls++
	m.finishReleaseID = params.ReleaseID
	if m.finishReleaseDone != nil {
		close(m.finishReleaseDone)
	}
	return m.finishReleaseResult, m.finishReleaseErr
}

func (m *mockManager) IsProtectedBranch(_ context.Context, branch string) bool {
	if m.isProtectedBranchResult != nil {
		return m.isProtectedBranchResult[branch]
	}
	return false
}

func (m *mockManager) BuildReleasePreview(_ context.Context, _ map[string]string) (task.ReleasePreview, error) {
	return task.ReleasePreview{}, nil
}

func (m *mockManager) RetryRelease(_ context.Context, _ string) (domain.Release, error) {
	return domain.Release{}, nil
}

func (m *mockManager) RejectRelease(_ context.Context, _ string) (domain.Release, error) {
	return domain.Release{}, nil
}

func (m *mockManager) RemoveRelease(_ context.Context, _ string) error { return nil }

func newTestConfig() *config.Config {
	cfg := &config.Config{
		RootDir:          "/tmp/wtui-test",
		TasksRoot:        "/tmp/wtui-test/.tasks",
		BranchPrefix:     "feature/",
		Editor:           "code",
		DiscoveryDepth:   4,
		OutputPanelLines: 12,
		LogLevel:         "INFO",
	}
	effective, err := cfg.Effective()
	if err != nil {
		panic(err)
	}
	return effective
}

func newTestModel(t *testing.T, mgr task.Manager) Model {
	t.Helper()
	m, err := New(newTestConfig(), mgr, slog.Default())
	if err != nil {
		t.Fatalf("tui.New: unexpected error: %v", err)
	}
	return m
}

func newTestModelWithOptions(t *testing.T, mgr task.Manager, opts Options) Model {
	t.Helper()
	m, err := NewWithOptions(newTestConfig(), mgr, slog.Default(), opts)
	if err != nil {
		t.Fatalf("tui.NewWithOptions: unexpected error: %v", err)
	}
	return m
}

func sendWindowSize(m Model, w, h int) Model {
	updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return updated.(Model)
}

func newLazygitUpdateTestModel(t *testing.T, mgr task.Manager) Model {
	t.Helper()
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 1000, 40)
	m.outputPanel.SetSize(1000, 12)
	return m
}

func stripANSIForModel(s string) string {
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

func TestNewWithOptions_SetsLazygitAvailable(t *testing.T) {
	m, err := NewWithOptions(newTestConfig(), &mockManager{}, slog.Default(), Options{LazygitAvailable: true})
	if err != nil {
		t.Fatalf("NewWithOptions: unexpected error: %v", err)
	}

	if !m.lazygitAvailable {
		t.Fatal("lazygitAvailable = false, want true")
	}
}

func TestNew_DefaultsLazygitUnavailable(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	if m.lazygitAvailable {
		t.Fatal("lazygitAvailable = true, want false")
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

	tasksWidth, _, releasesWidth := threePanelWidths(m.width)
	if tasksWidth != releasesWidth {
		t.Fatalf("tasks and releases width should match, got tasks=%d releases=%d", tasksWidth, releasesWidth)
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

func TestView_AfterWindowSize_IncludesReleasesPanel(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 140, 40)

	view := stripANSIForModel(m.View())
	if !strings.Contains(view, "[3] Releases") {
		t.Fatalf("view should include releases panel title, got %q", view)
	}
}

func TestThreePanelWidths_DistributesTasksAndReleasesEqually(t *testing.T) {
	cases := []struct {
		total           int
		wantTasks       int
		wantServices    int
		wantReleases    int
	}{
		{120, 44, 32, 44},
		{80, 29, 22, 29},
		{60, 25, 10, 25},
	}

	for _, tc := range cases {
		tasks, services, releases := threePanelWidths(tc.total)
		if tasks != tc.wantTasks || services != tc.wantServices || releases != tc.wantReleases {
			t.Errorf("threePanelWidths(%d) = tasks=%d services=%d releases=%d, want tasks=%d services=%d releases=%d",
				tc.total, tasks, services, releases, tc.wantTasks, tc.wantServices, tc.wantReleases)
		}
		if tasks+services+releases != tc.total {
			t.Errorf("threePanelWidths(%d) sums to %d, want %d", tc.total, tasks+services+releases, tc.total)
		}
		if tasks != releases {
			t.Errorf("threePanelWidths(%d): tasks (%d) != releases (%d)", tc.total, tasks, releases)
		}
	}
}

func TestRecalculateDimensions_UsesFullTerminalHeight(t *testing.T) {
	m := newTestModel(t, &mockManager{})

	for _, height := range []int{24, 40, 60} {
		m = sendWindowSize(m, 120, height)
		view := m.View()
		got := lipgloss.Height(view)
		if got != height {
			t.Errorf("View() height for terminal %d = %d, want %d", height, got, height)
		}
	}
}

func TestInit_ReturnsCmd(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)

	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() must return a non-nil tea.Cmd")
	}
	runBatchCommands(cmd())
	if mgr.listReleasesCalls != 1 {
		t.Fatalf("ListReleases calls = %d, want 1", mgr.listReleasesCalls)
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
	if m.focus != FocusServices {
		t.Errorf("after Tab: expected FocusServices, got %v", m.focus)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.focus != FocusReleases {
		t.Errorf("after Tab×2: expected FocusReleases, got %v", m.focus)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.focus != FocusOutput {
		t.Errorf("after Tab×3: expected FocusOutput, got %v", m.focus)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.focus != FocusTasks {
		t.Errorf("after Tab×4 (wrap): expected FocusTasks, got %v", m.focus)
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

func TestUpdate_HelpKey_AfterResize_AllowsScrollToBottom(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 80, 10)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)
	if m.modal == nil {
		t.Fatal("'?' must open a modal")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = updated.(Model)

	view := stripANSIForModel(m.modal.View())
	if !strings.Contains(view, "[Esc] or [?] to close") {
		t.Fatalf("scrolled help view must show bottom close hint, got: %q", view)
	}
	if strings.Contains(view, "Keyboard Shortcuts") {
		t.Fatalf("scrolled help view should not contain top title, got: %q", view)
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

func TestUpdate_OpenCloneDialogMsg_LoadsSourceServices(t *testing.T) {
	mgr := &mockManager{
		listServicesResult: []domain.Service{{Name: "api", Branch: "feature/SOURCE-1"}},
	}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)

	updated, cmd := m.Update(panels.OpenCloneDialogMsg{TaskID: "SOURCE-1"})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("OpenCloneDialogMsg must return cmd to load source services")
	}
	msg := cmd()
	loaded, ok := msg.(CloneSourceServicesLoadedMsg)
	if !ok {
		t.Fatalf("expected CloneSourceServicesLoadedMsg, got %T", msg)
	}
	if loaded.SourceTaskID != "SOURCE-1" {
		t.Errorf("SourceTaskID = %q, want SOURCE-1", loaded.SourceTaskID)
	}
	if mgr.listServicesTaskID != "SOURCE-1" {
		t.Errorf("ListServices taskID = %q, want SOURCE-1", mgr.listServicesTaskID)
	}
	if !strings.Contains(m.outputPanel.View(), "Loading source task SOURCE-1 services for clone") {
		t.Fatalf("output should mention loading clone source, got %q", m.outputPanel.View())
	}
}

func TestUpdate_CloneSourceServicesLoadedMsg_OpensCloneInitDialog(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)

	updated, _ := m.Update(CloneSourceServicesLoadedMsg{
		SourceTaskID: "SOURCE-1",
		Services:     []domain.Service{{Name: "api", Branch: "feature/SOURCE-1"}},
	})
	m = updated.(Model)

	d, ok := m.modal.(*modal.InitDialog)
	if !ok {
		t.Fatalf("expected clone init dialog, got %T", m.modal)
	}
	view := d.View()
	if !strings.Contains(view, "feature/SOURCE-1") {
		t.Fatalf("clone dialog should show source branch, got %q", view)
	}
}

func TestUpdate_CloneSourceServicesLoadedMsg_MismatchedBranchesShowsError(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)

	updated, _ := m.Update(CloneSourceServicesLoadedMsg{
		SourceTaskID: "SOURCE-1",
		Services: []domain.Service{
			{Name: "api", Branch: "feature/SOURCE-1"},
			{Name: "worker", Branch: "feature/OTHER"},
		},
	})
	m = updated.(Model)

	if m.modal == nil {
		t.Fatal("model should still open dialog so user can choose a valid subset")
	}
	if !strings.Contains(stripANSIForModel(m.modal.View()), "selected source services must share one branch") {
		t.Fatalf("dialog should show mismatch error, got %q", m.modal.View())
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
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("refresh key must return command")
	}
	if !strings.Contains(m.outputPanel.View(), "Refreshing tasks and repository cache...") {
		t.Fatalf("output should mention tasks and repository cache refresh, got %q", m.outputPanel.View())
	}
	runBatchCommands(cmd())
	if mgr.listReleasesCalls != 1 {
		t.Fatalf("refresh should load releases once, got %d", mgr.listReleasesCalls)
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

	updated, _ := m.Update(CommandDoneMsg{Op: "Sync task IN-1", Err: &mockError{"something went wrong"}})
	m = updated.(Model)

	if m.opRunning {
		t.Error("opRunning must be false after CommandDoneMsg")
	}

	view := m.outputPanel.View()
	if !strings.Contains(view, "Sync task IN-1 failed: something went wrong") {
		t.Errorf("output panel should contain contextual error message after CommandDoneMsg with error, got %q", view)
	}
}

func TestUpdate_CommandDoneMsg_NoError_AppendsOperationDone(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)
	m.opRunning = true

	updated, _ := m.Update(CommandDoneMsg{Op: "Push task IN-1", Err: nil})
	m = updated.(Model)

	if m.opRunning {
		t.Error("opRunning must be false after CommandDoneMsg")
	}

	view := m.outputPanel.View()
	if !strings.Contains(view, "Push task IN-1 done.") {
		t.Errorf("output panel should contain contextual done message after successful CommandDoneMsg, got %q", view)
	}
}

func TestUpdate_QKeyInTasksFocus_IsNoOp(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)
	m.tasksPanel.SetTasks([]domain.Task{{ID: "ZA-553", Phase: "feature", ParentID: ""}})

	beforeOutput := m.outputPanel.View()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Q")})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("Q in tasks focus must be no-op and return nil cmd")
	}
	if m.modal != nil {
		t.Fatalf("Q in tasks focus must not open modal, got %T", m.modal)
	}
	if got := m.outputPanel.View(); got != beforeOutput {
		t.Fatalf("Q in tasks focus must not append output, before=%q after=%q", beforeOutput, got)
	}
}

func TestUpdate_RefreshCompletionLogsTasksAndRepos(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)
	m.refreshing = true

	updated, _ := m.Update(TasksLoadedMsg{Tasks: []domain.Task{{ID: "IN-1"}}})
	m = updated.(Model)
	if !strings.Contains(m.outputPanel.View(), "Tasks refreshed.") {
		t.Fatalf("output should contain task refresh completion, got %q", m.outputPanel.View())
	}
	if !m.refreshing {
		t.Fatal("refreshing should remain true until repos load")
	}

	updated, _ = m.Update(ReposLoadedMsg{Repos: []domain.Repo{{Name: "api", Path: "/repo/api"}}})
	m = updated.(Model)
	if !strings.Contains(m.outputPanel.View(), "Repository cache refreshed.") {
		t.Fatalf("output should contain repo refresh completion, got %q", m.outputPanel.View())
	}
	if m.refreshing {
		t.Fatal("refreshing should reset after repos load")
	}
}

func TestUpdate_ReleasesLoadedMsg_UpdatesReleasesPanel(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)
	releases := []domain.Release{{ID: "rel-1", Status: domain.ReleaseStatusDraft, CreatedAt: time.Now().UTC()}}

	updated, _ := m.Update(ReleasesLoadedMsg{Releases: releases})
	m = updated.(Model)

	if selected := m.releasesPanel.SelectedRelease(); selected == nil || selected.ID != "rel-1" {
		t.Fatalf("expected releases panel selected rel-1, got %+v", selected)
	}
}

func TestUpdate_FocusReleasesAndNewRelease_OpensCreateReleaseDialog(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)
	m.tasks = []domain.Task{{ID: "ZA-1", Phase: "feature", Services: []domain.Service{{Name: "api"}}}}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	m = updated.(Model)
	if m.focus != FocusReleases {
		t.Fatalf("focus = %v, want FocusReleases", m.focus)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("N")})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("N should emit open create release dialog command when releases focused")
	}
	openMsg := cmd()
	open, ok := openMsg.(panels.OpenCreateReleaseDialogMsg)
	if !ok {
		t.Fatalf("expected OpenCreateReleaseDialogMsg, got %T", openMsg)
	}

	updated, _ = m.Update(open)
	m = updated.(Model)
	if _, ok := m.modal.(*modal.CreateReleaseDialog); !ok {
		t.Fatalf("expected CreateReleaseDialog modal, got %T", m.modal)
	}
}

func TestUpdate_SubmitCreateRelease_OpensExecuteConfirmModal(t *testing.T) {
	mgr := &mockManager{createReleaseResult: domain.Release{ID: "rel-1", CreatedAt: time.Now().UTC()}}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)

	updated, cmd := m.Update(modal.SubmitCreateReleaseMsg{
		TaskIDs:  []string{"ZA-1"},
		Versions: map[string]string{"api": "1.2.3"},
	})
	m = updated.(Model)

	if m.opRunning {
		t.Fatal("submit create release should not start operation before confirm")
	}
	if cmd != nil {
		t.Fatal("submit create release should not return command before confirm")
	}
	if _, ok := m.modal.(*modal.ReleaseExecuteConfirmDialog); !ok {
		t.Fatalf("expected ReleaseExecuteConfirmDialog, got %T", m.modal)
	}
	if m.pendingReleaseSubmit == nil {
		t.Fatal("pending release submit should be stored")
	}
}
func TestUpdate_ConfirmReleaseExecute_StartsOperation(t *testing.T) {
	mgr := &mockManager{
		createReleaseResult: domain.Release{ID: "rel-1", CreatedAt: time.Now().UTC()},
		createReleaseDone:   make(chan struct{}),
	}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	m.pendingReleaseSubmit = &modal.SubmitCreateReleaseMsg{TaskIDs: []string{"ZA-1"}, Versions: map[string]string{"api": "1.2.3"}}
	m.modal = modal.NewReleaseExecuteConfirmDialog([]string{"ZA-1"}, map[string]string{"api": "1.2.3"}, task.ReleasePreview{})

	updated, cmd := m.Update(modal.ConfirmReleaseExecuteMsg{TaskIDs: []string{"ZA-1"}, Versions: map[string]string{"api": "1.2.3"}})
	m = updated.(Model)

	if !m.opRunning {
		t.Fatal("confirm release execute should set opRunning=true")
	}
	if cmd == nil {
		t.Fatal("confirm release execute should return command")
	}
	if !strings.Contains(m.outputPanel.View(), "Creating release from selected tasks") {
		t.Fatalf("output should include create release start line, got %q", m.outputPanel.View())
	}
	if m.pendingReleaseSubmit != nil {
		t.Fatal("pending release submit should be cleared after confirm")
	}

	select {
	case <-mgr.createReleaseDone:
	case <-time.After(2 * time.Second):
		t.Fatal("CreateRelease was not called")
	}

	mgr.mu.Lock()
	calls := mgr.createReleaseCalls
	params := mgr.createReleaseParams
	mgr.mu.Unlock()

	if calls != 1 {
		t.Fatalf("CreateRelease calls = %d, want 1", calls)
	}
	if !params.StartImmediately {
		t.Fatal("CreateRelease StartImmediately should be true after confirm")
	}
}

func TestUpdate_ConfirmReleaseExecute_WithoutPendingSubmit_DoesNotExecute(t *testing.T) {
	mgr := &mockManager{createReleaseResult: domain.Release{ID: "rel-1", CreatedAt: time.Now().UTC()}}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	m.modal = modal.NewReleaseExecuteConfirmDialog([]string{"ZA-1"}, map[string]string{"api": "1.2.3"}, task.ReleasePreview{})

	updated, cmd := m.Update(modal.ConfirmReleaseExecuteMsg{TaskIDs: []string{"ZA-1"}, Versions: map[string]string{"api": "1.2.3"}})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("confirm release execute without pending submit must not return command")
	}
	if m.opRunning {
		t.Fatal("confirm release execute without pending submit must not set opRunning")
	}
	if mgr.createReleaseCalls != 0 {
		t.Fatalf("create release should not execute, got calls=%d", mgr.createReleaseCalls)
	}
}

func TestUpdate_ConfirmReleaseExecute_WithWrongActiveModal_DoesNotExecute(t *testing.T) {
	mgr := &mockManager{createReleaseResult: domain.Release{ID: "rel-1", CreatedAt: time.Now().UTC()}}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	m.pendingReleaseSubmit = &modal.SubmitCreateReleaseMsg{TaskIDs: []string{"ZA-1"}, Versions: map[string]string{"api": "1.2.3"}}
	m.modal = nil

	updated, cmd := m.Update(modal.ConfirmReleaseExecuteMsg{TaskIDs: []string{"ZA-1"}, Versions: map[string]string{"api": "1.2.3"}})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("confirm release execute with wrong active modal must not return command")
	}
	if m.opRunning {
		t.Fatal("confirm release execute with wrong active modal must not set opRunning")
	}
	if mgr.createReleaseCalls != 0 {
		t.Fatalf("create release should not execute, got calls=%d", mgr.createReleaseCalls)
	}
}

func TestUpdate_ConfirmReleaseExecute_WithMismatchedTaskIDs_DoesNotExecute(t *testing.T) {
	mgr := &mockManager{createReleaseResult: domain.Release{ID: "rel-1", CreatedAt: time.Now().UTC()}}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	m.pendingReleaseSubmit = &modal.SubmitCreateReleaseMsg{TaskIDs: []string{"ZA-1"}, Versions: map[string]string{"api": "1.2.3"}}
	m.modal = modal.NewReleaseExecuteConfirmDialog([]string{"ZA-1"}, map[string]string{"api": "1.2.3"}, task.ReleasePreview{})

	updated, cmd := m.Update(modal.ConfirmReleaseExecuteMsg{TaskIDs: []string{"ZA-2"}, Versions: map[string]string{"api": "1.2.3"}})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("confirm release execute with mismatched task IDs must not return command")
	}
	if m.opRunning {
		t.Fatal("confirm release execute with mismatched task IDs must not set opRunning")
	}
	if mgr.createReleaseCalls != 0 {
		t.Fatalf("create release should not execute, got calls=%d", mgr.createReleaseCalls)
	}
}

func TestUpdate_ConfirmReleaseExecute_WithMismatchedVersions_DoesNotExecute(t *testing.T) {
	mgr := &mockManager{createReleaseResult: domain.Release{ID: "rel-1", CreatedAt: time.Now().UTC()}}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	m.pendingReleaseSubmit = &modal.SubmitCreateReleaseMsg{TaskIDs: []string{"ZA-1"}, Versions: map[string]string{"api": "1.2.3"}}
	m.modal = modal.NewReleaseExecuteConfirmDialog([]string{"ZA-1"}, map[string]string{"api": "1.2.3"}, task.ReleasePreview{})

	updated, cmd := m.Update(modal.ConfirmReleaseExecuteMsg{TaskIDs: []string{"ZA-1"}, Versions: map[string]string{"api": "1.2.4"}})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("confirm release execute with mismatched versions must not return command")
	}
	if m.opRunning {
		t.Fatal("confirm release execute with mismatched versions must not set opRunning")
	}
	if mgr.createReleaseCalls != 0 {
		t.Fatalf("create release should not execute, got calls=%d", mgr.createReleaseCalls)
	}
}

func TestUpdate_CreateReleaseDone_AppendsOutputAndRefreshesReleases(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	m.opRunning = true

	updated, cmd := m.Update(CreateReleaseDoneMsg{Release: domain.Release{ID: "rel-2"}})
	m = updated.(Model)
	if m.opRunning {
		t.Fatal("create release done should clear opRunning")
	}
	if !strings.Contains(m.outputPanel.View(), "Release prepared: rel-2 — run Finish Release after regression testing.") {
		t.Fatalf("output should include create release done line, got %q", m.outputPanel.View())
	}
	if cmd == nil {
		t.Fatal("create release done should trigger releases refresh command")
	}
	_ = cmd()
	if mgr.listReleasesCalls != 1 {
		t.Fatalf("create completion should refresh releases once, got %d", mgr.listReleasesCalls)
	}
}

func TestUpdate_FinishReleaseMsg_OpensFinishConfirmDialog(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)
	rel := domain.Release{
		ID:     "rel-1",
		Status: domain.ReleaseStatusPrepared,
		Services: []domain.ReleaseService{
			{Name: "api", Version: "1.2.3", Tag: "v1.2.3"},
		},
	}
	m.releasesPanel.SetReleases([]domain.Release{rel})

	updated, _ := m.Update(panels.FinishReleaseMsg{ReleaseID: "rel-1"})
	m = updated.(Model)

	if m.pendingFinishReleaseID == nil || *m.pendingFinishReleaseID != "rel-1" {
		t.Fatal("pendingFinishReleaseID should be set to release ID")
	}
	if _, ok := m.modal.(*modal.ReleaseFinishConfirmDialog); !ok {
		t.Fatalf("expected ReleaseFinishConfirmDialog, got %T", m.modal)
	}
}

func TestUpdate_ConfirmFinishReleaseMsg_StartsOperation(t *testing.T) {
	mgr := &mockManager{
		finishReleaseResult: domain.Release{ID: "rel-1"},
		finishReleaseDone:   make(chan struct{}),
	}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	pending := "rel-1"
	m.pendingFinishReleaseID = &pending
	m.modal = modal.NewReleaseFinishConfirmDialog("rel-1", domain.Release{ID: "rel-1"}, m.cfg)

	updated, cmd := m.Update(modal.ConfirmFinishReleaseMsg{ReleaseID: "rel-1"})
	m = updated.(Model)

	if !m.opRunning {
		t.Fatal("confirm finish release should set opRunning=true")
	}
	if cmd == nil {
		t.Fatal("confirm finish release should return command")
	}
	if m.modal != nil {
		t.Fatal("modal should be cleared after confirm")
	}
	if m.pendingFinishReleaseID != nil {
		t.Fatal("pending finish release ID should be cleared after confirm")
	}
	if !strings.Contains(m.outputPanel.View(), "Finishing release rel-1...") {
		t.Fatalf("output should include finish start line, got %q", m.outputPanel.View())
	}

	select {
	case <-mgr.finishReleaseDone:
	case <-time.After(2 * time.Second):
		t.Fatal("FinishRelease was not called")
	}

	mgr.mu.Lock()
	calls := mgr.finishReleaseCalls
	id := mgr.finishReleaseID
	mgr.mu.Unlock()
	if calls != 1 {
		t.Fatalf("FinishRelease calls = %d, want 1", calls)
	}
	if id != "rel-1" {
		t.Fatalf("FinishRelease releaseID = %q, want rel-1", id)
	}
}

func TestUpdate_ConfirmFinishReleaseMsg_NoPending_DoesNotExecute(t *testing.T) {
	mgr := &mockManager{finishReleaseResult: domain.Release{ID: "rel-1"}}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	m.modal = modal.NewReleaseFinishConfirmDialog("rel-1", domain.Release{ID: "rel-1"}, m.cfg)

	updated, cmd := m.Update(modal.ConfirmFinishReleaseMsg{ReleaseID: "rel-1"})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("confirm finish release without pending ID must not return command")
	}
	if m.opRunning {
		t.Fatal("confirm finish release without pending ID must not set opRunning")
	}
	if mgr.finishReleaseCalls != 0 {
		t.Fatalf("finish release should not execute, got calls=%d", mgr.finishReleaseCalls)
	}
}

func TestUpdate_ConfirmFinishReleaseMsg_WrongModal_DoesNotExecute(t *testing.T) {
	mgr := &mockManager{finishReleaseResult: domain.Release{ID: "rel-1"}}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	pending := "rel-1"
	m.pendingFinishReleaseID = &pending
	m.modal = modal.NewHelpOverlayWithOptions(false)

	updated, cmd := m.Update(modal.ConfirmFinishReleaseMsg{ReleaseID: "rel-1"})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("confirm finish release with wrong active modal must not return command")
	}
	if m.opRunning {
		t.Fatal("confirm finish release with wrong active modal must not set opRunning")
	}
	if mgr.finishReleaseCalls != 0 {
		t.Fatalf("finish release should not execute, got calls=%d", mgr.finishReleaseCalls)
	}
}

func TestUpdate_ConfirmFinishReleaseMsg_MismatchedID_DoesNotExecute(t *testing.T) {
	mgr := &mockManager{finishReleaseResult: domain.Release{ID: "rel-1"}}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	pending := "rel-1"
	m.pendingFinishReleaseID = &pending
	m.modal = modal.NewReleaseFinishConfirmDialog("rel-1", domain.Release{ID: "rel-1"}, m.cfg)

	updated, cmd := m.Update(modal.ConfirmFinishReleaseMsg{ReleaseID: "rel-2"})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("confirm finish release with mismatched ID must not return command")
	}
	if m.opRunning {
		t.Fatal("confirm finish release with mismatched ID must not set opRunning")
	}
	if mgr.finishReleaseCalls != 0 {
		t.Fatalf("finish release should not execute, got calls=%d", mgr.finishReleaseCalls)
	}
}

func TestUpdate_FinishReleaseDoneMsg_Success_AppendsDoneAndReloads(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	m.opRunning = true

	updated, cmd := m.Update(FinishReleaseDoneMsg{Release: domain.Release{ID: "rel-1"}})
	m = updated.(Model)

	if m.opRunning {
		t.Fatal("FinishReleaseDoneMsg should clear opRunning")
	}
	if !strings.Contains(m.outputPanel.View(), "Finish release done: rel-1") {
		t.Fatalf("output should include finish done line, got %q", m.outputPanel.View())
	}
	if cmd == nil {
		t.Fatal("FinishReleaseDoneMsg should trigger releases refresh command")
	}
	msg := cmd()
	if _, ok := msg.(ReleasesLoadedMsg); !ok {
		t.Fatalf("expected ReleasesLoadedMsg, got %T", msg)
	}
}

func TestUpdate_FinishReleaseDoneMsg_Error_AppendsFailureAndReloads(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	m.opRunning = true

	updated, cmd := m.Update(FinishReleaseDoneMsg{Release: domain.Release{ID: "rel-1"}, Err: &mockError{msg: "tag push failed"}})
	m = updated.(Model)

	if m.opRunning {
		t.Fatal("FinishReleaseDoneMsg should clear opRunning")
	}
	if !strings.Contains(m.outputPanel.View(), "Finish release failed: tag push failed") {
		t.Fatalf("output should include finish failure line, got %q", m.outputPanel.View())
	}
	if cmd == nil {
		t.Fatal("FinishReleaseDoneMsg should trigger releases refresh command")
	}
	msg := cmd()
	if _, ok := msg.(ReleasesLoadedMsg); !ok {
		t.Fatalf("expected ReleasesLoadedMsg, got %T", msg)
	}
}

func TestUpdate_CloseModalMsg_FinishConfirm_ClearsPendingAndAppendsCancelled(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)
	pending := "rel-1"
	m.pendingFinishReleaseID = &pending
	m.modal = modal.NewReleaseFinishConfirmDialog("rel-1", domain.Release{ID: "rel-1"}, m.cfg)

	updated, _ := m.Update(modal.CloseModalMsg{})
	m = updated.(Model)

	if m.pendingFinishReleaseID != nil {
		t.Fatal("pending finish release ID should be cleared on cancel")
	}
	if m.modal != nil {
		t.Fatal("modal should be cleared on cancel")
	}
	if !strings.Contains(m.outputPanel.View(), "Finish release cancelled.") {
		t.Fatalf("output should include finish cancellation, got %q", m.outputPanel.View())
	}
}

func TestUpdate_ServicesLoadedMsg_DoesNotAppendCompletionLog(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)
	m.refreshing = true

	updated, _ := m.Update(ServicesLoadedMsg{TaskID: "IN-1", Services: []domain.Service{{Name: "api"}}})
	m = updated.(Model)
	if strings.Contains(m.outputPanel.View(), "Services loaded for task IN-1.") {
		t.Fatalf("services load should not append noisy completion log, got %q", m.outputPanel.View())
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
		{FocusReleases, "releases"},
		{FocusPanel(99), "unknown"},
	}
	for _, tc := range tests {
		if got := tc.panel.String(); got != tc.want {
			t.Errorf("FocusPanel(%d).String(): expected %q, got %q", tc.panel, tc.want, got)
		}
	}
}

func TestFocusPanel_NextPrev(t *testing.T) {

	if got := FocusTasks.Next(); got != FocusServices {
		t.Errorf("FocusTasks.Next(): expected FocusServices, got %v", got)
	}
	if got := FocusServices.Next(); got != FocusReleases {
		t.Errorf("FocusServices.Next(): expected FocusReleases, got %v", got)
	}

	if got := FocusReleases.Next(); got != FocusOutput {
		t.Errorf("FocusReleases.Next(): expected FocusOutput, got %v", got)
	}
	if got := FocusOutput.Next(); got != FocusTasks {
		t.Errorf("FocusOutput.Next(): expected FocusTasks, got %v", got)
	}

	if got := FocusServices.Prev(); got != FocusTasks {
		t.Errorf("FocusServices.Prev(): expected FocusTasks, got %v", got)
	}
	if got := FocusTasks.Prev(); got != FocusOutput {
		t.Errorf("FocusTasks.Prev(): expected FocusOutput, got %v", got)
	}

	if got := FocusReleases.Prev(); got != FocusServices {
		t.Errorf("FocusReleases.Prev(): expected FocusServices, got %v", got)
	}

	if got := FocusOutput.Prev(); got != FocusReleases {
		t.Errorf("FocusOutput.Prev(): expected FocusReleases, got %v", got)
	}
}

type mockError struct{ msg string }

func (e *mockError) Error() string { return e.msg }

func TestUpdate_PushServiceMsg_OpensConfirmModal(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)
	m.servicesPanel.SetServices("IN-001", []domain.Service{{Name: "svc-a", Branch: "feature/IN-001", RemoteURL: "git@host/repo.git"}})

	updated, cmd := m.Update(panels.PushServiceMsg{TaskID: "IN-001", ServiceName: "svc-a"})
	m = updated.(Model)

	if m.opRunning {
		t.Error("opRunning must stay false before push confirm")
	}
	if cmd != nil {
		t.Fatal("PushServiceMsg should not return command before confirm")
	}
	if _, ok := m.modal.(*modal.PushConfirmDialog); !ok {
		t.Fatalf("expected PushConfirmDialog, got %T", m.modal)
	}
}

func TestUpdate_PushTaskMsg_OpensConfirmModal(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)
	m.servicesPanel.SetServices("IN-777", []domain.Service{{Name: "svc-a", Branch: "feature/IN-777"}})

	updated, cmd := m.Update(panels.PushTaskMsg{TaskID: "IN-777"})
	m = updated.(Model)

	if m.opRunning {
		t.Error("opRunning must stay false before push confirm")
	}
	if cmd != nil {
		t.Fatal("PushTaskMsg should not return command before confirm")
	}
	if _, ok := m.modal.(*modal.PushConfirmDialog); !ok {
		t.Fatalf("expected PushConfirmDialog, got %T", m.modal)
	}
}

func TestPushTargets_ProtectedBranch_SetsProtectedTrue(t *testing.T) {
	mgr := &mockManager{isProtectedBranchResult: map[string]bool{"main": true}}
	m := newTestModel(t, mgr)
	m.servicesPanel.SetServices("IN-001", []domain.Service{{Name: "api", Branch: "main"}})

	targets := m.pushTargets("IN-001", "")
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if !targets[0].Protected {
		t.Fatalf("expected Protected=true for main branch, got %v", targets[0].Protected)
	}
}

func TestPushTargets_FeatureBranch_SetsProtectedFalse(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m.servicesPanel.SetServices("IN-002", []domain.Service{{Name: "api", Branch: "feature/IN-002"}})

	targets := m.pushTargets("IN-002", "")
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].Protected {
		t.Fatalf("expected Protected=false for feature branch, got %v", targets[0].Protected)
	}
}

func TestUpdate_SubmitPushMsg_StartsPushOperation(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	m.modal = modal.NewPushConfirmDialog("IN-001", "svc-a", nil)
	m.pendingPushSubmit = &modal.SubmitPushMsg{TaskID: "IN-001", ServiceName: "svc-a"}

	updated, cmd := m.Update(modal.SubmitPushMsg{TaskID: "IN-001", ServiceName: "svc-a"})
	m = updated.(Model)

	if !m.opRunning {
		t.Fatal("submit push should set opRunning")
	}
	if cmd == nil {
		t.Fatal("submit push should return command")
	}
	runBatchCommands(cmd())
	if mgr.pushServiceCalls != 1 || mgr.pushServiceTask != "IN-001" || mgr.pushServiceName != "svc-a" {
		t.Fatalf("push service call mismatch: calls=%d task=%q service=%q", mgr.pushServiceCalls, mgr.pushServiceTask, mgr.pushServiceName)
	}
	if !strings.Contains(m.outputPanel.View(), "Pushing service svc-a for task IN-001...") {
		t.Fatalf("output should include push start, got %q", m.outputPanel.View())
	}
}

func TestUpdate_SubmitPushMsg_WithoutActivePushModal_DoesNotExecute(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	m.modal = modal.NewHelpOverlayWithOptions(false)
	m.pendingPushSubmit = &modal.SubmitPushMsg{TaskID: "IN-001", ServiceName: "svc-a"}

	updated, cmd := m.Update(modal.SubmitPushMsg{TaskID: "IN-001", ServiceName: "svc-a"})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("submit push without push modal must not return command")
	}
	if m.opRunning {
		t.Fatal("submit push without push modal must not set opRunning")
	}
	if mgr.pushTaskCalls != 0 || mgr.pushServiceCalls != 0 {
		t.Fatalf("push should not execute, got pushTask=%d pushService=%d", mgr.pushTaskCalls, mgr.pushServiceCalls)
	}
}

func TestUpdate_SubmitPushMsg_WithMismatchedPending_DoesNotExecute(t *testing.T) {
	mgr := &mockManager{}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	m.modal = modal.NewPushConfirmDialog("IN-001", "svc-a", nil)
	m.pendingPushSubmit = &modal.SubmitPushMsg{TaskID: "IN-001", ServiceName: "svc-b"}

	updated, cmd := m.Update(modal.SubmitPushMsg{TaskID: "IN-001", ServiceName: "svc-a"})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("submit push with mismatched pending must not return command")
	}
	if m.opRunning {
		t.Fatal("submit push with mismatched pending must not set opRunning")
	}
	if mgr.pushTaskCalls != 0 || mgr.pushServiceCalls != 0 {
		t.Fatalf("push should not execute, got pushTask=%d pushService=%d", mgr.pushTaskCalls, mgr.pushServiceCalls)
	}
}

func TestUpdate_CloseModalMsg_PushConfirm_AppendsCancelledMessage(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)
	m.modal = modal.NewPushConfirmDialog("IN-001", "", nil)
	m.pendingPushSubmit = &modal.SubmitPushMsg{TaskID: "IN-001"}

	updated, _ := m.Update(modal.CloseModalMsg{})
	m = updated.(Model)

	if m.pendingPushSubmit != nil {
		t.Fatal("pending push submit should be cleared on cancel")
	}
	if !strings.Contains(m.outputPanel.View(), "Push cancelled.") {
		t.Fatalf("output should include push cancelled, got %q", m.outputPanel.View())
	}
}

func TestUpdate_CloseModalMsg_ReleaseExecuteConfirm_ClearsPendingAndAppendsCancelled(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)
	m.pendingReleaseSubmit = &modal.SubmitCreateReleaseMsg{TaskIDs: []string{"ZA-1"}, Versions: map[string]string{"api": "1.2.3"}}
	m.modal = modal.NewReleaseExecuteConfirmDialog([]string{"ZA-1"}, map[string]string{"api": "1.2.3"}, task.ReleasePreview{})

	updated, _ := m.Update(modal.CloseModalMsg{})
	m = updated.(Model)

	if m.pendingReleaseSubmit != nil {
		t.Fatal("pending release submit should be cleared on cancel")
	}
	if !strings.Contains(m.outputPanel.View(), "Release execution cancelled.") {
		t.Fatalf("output should include release cancellation, got %q", m.outputPanel.View())
	}
}

func TestUpdate_PartialInitDoneMsg_AppendsWarningSummary(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)
	m.opRunning = true

	updated, _ := m.Update(PartialInitDoneMsg{
		Op: "Init task IN-1",
		Result: task.PartialFailureResult{
			SucceededServices: []string{"svc-a"},
			FailedServices:    []task.FailedService{{Name: "svc-b", Cause: &mockError{msg: "clone failed"}}},
		},
	})
	m = updated.(Model)

	view := m.outputPanel.View()
	if !strings.Contains(view, "Init task IN-1 partially done.") {
		t.Fatalf("output should include partial init warning, got %q", view)
	}
	if !strings.Contains(view, "Succeeded: svc-a") || !strings.Contains(view, "Failed: svc-b (clone failed)") {
		t.Fatalf("output should include succeeded/failed services, got %q", view)
	}
}

func TestUpdate_PartialAddDoneMsg_AppendsWarningSummary(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)
	m.opRunning = true

	updated, _ := m.Update(PartialAddDoneMsg{
		Op: "Add services to IN-1",
		Result: task.PartialFailureResult{
			SucceededServices: []string{"svc-a"},
			FailedServices:    []task.FailedService{{Name: "svc-c", Cause: &mockError{msg: "branch exists"}}},
		},
	})
	m = updated.(Model)

	view := m.outputPanel.View()
	if !strings.Contains(view, "Add services to IN-1 partially done.") {
		t.Fatalf("output should include partial add warning, got %q", view)
	}
	if !strings.Contains(view, "Succeeded: svc-a") || !strings.Contains(view, "Failed: svc-c (branch exists)") {
		t.Fatalf("output should include succeeded/failed services, got %q", view)
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

func TestUpdate_OpenLazygitServiceMsg_StaleService_AppendsOutputAndReturnsNoCommand(t *testing.T) {
	mgr := &mockManager{}
	m := newLazygitUpdateTestModel(t, mgr)

	updated, cmd := m.Update(panels.OpenLazygitServiceMsg{
		TaskID:       "IN-001",
		ServiceName:  "svc-a",
		WorktreePath: "/tmp/missing-svc-a",
		Stale:        true,
	})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("stale lazygit service must not return a command")
	}
	if m.opRunning {
		t.Fatal("stale lazygit service must not start operation")
	}
	assertNoManagerRefresh(t, mgr)
	view := m.outputPanel.View()
	if !strings.Contains(view, "Cannot open lazygit for service svc-a") || !strings.Contains(view, "stale") {
		t.Fatalf("output should mention stale service, got:\n%s", view)
	}
}

func TestUpdate_OpenLazygitServiceMsg_NoServiceSelected_AppendsOutputAndReturnsNoCommand(t *testing.T) {
	mgr := &mockManager{}
	m := newLazygitUpdateTestModel(t, mgr)

	updated, cmd := m.Update(panels.OpenLazygitServiceMsg{TaskID: "IN-001"})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("empty lazygit service selection must not return a command")
	}
	if m.opRunning {
		t.Fatal("empty lazygit service selection must not start operation")
	}
	assertNoManagerRefresh(t, mgr)
	if !strings.Contains(m.outputPanel.View(), "No service selected.") {
		t.Fatalf("output should mention no service selection, got:\n%s", m.outputPanel.View())
	}
}

func TestUpdate_OpenLazygitServiceMsg_EmptyWorktreePath_AppendsOutputAndReturnsNoCommand(t *testing.T) {
	mgr := &mockManager{}
	m := newLazygitUpdateTestModel(t, mgr)

	updated, cmd := m.Update(panels.OpenLazygitServiceMsg{
		TaskID:      "IN-001",
		ServiceName: "svc-a",
	})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("empty lazygit worktree path must not return a command")
	}
	if m.opRunning {
		t.Fatal("empty lazygit worktree path must not start operation")
	}
	assertNoManagerRefresh(t, mgr)
	if !strings.Contains(m.outputPanel.View(), "selected service has no worktree path") {
		t.Fatalf("output should mention empty worktree path, got:\n%s", m.outputPanel.View())
	}
}

func TestUpdate_OpenLazygitServiceMsg_MissingPath_AppendsOutputAndReturnsNoCommand(t *testing.T) {
	mgr := &mockManager{}
	m := newLazygitUpdateTestModel(t, mgr)
	missingPath := filepath.Join(t.TempDir(), "missing-service")

	updated, cmd := m.Update(panels.OpenLazygitServiceMsg{
		TaskID:       "IN-001",
		ServiceName:  "svc-a",
		WorktreePath: missingPath,
	})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("missing lazygit worktree path must not return a command")
	}
	if m.opRunning {
		t.Fatal("missing lazygit worktree path must not start operation")
	}
	assertNoManagerRefresh(t, mgr)
	view := m.outputPanel.View()
	if !strings.Contains(view, "worktree path is missing or inaccessible") || !strings.Contains(view, missingPath) {
		t.Fatalf("output should mention missing worktree path, got:\n%s", view)
	}
}

func TestUpdate_OpenLazygitServiceMsg_ValidDirectory_StartsOperation(t *testing.T) {
	m := newLazygitUpdateTestModel(t, &mockManager{})
	worktreePath := t.TempDir()

	updated, cmd := m.Update(panels.OpenLazygitServiceMsg{
		TaskID:       "IN-001",
		ServiceName:  "svc-a",
		WorktreePath: worktreePath,
	})
	m = updated.(Model)

	if cmd == nil {
		t.Fatal("valid lazygit worktree path must return a command")
	}
	if !m.opRunning {
		t.Fatal("valid lazygit worktree path must start operation")
	}
	want := "Opening lazygit for service svc-a from " + worktreePath + "..."
	if !strings.Contains(m.outputPanel.View(), want) {
		t.Fatalf("output should mention lazygit launch, want %q, got:\n%s", want, m.outputPanel.View())
	}
}

func assertNoManagerRefresh(t *testing.T, mgr *mockManager) {
	t.Helper()
	if mgr.listTasksCalls != 0 {
		t.Fatalf("validation failure List calls = %d, want 0", mgr.listTasksCalls)
	}
	if mgr.listServicesCalls != 0 {
		t.Fatalf("validation failure ListServices calls = %d, want 0", mgr.listServicesCalls)
	}
}

func TestUpdate_LazygitDoneMsg_SuccessAppendsDoneStopsOperationAndRefreshes(t *testing.T) {
	mgr := &mockManager{
		listTasksResult:    []domain.Task{{ID: "TASK-6"}},
		listServicesResult: []domain.Service{{Name: "api"}},
	}
	m := newLazygitUpdateTestModel(t, mgr)
	m.opRunning = true

	updated, cmd := m.Update(LazygitDoneMsg{TaskID: "TASK-6", ServiceName: "api", WorktreePath: "/tmp/api"})
	m = updated.(Model)

	if m.opRunning {
		t.Fatal("opRunning must be false after LazygitDoneMsg")
	}
	if !strings.Contains(m.outputPanel.View(), "Open lazygit for api done.") {
		t.Fatalf("output should contain lazygit done message, got %q", m.outputPanel.View())
	}
	assertLazygitRefreshCommands(t, cmd, mgr, "TASK-6")
}

func TestUpdate_LazygitDoneMsg_ErrorAppendsFailureStopsOperationAndRefreshes(t *testing.T) {
	mgr := &mockManager{
		listTasksResult:    []domain.Task{{ID: "TASK-6"}},
		listServicesResult: []domain.Service{{Name: "api"}},
	}
	m := newLazygitUpdateTestModel(t, mgr)
	m.opRunning = true

	updated, cmd := m.Update(LazygitDoneMsg{
		TaskID:       "TASK-6",
		ServiceName:  "api",
		WorktreePath: "/tmp/api",
		Err:          &mockError{"exit status 1"},
	})
	m = updated.(Model)

	if m.opRunning {
		t.Fatal("opRunning must be false after LazygitDoneMsg error")
	}
	if !strings.Contains(m.outputPanel.View(), "Open lazygit for api failed: exit status 1") {
		t.Fatalf("output should contain lazygit failure message, got %q", m.outputPanel.View())
	}
	assertLazygitRefreshCommands(t, cmd, mgr, "TASK-6")
}

func TestUpdate_LazygitDoneMsg_ExecutableNotFoundAppendsPathGuidance(t *testing.T) {
	m := newLazygitUpdateTestModel(t, &mockManager{})

	updated, _ := m.Update(LazygitDoneMsg{
		TaskID:      "TASK-6",
		ServiceName: "api",
		Err:         &exec.Error{Name: "lazygit", Err: exec.ErrNotFound},
	})
	m = updated.(Model)

	view := m.outputPanel.View()
	if !strings.Contains(view, "Open lazygit for api failed:") {
		t.Fatalf("output should contain lazygit failure message, got %q", view)
	}
	if !strings.Contains(view, "lazygit not found on PATH. Install lazygit or add it to PATH.") {
		t.Fatalf("output should contain PATH guidance, got %q", view)
	}
}

func TestUpdate_LazygitDoneMsg_WorktreeMissingDoesNotAppendPathGuidance(t *testing.T) {
	m := newLazygitUpdateTestModel(t, &mockManager{})

	updated, _ := m.Update(LazygitDoneMsg{
		TaskID:      "TASK-6",
		ServiceName: "api",
		Err:         &os.PathError{Op: "chdir", Path: "/tmp/api", Err: os.ErrNotExist},
	})
	m = updated.(Model)

	view := m.outputPanel.View()
	if !strings.Contains(view, "Open lazygit for api failed:") {
		t.Fatalf("output should contain lazygit failure message, got %q", view)
	}
	if strings.Contains(view, "lazygit not found on PATH. Install lazygit or add it to PATH.") {
		t.Fatalf("output should not contain PATH guidance for missing worktree path, got %q", view)
	}
}

func assertLazygitRefreshCommands(t *testing.T, cmd tea.Cmd, mgr *mockManager, wantTaskID string) {
	t.Helper()
	if cmd == nil {
		t.Fatal("LazygitDoneMsg must return refresh commands")
	}

	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("refresh command returned %T, want tea.BatchMsg", msg)
	}
	if len(batch) != 2 {
		t.Fatalf("refresh batch command count = %d, want 2", len(batch))
	}
	for _, refreshCmd := range batch {
		if refreshCmd == nil {
			t.Fatal("refresh batch contains nil command")
		}
		refreshCmd()
	}
	if mgr.listTasksCalls != 1 {
		t.Fatalf("List calls = %d, want 1", mgr.listTasksCalls)
	}
	if mgr.listServicesCalls != 1 {
		t.Fatalf("ListServices calls = %d, want 1", mgr.listServicesCalls)
	}
	if mgr.listServicesTaskID != wantTaskID {
		t.Fatalf("ListServices taskID = %q, want %q", mgr.listServicesTaskID, wantTaskID)
	}
}

func runBatchCommands(msg tea.Msg) {
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return
	}
	for _, cmd := range batch {
		if cmd != nil {
			runBatchCommands(cmd())
		}
	}
}

func extractValidationFromBatch(msg tea.Msg) (ValidationResultMsg, bool) {
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return ValidationResultMsg{}, false
	}
	for _, cmd := range batch {
		if cmd == nil {
			continue
		}
		if vm, ok := cmd().(ValidationResultMsg); ok {
			return vm, true
		}
	}
	return ValidationResultMsg{}, false
}

func TestUpdate_ValidationResultMsg_BlockingOpensValidationModal(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)

	updated, cmd := m.Update(ValidationResultMsg{Validation: domain.TaskValidation{TaskID: "IN-1", Blocking: true}})
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("blocking validation should not return cmd")
	}
	if _, ok := m.modal.(*modal.ValidationErrorModal); !ok {
		t.Fatalf("expected ValidationErrorModal, got %T", m.modal)
	}
}

func TestUpdate_ValidationResultMsg_NonBlockingAppendsOutput(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)

	updated, _ := m.Update(ValidationResultMsg{Validation: domain.TaskValidation{TaskID: "IN-1", Blocking: false}})
	m = updated.(Model)
	if !strings.Contains(m.outputPanel.View(), "All services clean.") {
		t.Fatalf("expected clean message in output, got %q", m.outputPanel.View())
	}
}

func TestUpdate_ClosePlanReadyMsg_OpensConfirmModal(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)
	m.tasksPanel.SetTasks([]domain.Task{{ID: "IN-1", Phase: "feature"}})

	updated, _ := m.Update(ClosePlanReadyMsg{Plan: task.ClosePlan{TaskID: "IN-1"}})
	m = updated.(Model)
	closeModal, ok := m.modal.(*modal.CloseTaskConfirmModal)
	if !ok {
		t.Fatalf("expected CloseTaskConfirmModal, got %T", m.modal)
	}
	if got := closeModal.Title(); got != "Close Feature Task: IN-1" {
		t.Fatalf("confirm title = %q", got)
	}
}

func TestUpdate_CloseTaskFinishedMsg_OpensSummaryAndReloads(t *testing.T) {
	mgr := &mockManager{listTasksResult: []domain.Task{{ID: "IN-1"}}}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)
	m.tasksPanel.SetTasks([]domain.Task{{ID: "IN-1", Phase: "hotfix", Version: "1.2.1"}})
	m.pendingCloseTask = &domain.Task{ID: "IN-1", Phase: "hotfix", Version: "1.2.1"}

	updated, cmd := m.Update(CloseTaskFinishedMsg{Result: task.CloseTaskResult{TaskID: "IN-1", Success: true}})
	m = updated.(Model)
	closeModal, ok := m.modal.(*modal.CloseTaskSummaryModal)
	if !ok {
		t.Fatalf("expected CloseTaskSummaryModal, got %T", m.modal)
	}
	if got := closeModal.Title(); got != "Close Hotfix Task: IN-1 (v1.2.1)" {
		t.Fatalf("summary title = %q", got)
	}
	if m.pendingCloseTask != nil {
		t.Fatal("pendingCloseTask should be cleared after close finishes")
	}
	if cmd == nil {
		t.Fatal("close finished should reload tasks/services")
	}
}

func TestUpdate_PrunePlanReadyMsg_OpensPruneModal(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)

	updated, _ := m.Update(PrunePlanReadyMsg{Candidates: []domain.PruneCandidate{{TaskID: "IN-1", Prunable: true}}})
	m = updated.(Model)
	if _, ok := m.modal.(*modal.PruneConfirmModal); !ok {
		t.Fatalf("expected PruneConfirmModal, got %T", m.modal)
	}
}

func TestUpdate_TagListMsg_OpensTagBrowserModal(t *testing.T) {
	m := newTestModel(t, &mockManager{})
	m = sendWindowSize(m, 120, 40)

	updated, _ := m.Update(TagListMsg{TaskID: "IN-1", Tags: []domain.TagInfo{{Name: "v1.0.0"}}})
	m = updated.(Model)
	if _, ok := m.modal.(*modal.TagBrowserModal); !ok {
		t.Fatalf("expected TagBrowserModal, got %T", m.modal)
	}
}

func TestUpdate_OpenForgeMenuMsg_OpensForgeMenuModal(t *testing.T) {
	m := newTestModelWithOptions(t, &mockManager{}, Options{ForgeClients: map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitLab: nil}})
	m = sendWindowSize(m, 120, 40)

	updated, _ := m.Update(panels.OpenForgeMenuMsg{TaskID: "IN-1", ServiceName: "svc-a", Provider: forge.ForgeProviderGitLab})
	m = updated.(Model)
	if _, ok := m.modal.(*modal.ForgeMenuModal); !ok {
		t.Fatalf("expected ForgeMenuModal, got %T", m.modal)
	}
}

func TestUpdate_SubmitSyncStrategyMsg_InterceptsWithValidationFirst(t *testing.T) {
	mgr := &mockManager{validateResult: domain.TaskValidation{TaskID: "IN-1", Blocking: false}}
	m := newTestModel(t, mgr)
	m = sendWindowSize(m, 120, 40)

	updated, cmd := m.Update(modal.SubmitSyncStrategyMsg{TaskID: "IN-1", Strategy: task.SyncStrategyRebase})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("submit sync should start validation cmd")
	}
	msg := cmd()
	vmsg, ok := extractValidationFromBatch(msg)
	if !ok {
		t.Fatalf("expected ValidationResultMsg in batch, got %T", msg)
	}
	if mgr.syncTaskCalls != 0 {
		t.Fatalf("sync should not run before validation, got calls=%d", mgr.syncTaskCalls)
	}

	updated, cmd = m.Update(vmsg)
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("non-blocking validation should continue with sync")
	}
	runBatchCommands(cmd())
	if mgr.syncTaskCalls == 0 {
		t.Fatal("expected sync task call after successful validation")
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
	if !strings.Contains(m.outputPanel.View(), "Validating task IN-010 before sync...") {
		t.Errorf("output panel should contain validation-first sync message, got:\n%s", m.outputPanel.View())
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
