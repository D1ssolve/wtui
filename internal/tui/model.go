package tui

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
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
	previousFocus FocusPanel

	width  int
	height int

	ready bool

	tasksPanel    panels.TasksPanel
	servicesPanel panels.ServicesPanel
	outputPanel   panels.OutputPanel

	repos []domain.Repo

	modal modal.Modal

	logOverlay *LogOverlay
	logPath    string

	spinner   spinner.Model
	opRunning bool

	keymap KeyMap

	styles Styles

	initDialogPending bool
	addDialogPending  *panels.OpenAddServiceMsg

	pendingInitParams *task.InitParams

	pendingAddParams *task.AddParams
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
		loadReposCmd(m.mgr, false),
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

		if key.Matches(msg, m.keymap.ForceQuit) {
			m.logger.Info("quit requested")
			return m, tea.Quit
		}
		switch m.focus {
		case FocusTasks:
			if m.tasksPanel.FilterActive() {
				newPanel, cmd := m.tasksPanel.Update(msg)
				m.tasksPanel = newPanel
				return m, cmd
			}
		case FocusServices:
			if m.servicesPanel.FilterActive() {
				newPanel, cmd := m.servicesPanel.Update(msg)
				m.servicesPanel = newPanel
				return m, cmd
			}
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
			m.outputPanel.AppendLine("Refreshing tasks and repository cache...")
			cmds := []tea.Cmd{loadTasksCmd(m.mgr), loadReposCmd(m.mgr, true)}
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
		m.outputPanel.AppendLine("Loading repository cache for init dialog...")
		m.initDialogPending = true
		return m, loadReposCmd(m.mgr, false)

	case panels.OpenAddServiceMsg:
		if len(m.repos) == 0 {
			pending := msg
			m.addDialogPending = &pending
			m.outputPanel.AppendLine("Loading repository cache for add service dialog...")
			return m, loadReposCmd(m.mgr, false)
		}
		m.modal = modal.NewAddDialog(msg.TaskID, m.repos, msg.ExistingServices, m.width, m.height)
		return m, nil

	case panels.OpenRemoveDialogMsg:
		m.modal = modal.NewRemoveTaskDialog(msg.TaskID, 0, nil)
		return m, loadDirtyServicesCmd(m.mgr, msg.TaskID)

	case panels.OpenSyncStrategyDialogMsg:
		m.modal = modal.NewSyncStrategyDialog(msg.TaskID)
		return m, nil

	case panels.OpenSyncServiceStrategyDialogMsg:
		m.modal = modal.NewSyncServiceStrategyDialog(msg.TaskID, msg.ServiceName)
		return m, nil

	case panels.OpenConfigModalMsg:
		m.modal = modal.NewConfigModal(m.cfg)
		return m, nil

	case panels.PushTaskMsg:
		m.opRunning = true
		m.outputPanel.AppendLine("Pushing task " + msg.TaskID + "...")
		return m, tea.Batch(pushTaskCmd(m.mgr, msg.TaskID), m.spinner.Tick)

	case panels.PushServiceMsg:
		m.opRunning = true
		m.outputPanel.AppendLine("Pushing service " + msg.ServiceName + " for task " + msg.TaskID + "...")
		return m, tea.Batch(pushServiceCmd(m.mgr, msg.TaskID, msg.ServiceName), m.spinner.Tick)

	case panels.StashServiceMsg:
		op := "Stashing"
		if msg.Pop {
			op = "Unstashing"
		}
		m.opRunning = true
		m.outputPanel.AppendLine(op + " service " + msg.ServiceName + " for task " + msg.TaskID + "...")
		return m, tea.Batch(stashServiceCmd(m.mgr, msg.TaskID, msg.ServiceName, msg.Pop, false), m.spinner.Tick)

	case panels.OpenStashDialogMsg:
		m.modal = modal.NewStashDialog(msg.TaskID, msg.ServiceName, msg.Pop)
		return m, nil

	case modal.SubmitStashMsg:
		m.modal = nil
		op := "Stashing"
		if msg.Pop {
			op = "Unstashing"
		}
		untracked := ""
		if msg.IncludeUntracked {
			untracked = " (including untracked)"
		}
		m.opRunning = true
		m.outputPanel.AppendLine(op + " service " + msg.ServiceName + " for task " + msg.TaskID + untracked + "...")
		return m, tea.Batch(stashServiceCmd(m.mgr, msg.TaskID, msg.ServiceName, msg.Pop, msg.IncludeUntracked), m.spinner.Tick)

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

	case modal.SubmitSyncStrategyMsg:
		m.modal = nil
		if msg.Strategy == task.SyncStrategyNoop {
			m.outputPanel.AppendLine("Sync cancelled for task " + msg.TaskID + ".")
			return m, nil
		}
		m.opRunning = true
		m.outputPanel.AppendLine("Syncing task " + msg.TaskID + " with " + msg.Strategy.String() + " strategy...")
		return m, tea.Batch(syncTaskCmd(m.mgr, msg.TaskID, msg.Strategy), m.spinner.Tick)

	case modal.SubmitSyncServiceStrategyMsg:
		m.modal = nil
		if msg.Strategy == task.SyncStrategyNoop {
			m.outputPanel.AppendLine("Sync cancelled for service " + msg.ServiceName + ".")
			return m, nil
		}
		m.opRunning = true
		m.outputPanel.AppendLine("Syncing service " + msg.ServiceName + " with " + msg.Strategy.String() + " strategy...")
		return m, tea.Batch(syncServiceCmd(m.mgr, msg.TaskID, msg.ServiceName, msg.Strategy), m.spinner.Tick)

	case panels.RiderTaskMsg:
		m.opRunning = true
		m.outputPanel.AppendLine("Opening " + msg.TaskID + ".sln in Rider from " + msg.TaskDir + "...")
		return m, tea.Batch(riderTaskCmd(msg.TaskID, msg.TaskDir), m.spinner.Tick)

	case panels.CodeWorkspaceTaskMsg:
		m.opRunning = true
		m.outputPanel.AppendLine("Opening " + msg.TaskID + ".code-workspace in " + m.cfg.Editor + " from " + msg.TaskDir + "...")
		return m, tea.Batch(codeWorkspaceTaskCmd(m.cfg.Editor, msg.TaskID, msg.TaskDir), m.spinner.Tick)

	case modal.CloseModalMsg:
		m.modal = nil

		m.pendingInitParams = nil
		m.pendingAddParams = nil
		return m, nil

	case modal.RemoteBranchConflictMsg:

		m.modal = modal.NewRemoteBranchConflictDialog(
			msg.TaskID,
			msg.ServiceName,
			msg.BranchName,
			msg.RepoPath,
		)
		return m, nil

	case modal.SubmitRemoteBranchStrategyMsg:
		m.modal = nil

		if msg.Strategy == task.StrategyCancel {
			m.outputPanel.AppendLine("Skipped service " + msg.ServiceName + " (cancelled by user)")
			m.pendingInitParams = nil
			m.pendingAddParams = nil
			return m, nil
		}

		if m.pendingInitParams != nil {

			if m.pendingInitParams.RemoteBranchStrategies == nil {
				m.pendingInitParams.RemoteBranchStrategies = make(map[string]task.RemoteBranchStrategy)
			}
			if m.pendingInitParams.BranchSuffixes == nil {
				m.pendingInitParams.BranchSuffixes = make(map[string]string)
			}
			m.pendingInitParams.RemoteBranchStrategies[msg.ServiceName] = msg.Strategy
			if msg.Strategy == task.StrategyNewBranch {
				m.pendingInitParams.BranchSuffixes[msg.ServiceName] = msg.BranchSuffix
			}

			m.outputPanel.AppendLine("Retrying with " + msg.Strategy.String() + " strategy for " + msg.ServiceName + "...")
			return m, tea.Batch(
				initTaskCmd(m.mgr, *m.pendingInitParams),
				m.spinner.Tick,
			)
		}

		if m.pendingAddParams != nil {

			if m.pendingAddParams.RemoteBranchStrategies == nil {
				m.pendingAddParams.RemoteBranchStrategies = make(map[string]task.RemoteBranchStrategy)
			}
			if m.pendingAddParams.BranchSuffixes == nil {
				m.pendingAddParams.BranchSuffixes = make(map[string]string)
			}
			m.pendingAddParams.RemoteBranchStrategies[msg.ServiceName] = msg.Strategy
			if msg.Strategy == task.StrategyNewBranch {
				m.pendingAddParams.BranchSuffixes[msg.ServiceName] = msg.BranchSuffix
			}

			m.outputPanel.AppendLine("Retrying with " + msg.Strategy.String() + " strategy for " + msg.ServiceName + "...")
			return m, tea.Batch(
				addServiceCmd(m.mgr, *m.pendingAddParams),
				m.spinner.Tick,
			)
		}

		m.logger.Error("SubmitRemoteBranchStrategyMsg received but no pending params")
		return m, nil

	case modal.SubmitInitMsg:
		m.modal = nil
		m.opRunning = true
		m.outputPanel.AppendLine("Initializing task " + msg.TaskID + "...")
		m.pendingInitParams = &task.InitParams{
			TaskID:       msg.TaskID,
			Services:     msg.Services,
			BranchPrefix: msg.BranchPrefix,
			BaseBranch:   msg.BaseBranch,
		}
		m.pendingAddParams = nil
		return m, tea.Batch(
			initTaskCmd(m.mgr, *m.pendingInitParams),
			m.spinner.Tick,
		)

	case modal.SubmitAddMsg:
		m.modal = nil
		m.opRunning = true
		m.outputPanel.AppendLine("Adding services to " + msg.TaskID + "...")
		m.pendingAddParams = &task.AddParams{
			TaskID:   msg.TaskID,
			Services: msg.Services,
		}
		m.pendingInitParams = nil
		return m, tea.Batch(
			addServiceCmd(m.mgr, *m.pendingAddParams),
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
			m.addDialogPending = nil
			return m, nil
		}
		m.repos = msg.Repos
		if m.initDialogPending {
			m.initDialogPending = false
			m.modal = modal.NewInitDialog(m.cfg.BranchPrefix, msg.Repos, m.width, m.height)
		}
		if m.addDialogPending != nil {
			pending := *m.addDialogPending
			m.addDialogPending = nil
			m.modal = modal.NewAddDialog(pending.TaskID, msg.Repos, pending.ExistingServices, m.width, m.height)
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

			var conflictErr *task.ErrRemoteBranchConflict
			if errors.As(msg.Err, &conflictErr) {

				m.outputPanel.AppendLine("Remote branch conflict detected for " + conflictErr.ServiceName + "...")
				return m, func() tea.Msg {
					return modal.RemoteBranchConflictMsg{
						TaskID:      conflictErr.TaskID,
						ServiceName: conflictErr.ServiceName,
						BranchName:  conflictErr.BranchName,
						RepoPath:    conflictErr.RepoPath,
					}
				}
			}

			m.outputPanel.AppendLine("Error: " + msg.Err.Error())
			m.logger.Error("command failed", slog.String("err", msg.Err.Error()))

			m.pendingInitParams = nil
			m.pendingAddParams = nil
		} else {
			m.outputPanel.AppendLine("Done.")

			m.pendingInitParams = nil
			m.pendingAddParams = nil
		}

		return m, tea.Batch(loadTasksCmd(m.mgr), m.maybeLoadServicesCmd())

	case channelDrainedMsg:

		return m, nil

	case spinner.TickMsg:
		if m.opRunning {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case list.FilterMatchesMsg:

		if m.modal != nil {
			newModal, cmd := m.modal.Update(msg)
			m.modal = newModal
			return m, cmd
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

func (m Model) toggleOutputFocus() Model {
	if m.focus == FocusOutput {

		m.focus = m.previousFocus
	} else {

		m.previousFocus = m.focus
		m.focus = FocusOutput
	}
	m.tasksPanel.SetFocused(m.focus == FocusTasks)
	m.servicesPanel.SetFocused(m.focus == FocusServices)
	m.outputPanel.SetFocused(m.focus == FocusOutput)
	return m
}

func (m Model) moveFocusLeft() Model {
	if m.focus == FocusServices {
		m.focus = FocusTasks
		m.tasksPanel.SetFocused(true)
		m.servicesPanel.SetFocused(false)
		m.outputPanel.SetFocused(false)
	}
	return m
}

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
