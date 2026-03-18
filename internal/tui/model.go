package tui

import (
	"log/slog"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/diss0x/wtui/internal/config"
	"github.com/diss0x/wtui/internal/task"
	"github.com/diss0x/wtui/internal/tui/modal"
	"github.com/diss0x/wtui/internal/tui/panels"
)

// Model is the root bubbletea model for the wtui TUI.
// It owns the top-level layout, focus state machine, panel models, and modal overlay.
// All UI mutations happen exclusively through Update(); View() is a pure render function.
type Model struct {
	// Core dependencies injected at construction time.
	cfg    *config.Config
	logger *slog.Logger
	mgr    task.Manager

	// focus tracks which panel currently has keyboard focus.
	focus FocusPanel

	// Terminal dimensions, updated on every tea.WindowSizeMsg.
	width  int
	height int

	// ready is false until the first tea.WindowSizeMsg is received.
	ready bool

	// Panels
	tasksPanel    panels.TasksPanel
	servicesPanel panels.ServicesPanel
	outputPanel   panels.OutputPanel
	reposPanel    panels.ReposPanel

	// modal is the active dialog overlay. nil when no modal is shown.
	modal modal.Modal

	// spinner is shown in the footer during long-running operations.
	spinner   spinner.Model
	opRunning bool

	// outputCh is non-nil when an operation is streaming line-by-line output.
	// Each OutputLineMsg handler reads the next line and re-dispatches readNextLine.
	outputCh <-chan string

	// keymap holds all global keybindings.
	keymap KeyMap

	// styles holds all lipgloss style constants.
	styles Styles

	// shellInput holds the current shell command being typed.
	// When non-nil, the shell prompt is active (footer replaced by input line).
	shellInput *shellInputState
}

// shellInputState tracks the inline shell command prompt state.
// It is non-nil only while the user is typing a command after pressing [;].
type shellInputState struct {
	taskDir string // absolute path to run the command in
	input   string // current text typed by the user
	cursor  int    // insertion point within input (rune index)
}

// New constructs the root TUI model wired to the provided task.Manager.
//
// cfg must already have Effective() applied (all defaults filled in).
// mgr must not be nil; it is the business logic layer called by all tea.Cmd factories.
// logger must not be nil; use slog.Default() if no custom logger is needed.
func New(cfg *config.Config, mgr task.Manager, logger *slog.Logger) (Model, error) {
	if cfg == nil {
		return Model{}, errorf("tui.New: cfg must not be nil")
	}
	if mgr == nil {
		return Model{}, errorf("tui.New: mgr must not be nil")
	}
	if logger == nil {
		return Model{}, errorf("tui.New: logger must not be nil")
	}

	sp := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(colorPrimary)),
	)

	// Panels are initialised with zero dimensions; recalculateDimensions() will
	// assign correct sizes once the first tea.WindowSizeMsg arrives.
	m := Model{
		cfg:           cfg,
		logger:        logger,
		mgr:           mgr,
		keymap:        DefaultKeyMap(),
		styles:        NewStyles(),
		focus:         FocusTasks,
		spinner:       sp,
		tasksPanel:    panels.NewTasksPanel(25, 10),
		servicesPanel: panels.NewServicesPanel(55, 10),
		outputPanel:   panels.NewOutputPanel(80, cfg.OutputPanelLines+2),
		reposPanel:    panels.NewReposPanel(55, 10),
	}

	// Initialise focus state on the tasks panel.
	m.tasksPanel.SetFocused(true)

	return m, nil
}

// ── bubbletea.Model interface ─────────────────────────────────────────────────

// Init is called by bubbletea once the program starts.
// It fires a window-size poll plus background data-loading and the spinner.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		loadTasksCmd(m.mgr),
		loadReposCmd(m.mgr),
		m.spinner.Tick,
	)
}

