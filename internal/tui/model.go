package tui

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/diss0x/wtui/internal/config"
	"github.com/diss0x/wtui/internal/domain"
	"github.com/diss0x/wtui/internal/logutil"
	"github.com/diss0x/wtui/internal/task"
	"github.com/diss0x/wtui/internal/tui/modal"
	"github.com/diss0x/wtui/internal/tui/panels"
)

type Model struct {
	cfg    *config.Config
	logger *slog.Logger
	mgr    task.Manager

	focus         FocusPanel
	previousFocus FocusPanel // Tracks the panel before switching to Output

	width  int
	height int

	ready bool

	tasksPanel    panels.TasksPanel
	servicesPanel panels.ServicesPanel
	outputPanel   panels.OutputPanel

	// repos caches the discovered repositories for the InitDialog.
	repos []domain.Repo

	modal modal.Modal

	logOverlay *LogOverlay
	logPath    string

	spinner   spinner.Model
	opRunning bool

	keymap KeyMap

	styles Styles

	shellInput *shellInputState

	initDialogPending bool
}

type shellInputState struct {
	taskDir string
	input   string
	cursor  int
}

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
		spinner.WithStyle(lipgloss.NewStyle().Foreground(ColorPrimary)),
	)

	logPath := filepath.Join(logutil.XDGStateDir("wtui"), "wtui.log")

	m := Model{
		cfg:           cfg,
		logger:        logger,
		mgr:           mgr,
		keymap:        DefaultKeyMap(),
		styles:        NewStyles(),
		focus:         FocusTasks,
		spinner:       sp,
		logPath:       logPath,
		tasksPanel:    panels.NewTasksPanel(25, 10),
		servicesPanel: panels.NewServicesPanel(55, 10),
		outputPanel:   panels.NewOutputPanel(80, cfg.OutputPanelLines+2),
	}

	m.tasksPanel.SetFocused(true)

	return m, nil
}
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		loadTasksCmd(m.mgr),
		loadReposCmd(m.mgr),
		m.spinner.Tick,
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.recalculateDimensions()
		if m.logOverlay != nil {
			m.logOverlay.SetSize(msg.Width, msg.Height)
		}
		// Update modal terminal dimensions if open.
		if m.modal != nil {
			m.modal.SetTerminalSize(msg.Width, msg.Height)
		}
		m.ready = true
		m.logger.Debug("terminal resized",
			slog.Int("width", m.width),
			slog.Int("height", m.height),
		)
		return m, nil

	case LogTickMsg:
		if m.logOverlay == nil {
			return m, nil
		}
		m.logOverlay.Refresh()
		return m, logTickCmd()

	case tea.KeyMsg:
		if m.shellInput != nil {
			return m.updateShellInput(msg)
		}

		// When the log overlay is open, route all keys to it.
		// L and Esc close the overlay; everything else scrolls.
		if m.logOverlay != nil {
			if key.Matches(msg, m.keymap.ToggleLogs) || key.Matches(msg, m.keymap.Escape) {
				m.logOverlay = nil
				return m, nil
			}
			newOverlay, cmd := m.logOverlay.Update(msg)
			m.logOverlay = newOverlay
			return m, cmd
		}

		if m.modal != nil {
			newModal, cmd := m.modal.Update(msg)
			m.modal = newModal
			return m, cmd
		}

		switch {
		case key.Matches(msg, m.keymap.Quit), key.Matches(msg, m.keymap.ForceQuit):
			m.logger.Info("quit requested")
			return m, tea.Quit

		case key.Matches(msg, m.keymap.Tab):
			m = m.toggleOutputFocus()
			m.logger.Debug("focus changed", slog.String("panel", m.focus.String()))
			return m, nil

		case key.Matches(msg, m.keymap.ShiftTab):
			if m.focus != FocusServices {
				m = m.cycleFocusBackward()
				m.logger.Debug("focus changed", slog.String("panel", m.focus.String()))
			}
			return m, nil

		case key.Matches(msg, m.keymap.Help):
			m.modal = modal.NewHelpOverlay()
			return m, nil

		case key.Matches(msg, m.keymap.ToggleLogs):
			filterTask := ""
			if t := m.tasksPanel.SelectedTask(); t != nil {
				filterTask = t.ID
			}
			m.logOverlay = NewLogOverlay(m.logPath, m.width, m.height, filterTask)
			return m, logTickCmd()

		case key.Matches(msg, m.keymap.Refresh):
			cmds := []tea.Cmd{loadTasksCmd(m.mgr)}
			if m.initDialogPending {
				cmds = append(cmds, loadReposCmd(m.mgr))
			}
			if _, ok := m.modal.(*modal.InitDialog); ok {
				cmds = append(cmds, loadReposCmd(m.mgr))
			}
			return m, tea.Batch(cmds...)
		}

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
		if len(m.repos) > 0 {
			m.modal = modal.NewInitDialog(m.cfg.BranchPrefix, m.repos, m.width, m.height)
			return m, nil
		}
		m.initDialogPending = true
		return m, loadReposCmd(m.mgr)

	case panels.OpenAddServiceMsg:
		m.modal = modal.NewAddDialog(msg.TaskID, m.repos, m.width, m.height)
		return m, nil

	case panels.OpenRemoveDialogMsg:
		m.modal = modal.NewRemoveTaskDialog(msg.TaskID, 0, nil)
		return m, loadDirtyServicesCmd(m.mgr, msg.TaskID)

	case panels.CloneTaskMsg:
		m.modal = modal.NewCloneDialog(msg.SrcTaskID)
		return m, nil

	case panels.OpenConfigModalMsg:
		m.modal = modal.NewConfigModal(m.cfg)
		return m, nil

	case panels.GenerateSlnMsg:
		m.opRunning = true
		m.outputPanel.AppendLine("Generating .sln file...")
		return m, tea.Batch(generateSlnCmd(m.mgr, msg.TaskID), m.spinner.Tick)

	case panels.SyncTaskMsg:
		m.opRunning = true
		m.outputPanel.AppendLine("Syncing task " + msg.TaskID + " onto origin/" + m.cfg.BaseBranch + "...")
		return m, tea.Batch(syncTaskCmd(m.mgr, msg.TaskID), m.spinner.Tick)

	case panels.PushTaskMsg:
		m.opRunning = true
		m.outputPanel.AppendLine("Pushing task " + msg.TaskID + "...")
		return m, tea.Batch(pushTaskCmd(m.mgr, msg.TaskID), m.spinner.Tick)

	case panels.PushServiceMsg:
		m.opRunning = true
		m.outputPanel.AppendLine("Pushing " + msg.ServiceName + "...")
		return m, tea.Batch(pushServiceCmd(m.mgr, msg.TaskID, msg.ServiceName), m.spinner.Tick)

	case panels.StashServiceMsg:
		op := "Stashing"
		if msg.Pop {
			op = "Unstashing"
		}
		m.opRunning = true
		m.outputPanel.AppendLine(op + " " + msg.ServiceName + "...")
		return m, tea.Batch(stashServiceCmd(m.mgr, msg.TaskID, msg.ServiceName, msg.Pop), m.spinner.Tick)

	case panels.OpenRemoveServiceDialogMsg:
		m.modal = modal.NewRemoveServiceDialog(msg.TaskID, msg.ServiceName, msg.BranchName)
		return m, nil

	case modal.SubmitRemoveServiceMsg:
		m.modal = nil
		m.opRunning = true
		m.outputPanel.AppendLine("Removing service " + msg.ServiceName + "...")
		return m, tea.Batch(
			removeServiceCmd(m.mgr, msg.TaskID, msg.ServiceName, msg.RemoveBranch),
			m.spinner.Tick,
		)

	case panels.ShellExecMsg:
		m.shellInput = &shellInputState{taskDir: msg.TaskDir}
		return m, nil

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

	case modal.SubmitRemoveTaskMsg:
		m.modal = nil
		m.opRunning = true
		m.outputPanel.AppendLine("Removing task " + msg.TaskID + "...")
		return m, tea.Batch(
			removeTaskCmd(m.mgr, msg.TaskID, msg.Force, msg.DeleteBranches),
			m.spinner.Tick,
		)

	case modal.SubmitCloneMsg:
		m.modal = nil
		m.opRunning = true
		m.outputPanel.AppendLine("Cloning task " + msg.Dst + " from " + msg.Src + "...")
		return m, tea.Batch(cloneTaskCmd(m.mgr, msg.Src, msg.Dst), m.spinner.Tick)

	case TasksLoadedMsg:
		m.tasksPanel.SetTasks(msg.Tasks)
		return m, m.maybeLoadServicesCmd()

	case ServicesLoadedMsg:
		m.servicesPanel.SetServices(msg.TaskID, msg.Services)
		return m, nil

	case ReposLoadedMsg:
		if msg.Err != nil {
			m.logger.ErrorContext(context.Background(), "failed to discover repos",
				slog.String("error", msg.Err.Error()))
			m.outputPanel.AppendLine("Error: could not discover repos: " + msg.Err.Error())
			m.initDialogPending = false
			return m, nil
		}
		m.repos = msg.Repos
		if m.initDialogPending {
			m.initDialogPending = false
			m.modal = modal.NewInitDialog(m.cfg.BranchPrefix, msg.Repos, m.width, m.height)
		}
		return m, nil

	case DirtyServicesLoadedMsg:
		if m.modal != nil {
			if rd, ok := m.modal.(*modal.RemoveTaskDialog); ok {
				rd.UpdateInfo(msg.ServiceCount, msg.DirtyServices)
				m.modal = rd
			}
		}
		return m, nil

	case OutputLineMsg:
		m.outputPanel.AppendLine(msg.Line)
		return m, msg.Next

	case CommandDoneMsg:
		m.opRunning = false
		if msg.Err != nil {
			m.outputPanel.AppendLine("Error: " + msg.Err.Error())
			m.logger.Error("command failed", slog.String("err", msg.Err.Error()))
		} else {
			m.outputPanel.AppendLine("Done.")
		}

		return m, tea.Batch(loadTasksCmd(m.mgr), m.maybeLoadServicesCmd())

	case channelDrainedMsg:
		// The status channel was closed. No action needed — the authoritative
		// CommandDoneMsg from the main operation goroutine handles completion.
		return m, nil

	case spinner.TickMsg:
		if m.opRunning {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	return m, nil
}

