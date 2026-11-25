package tui

import (
	"context"
	"errors"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type operationCancellation struct {
	cancel     context.CancelFunc
	label      string
	generation uint64
	confirm    bool
	requested  bool
}

func (m *Model) startOperation(label string, cancellable bool) context.Context {
	m.opRunning = true
	m.operationGeneration++
	base := context.Background()
	if !cancellable {
		return base
	}
	ctx, cancel := context.WithCancel(base)
	m.operationCancellation = operationCancellation{cancel: cancel, label: label, generation: m.operationGeneration}
	return ctx
}

func (m *Model) handleOperationCancelKey(msg tea.KeyMsg) bool {
	op := &m.operationCancellation
	if !m.opRunning {
		return false
	}
	escape := key.Matches(msg, m.keymap.Escape)
	if op.cancel == nil || op.generation != m.operationGeneration {
		if escape {
			m.outputPanel.AppendLine("This operation cannot be interrupted safely. Wait for it to finish.")
		}
		return escape
	}
	if op.requested {
		return escape
	}
	if !op.confirm {
		op.confirm = escape
		return escape
	}
	switch msg.String() {
	case "y", "Y":
		op.confirm = false
		op.requested = true
		op.cancel()
	case "n", "N", "enter":
		op.confirm = false
	default:
		if escape {
			op.confirm = false
		}
	}
	return true
}

func (m *Model) finishOperationCancellation(err error) (requested, cancelled bool) {
	op := m.operationCancellation
	if op.cancel == nil || op.generation != m.operationGeneration {
		return false, false
	}
	m.operationCancellation = operationCancellation{}
	op.cancel()
	return op.requested, op.requested && errors.Is(err, context.Canceled)
}

func (m Model) operationCancelHint() string {
	op := m.operationCancellation
	if !m.opRunning || op.cancel == nil || op.generation != m.operationGeneration {
		return ""
	}
	switch {
	case op.confirm:
		return "Cancel " + op.label + "? [y/N]"
	case op.requested:
		return "Cancelling " + op.label + "…"
	default:
		return ""
	}
}

func (m Model) releaseCancellationSafe(release domain.Release, event *string) bool {
	if m.flow == nil || m.flow.Release == nil || task.IsLegacyManifest(release) {
		return false
	}
	machine := m.flow.Release.Machine
	state, ok := machine.State(string(release.Status))
	if !ok {
		state, ok = machine.State(release.State)
	}
	if !ok {
		return false
	}
	if event == nil {
		// Unprepared releases can take a different retry path through integration.
		return release.PreparedAt != nil && state.Kind == gitflow.StateRun && machine.CancellablePath(state.ID)
	}
	if state.Kind != gitflow.StateWait {
		return false
	}
	for _, candidate := range state.Events {
		if candidate.Name == *event {
			return candidate.Action == "" && machine.CancellablePath(candidate.To)
		}
	}
	return *event == "" && state.Wait != gitflow.WaitManual && machine.CancellablePath(state.Next)
}