// Update is the central message dispatcher.  It handles all tea.Msg types,
// mutates a copy of the model, and optionally returns side-effect commands.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	// ── Layout ────────────────────────────────────────────────────────────
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.recalculateDimensions()
		m.ready = true
		m.logger.Debug("terminal resized",
			slog.Int("width", m.width),
			slog.Int("height", m.height),
		)
		return m, nil

	// ── Keyboard ──────────────────────────────────────────────────────────
	case tea.KeyMsg:
		// If the shell input prompt is active, handle all keys there.
		if m.shellInput != nil {
			return m.updateShellInput(msg)
		}

		// If a modal is open, forward all keys to it first.
		if m.modal != nil {
			newModal, cmd := m.modal.Update(msg)
			m.modal = newModal
			return m, cmd
		}

		// Handle global keys.
		switch {
		case key.Matches(msg, m.keymap.Quit), key.Matches(msg, m.keymap.ForceQuit):
			m.logger.Info("quit requested")
			return m, tea.Quit

		case key.Matches(msg, m.keymap.Tab):
			m = m.cycleFocusForward()
			m.logger.Debug("focus changed", slog.String("panel", m.focus.String()))
			return m, nil

		case key.Matches(msg, m.keymap.ShiftTab):
			m = m.cycleFocusBackward()
			m.logger.Debug("focus changed", slog.String("panel", m.focus.String()))
			return m, nil

		case key.Matches(msg, m.keymap.Help):
			m.modal = modal.NewHelpOverlay()
			return m, nil

		case key.Matches(msg, m.keymap.Refresh):
			return m, tea.Batch(loadTasksCmd(m.mgr), loadReposCmd(m.mgr))
		}

		// Forward to the focused panel.
		switch m.focus {
		case FocusTasks:
			newPanel, cmd := m.tasksPanel.Update(msg)
			m.tasksPanel = newPanel
			return m, cmd

		case FocusServices:
			newPanel, cmd := m.servicesPanel.Update(msg)
			m.servicesPanel = newPanel
			return m, cmd

		case FocusOutput:
			newPanel, cmd := m.outputPanel.Update(msg)
			m.outputPanel = newPanel
			return m, cmd
		}

	// ── Panel business-logic messages ─────────────────────────────────────

	case panels.TaskSelectionChangedMsg:
		return m, loadServicesCmd(m.mgr, msg.TaskID)

	case panels.FocusServicesMsg:
		m.focus = FocusServices
		m.tasksPanel.SetFocused(false)
		m.servicesPanel.SetFocused(true)
		m.outputPanel.SetFocused(false)
		return m, loadServicesCmd(m.mgr, msg.TaskID)

	case panels.FocusTasksMsg:
		m.focus = FocusTasks
		m.tasksPanel.SetFocused(true)
		m.servicesPanel.SetFocused(false)
		m.outputPanel.SetFocused(false)
		return m, nil

	case panels.OpenInitDialogMsg:
		m.modal = modal.NewInitDialog(m.cfg.BranchPrefix)
		return m, nil

	case panels.OpenAddServiceMsg:
		m.modal = modal.NewAddDialog(msg.TaskID)
		return m, nil

	case panels.OpenRemoveDialogMsg:
		// Create the dialog immediately (with zero counts); dispatch a background
		// command to load accurate dirty-service info which will call UpdateInfo.
		m.modal = modal.NewRemoveDialog(msg.TaskID, 0, nil)
		return m, loadDirtyServicesCmd(m.mgr, msg.TaskID)

	case panels.OpenWorkspaceMsg:
		return m, openWorkspaceCmd(m.mgr, msg.TaskID)

	case panels.OpenFilePickerMsg:
		return m, loadOpenCandidatesCmd(m.mgr, msg.TaskID)

	case panels.GenerateSlnMsg:
		m.opRunning = true
		m.outputPanel.AppendLine("Generating .sln file...")
		return m, tea.Batch(generateSlnCmd(m.mgr, msg.TaskID), m.spinner.Tick)

	case panels.ShellExecMsg:
		m.shellInput = &shellInputState{taskDir: msg.TaskDir}
		return m, nil

	// ── Modal messages ────────────────────────────────────────────────────

	case modal.CloseModalMsg:
		m.modal = nil
		return m, nil

	case modal.SubmitInitMsg:
		m.modal = nil
		m.opRunning = true
		m.outputPanel.AppendLine("Initializing task " + msg.TaskID + "...")
		return m, tea.Batch(
			initTaskCmd(m.mgr, task.InitParams{
				TaskID:       msg.TaskID,
				Services:     msg.Services,
				BranchPrefix: msg.BranchPrefix,
				BaseBranch:   msg.BaseBranch,
			}),
			m.spinner.Tick,
		)

	case modal.SubmitAddMsg:
		m.modal = nil
		m.opRunning = true
		m.outputPanel.AppendLine("Adding services to " + msg.TaskID + "...")
		return m, tea.Batch(
			addServiceCmd(m.mgr, task.AddParams{
				TaskID:   msg.TaskID,
				Services: msg.Services,
			}),
			m.spinner.Tick,
		)

	case modal.SubmitRemoveMsg:
		m.modal = nil
		m.opRunning = true
		m.outputPanel.AppendLine("Removing task " + msg.TaskID + "...")
		return m, tea.Batch(
			removeTaskCmd(m.mgr, msg.TaskID, msg.Force),
			m.spinner.Tick,
		)

	case modal.SubmitOpenFileMsg:
		m.modal = nil
		return m, openFileCmd(m.mgr, msg.Path, msg.App)

	// ── Data-loading messages ─────────────────────────────────────────────

	case TasksLoadedMsg:
		m.tasksPanel.SetTasks(msg.Tasks)
		return m, nil

	case ServicesLoadedMsg:
		m.servicesPanel.SetServices(msg.TaskID, msg.Services)
		return m, nil

	case ReposLoadedMsg:
		m.reposPanel.SetRepos(msg.Repos)
		return m, nil

	case DirtyServicesLoadedMsg:
		// Update the RemoveDialog if it is still the active modal.
		if m.modal != nil {
			if rd, ok := m.modal.(*modal.RemoveDialog); ok {
				rd.UpdateInfo(msg.ServiceCount, msg.DirtyServices)
				m.modal = rd
			}
		}
		return m, nil

	case OpenCandidatesLoadedMsg:
		if len(msg.Candidates.Files) == 0 {
			m.outputPanel.AppendLine("No openable files found for task " + msg.TaskID)
			return m, nil
		}
		m.modal = modal.NewOpenDialog(msg.Candidates)
		return m, nil

	// ── Streaming output messages ─────────────────────────────────────────

	case OutputLineMsg:
		m.outputPanel.AppendLine(msg.Line)
		if m.outputCh != nil {
			return m, readNextLine(m.outputCh)
		}
		return m, nil

	case CommandDoneMsg:
		m.opRunning = false
		m.outputCh = nil
		if msg.Err != nil {
			m.outputPanel.AppendLine("Error: " + msg.Err.Error())
			m.logger.Error("command failed", slog.String("err", msg.Err.Error()))
		} else {
			m.outputPanel.AppendLine("Done.")
		}
		// Refresh the task list so new/removed tasks appear immediately.
		// Also reload services for the currently selected task (if any).
		return m, tea.Batch(loadTasksCmd(m.mgr), m.maybeLoadServicesCmd())

	// ── Spinner ───────────────────────────────────────────────────────────

	case spinner.TickMsg:
		// Only forward spinner ticks when an operation is running to avoid
		// unnecessary re-renders.
		if m.opRunning {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	return m, nil
}