func (m Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	header := renderHeader(m)
	footer := renderFooter(m)

	leftView := m.tasksPanel.View()
	rightView := m.servicesPanel.View()
	mainRow := lipgloss.JoinHorizontal(lipgloss.Top, leftView, rightView)
	outputView := m.outputPanel.View()
	fullView := lipgloss.JoinVertical(lipgloss.Left,
		header,
		mainRow,
		outputView,
		footer,
	)

	if m.logOverlay != nil {
		return m.logOverlay.View()
	}

	if m.modal != nil {
		maxContentH := max(m.height*70/100, 10)
		return modal.OverlayView(m.modal.View(), m.width, m.height, maxContentH)
	}

	return fullView
}
func (m *Model) recalculateDimensions() {
	const headerHeight = 1
	const footerHeight = 1

	outputPanelHeight := m.cfg.OutputPanelLines + 2
	mainPanelHeight := max(m.height-headerHeight-footerHeight-outputPanelHeight, 3)
	tasksWidth := max(m.width*30/100, 25)

	servicesWidth := max(m.width-tasksWidth, 1)

	m.tasksPanel.SetSize(tasksWidth, mainPanelHeight)
	m.servicesPanel.SetSize(servicesWidth, mainPanelHeight)
	m.outputPanel.SetSize(m.width, m.cfg.OutputPanelLines)
}

