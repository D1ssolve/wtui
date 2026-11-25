package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/task"
)

const waitRefreshInterval = 30 * time.Second
const waitRefreshTimeout = 10 * time.Second

type waitRefreshTarget struct {
	id, state string
	release   bool
	operation uint64
	selection uint64
}

type waitRefresh struct {
	target          waitRefreshTarget
	scope           uint64
	sequence        uint64
	timer, inFlight bool
	cancel          context.CancelFunc
	parent          context.Context
	closing         bool
	lastLine        string
}

type waitRefreshTickMsg uint64

type waitRefreshResultMsg struct {
	sequence, scope uint64
	observation     task.WaitObservation
	err             error
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	observer, capable := m.mgr.(task.WaitObserver)
	if !capable {
		return m.update(msg)
	}
	switch msg := msg.(type) {
	case waitRefreshTickMsg:
		return m.refreshWait(observer, msg)
	case waitRefreshResultMsg:
		if !m.waitRefresh.inFlight || msg.sequence != m.waitRefresh.sequence {
			return m, nil
		}
		m.waitRefresh.inFlight = false
		m.waitRefresh.cancel = nil
		if m.waitRefresh.target.id != "" && msg.scope == m.waitRefresh.scope {
			m.reportWaitRefresh(msg)
		}
		return m, m.scheduleWaitRefresh()
	default:
		next, cmd := m.update(msg)
		target := next.selectedWait()
		if target != m.waitRefresh.target {
			if cancel := next.waitRefresh.cancel; cancel != nil {
				cancel()
			}
			next.waitRefresh.target = target
			next.waitRefresh.scope++
		}
		return next, tea.Batch(cmd, next.scheduleWaitRefresh())
	}
}

func (m *Model) scheduleWaitRefresh() tea.Cmd {
	if m.waitRefresh.target.id == "" || m.waitRefresh.timer || m.waitRefresh.inFlight {
		return nil
	}
	m.waitRefresh.timer = true
	m.waitRefresh.sequence++
	tick := waitRefreshTickMsg(m.waitRefresh.sequence)
	return tea.Tick(waitRefreshInterval, func(time.Time) tea.Msg { return tick })
}

func (m Model) refreshWait(observer task.WaitObserver, tick waitRefreshTickMsg) (Model, tea.Cmd) {
	refresh := &m.waitRefresh
	if !refresh.timer || uint64(tick) != refresh.sequence {
		return m, nil
	}
	refresh.timer = false
	target, scope := refresh.target, refresh.scope
	if target.id == "" {
		return m, nil
	}
	var selected domain.Task
	if !target.release {
		selected = *m.tasksPanel.SelectedTask()
		selected.Services = append([]domain.Service(nil), selected.Services...)
	}
	refresh.inFlight = true
	ctx, cancel := context.WithTimeout(refresh.parent, waitRefreshTimeout)
	refresh.cancel = cancel
	return m, func() tea.Msg {
		defer cancel()
		result := waitRefreshResultMsg{sequence: uint64(tick), scope: scope}
		if result.err = ctx.Err(); result.err != nil {
			return result
		}
		if target.release {
			result.observation, result.err = observer.ObserveReleaseWait(ctx, target.id, target.state)
		} else {
			result.observation, result.err = observer.ObserveTaskWait(ctx, selected, target.state)
		}
		return result
	}
}

func (m Model) selectedWait() waitRefreshTarget {
	if m.waitRefresh.closing || m.releaseMutationBlocked() || m.releaseCleanupWorktreeLocked() || m.conversionRunning || m.refreshing ||
		m.modal != nil || m.shellInput != nil || m.logOverlay != nil || m.pipelineView != nil {
		return waitRefreshTarget{}
	}
	switch m.focus {
	case FocusReleases:
		if release := m.releasesPanel.SelectedRelease(); release != nil {
			if state := task.ObservableReleaseWait(*release, m.flow); state != "" {
				return waitRefreshTarget{id: release.ID, state: state, release: true, operation: m.operationGeneration}
			}
		}
	case FocusTasks:
		if selected := m.tasksPanel.SelectedTask(); selected != nil && m.servicesPanel.TaskID() == selected.ID &&
			len(selected.Services) > 0 && m.taskWorkflow != nil && m.taskWorkflow.Gated && !m.taskWorkflowPending {
			if state := task.ObservableTaskWait(m.flow, selected.Phase, string(m.taskWorkflow.Current)); state != "" {
				return waitRefreshTarget{id: selected.ID, state: state, operation: m.operationGeneration, selection: m.taskWorkflowGeneration}
			}
		}
	case FocusServices, FocusOutput:
		// The wait's manual key is handled in its owning panel.
	}
	return waitRefreshTarget{}
}

func (m *Model) reportWaitRefresh(result waitRefreshResultMsg) {
	target := m.waitRefresh.target
	kind := "Task"
	if target.release {
		kind = "Release"
	}
	status := "still waiting."
	switch {
	case result.err != nil:
		status = "could not refresh wait status; check workflow and repository access."
	case result.observation.Ready && result.observation.Key == "M":
		status = "forge reports merged reviews; press M to reconcile."
	case result.observation.Ready && result.observation.Key != "":
		status = "wait condition met; press " + result.observation.Key + " to continue."
	case result.observation.Ready:
		status = "wait condition met; no automatic key suggestion for this workflow."
	}
	line := fmt.Sprintf("%s %s (%s): %s", kind, target.id, target.state, status)
	if line != m.waitRefresh.lastLine {
		m.waitRefresh.lastLine = line
		m.outputPanel.AppendLine(line)
	}
}