// View renders the complete TUI layout as a string.
// It is called on every model change; it must be a pure function.
//
// Layout structure:
//
//	┌─ Header (1 line) ──────────────────────────────────┐
//	│ [Tasks panel 30%]  │  [Services panel 70%]         │
//	│                    │                               │
//	├────────────────────────────────────────────────────┤
//	│ [Output panel]                                     │
//	├────────────────────────────────────────────────────┤
//	│ [Footer: context-sensitive key hints]              │
//	└────────────────────────────────────────────────────┘
func (m Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	header := renderHeader(m)
	footer := renderFooter(m)

	// Left panel: task list.
	leftView := m.tasksPanel.View()

	// Right panel: services (primary) or repos (informational).
	// In v1 the services panel is always shown; the repos panel is a separate
	// reference widget that doesn't replace the services panel in the layout.
	rightView := m.servicesPanel.View()

	// Main row: tasks (left) + services (right) joined horizontally.
	mainRow := lipgloss.JoinHorizontal(lipgloss.Top, leftView, rightView)

	// Output panel below main row.
	outputView := m.outputPanel.View()

	// Compose the full screen layout.
	fullView := lipgloss.JoinVertical(lipgloss.Left,
		header,
		mainRow,
		outputView,
		footer,
	)

	// If a modal is active, render it as a centered overlay on top.
	if m.modal != nil {
		return modal.OverlayView(m.modal.View(), m.width, m.height)
	}

	return fullView
}