func (m Model) cycleFocusForward() Model {
	m.focus = m.focus.Next()
	m.tasksPanel.SetFocused(m.focus == FocusTasks)
	m.servicesPanel.SetFocused(m.focus == FocusServices)
	m.outputPanel.SetFocused(m.focus == FocusOutput)
	return m
}

func (m Model) cycleFocusBackward() Model {
	m.focus = m.focus.Prev()
	m.tasksPanel.SetFocused(m.focus == FocusTasks)
	m.servicesPanel.SetFocused(m.focus == FocusServices)
	m.outputPanel.SetFocused(m.focus == FocusOutput)
	return m
}

// toggleOutputFocus toggles between Output panel and the previously focused panel.
// If currently on Output, it restores the previous focus; otherwise, it saves
// the current focus and switches to Output.
func (m Model) toggleOutputFocus() Model {
	if m.focus == FocusOutput {
		// Restore previous focus
		m.focus = m.previousFocus
	} else {
		// Save current focus and switch to Output
		m.previousFocus = m.focus
		m.focus = FocusOutput
	}
	m.tasksPanel.SetFocused(m.focus == FocusTasks)
	m.servicesPanel.SetFocused(m.focus == FocusServices)
	m.outputPanel.SetFocused(m.focus == FocusOutput)
	return m
}

// moveFocusLeft moves focus from Services to Tasks.
// If already on Tasks, focus stays on Tasks.
func (m Model) moveFocusLeft() Model {
	if m.focus == FocusServices {
		m.focus = FocusTasks
		m.tasksPanel.SetFocused(true)
		m.servicesPanel.SetFocused(false)
		m.outputPanel.SetFocused(false)
	}
	return m
}

// moveFocusRight moves focus from Tasks to Services.
// If already on Services, focus stays on Services.
func (m Model) moveFocusRight() Model {
	if m.focus == FocusTasks {
		m.focus = FocusServices
		m.tasksPanel.SetFocused(false)
		m.servicesPanel.SetFocused(true)
		m.outputPanel.SetFocused(false)
	}
	return m
}

func (m Model) maybeLoadServicesCmd() tea.Cmd {
	t := m.tasksPanel.SelectedTask()
	if t == nil {
		return nil
	}
	return loadServicesCmd(m.mgr, t.ID)
}

func errorf(msg string) error {
	return &modelError{msg: msg}
}

type modelError struct{ msg string }

func (e *modelError) Error() string { return e.msg }

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