// ── Dimension management ──────────────────────────────────────────────────────

// recalculateDimensions recomputes and propagates panel sizes based on the
// current terminal dimensions.  Call this whenever m.width or m.height changes.
func (m *Model) recalculateDimensions() {
	const headerHeight = 1
	const footerHeight = 1

	// Output panel: content rows + 2 border rows.
	outputPanelHeight := m.cfg.OutputPanelLines + 2

	// Main panel height: total minus fixed rows.
	mainPanelHeight := m.height - headerHeight - footerHeight - outputPanelHeight
	if mainPanelHeight < 3 {
		mainPanelHeight = 3
	}

	// Tasks panel: 30% of terminal width, minimum 25 characters.
	tasksWidth := m.width * 30 / 100
	if tasksWidth < 25 {
		tasksWidth = 25
	}

	// Services / repos panels: remaining width.
	servicesWidth := m.width - tasksWidth
	if servicesWidth < 1 {
		servicesWidth = 1
	}

	m.tasksPanel.SetSize(tasksWidth, mainPanelHeight)
	m.servicesPanel.SetSize(servicesWidth, mainPanelHeight)
	m.outputPanel.SetSize(m.width, m.cfg.OutputPanelLines)
	m.reposPanel.SetSize(servicesWidth, mainPanelHeight)
}

// ── Focus cycling ─────────────────────────────────────────────────────────────

// cycleFocusForward advances focus to the next panel in the cycle and updates
// each panel's focused state accordingly.
func (m Model) cycleFocusForward() Model {
	m.focus = m.focus.Next()
	m.tasksPanel.SetFocused(m.focus == FocusTasks)
	m.servicesPanel.SetFocused(m.focus == FocusServices)
	m.outputPanel.SetFocused(m.focus == FocusOutput)
	return m
}

// cycleFocusBackward reverses focus to the previous panel in the cycle.
func (m Model) cycleFocusBackward() Model {
	m.focus = m.focus.Prev()
	m.tasksPanel.SetFocused(m.focus == FocusTasks)
	m.servicesPanel.SetFocused(m.focus == FocusServices)
	m.outputPanel.SetFocused(m.focus == FocusOutput)
	return m
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// maybeLoadServicesCmd returns loadServicesCmd for the currently selected task,
// or nil if no task is selected.
func (m Model) maybeLoadServicesCmd() tea.Cmd {
	t := m.tasksPanel.SelectedTask()
	if t == nil {
		return nil
	}
	return loadServicesCmd(m.mgr, t.ID)
}

// errorf is a thin wrapper around fmt.Errorf to avoid importing "fmt" in
// model.go only for error construction.
func errorf(msg string) error {
	return &modelError{msg: msg}
}

type modelError struct{ msg string }

func (e *modelError) Error() string { return e.msg }

// ── Shell input prompt ────────────────────────────────────────────────────────

// updateShellInput handles all key events while the inline shell prompt is active.
// It operates on a copy of m (value receiver) and returns a new model + optional cmd.
func (m Model) updateShellInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.shellInput = nil
		return m, nil

	case "enter":
		if m.shellInput.input == "" {
			m.shellInput = nil
			return m, nil
		}
		cmd := m.shellInput.input
		dir := m.shellInput.taskDir
		m.shellInput = nil
		return m, execShellCmd(cmd, dir)

	case "backspace", "ctrl+h":
		si := m.shellInput
		if si.cursor > 0 {
			runes := []rune(si.input)
			si.input = string(runes[:si.cursor-1]) + string(runes[si.cursor:])
			si.cursor--
		}
		return m, nil

	default:
		// Append printable characters at the cursor position.
		if len(msg.Runes) > 0 {
			si := m.shellInput
			runes := []rune(si.input)
			newRunes := make([]rune, 0, len(runes)+len(msg.Runes))
			newRunes = append(newRunes, runes[:si.cursor]...)
			newRunes = append(newRunes, msg.Runes...)
			newRunes = append(newRunes, runes[si.cursor:]...)
			si.input = string(newRunes)
			si.cursor += len(msg.Runes)
		}
		return m, nil
	}
}
