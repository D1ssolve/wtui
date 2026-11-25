package tui

import (
	"context"
	"errors"
	"os/exec"

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
	if !m.opRunning || op.cancel == nil || op.generation != m.operationGeneration || op.requested {
		return false
	}
	escape := key.Matches(msg, m.keymap.Escape)
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
	if !op.requested || err == nil {
		return op.requested, false
	}
	// The workflow hook runner can return the killed child instead of ctx.Err().
	var exitErr *exec.ExitError
	interrupted := errors.As(err, &exitErr) && exitErr.ExitCode() == -1
	return true, errors.Is(err, context.Canceled) || interrupted
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
		return release.PreparedAt != nil && state.Kind == gitflow.StateRun && hookPath(machine, state.ID)
	}
	if state.Kind != gitflow.StateWait {
		return false
	}
	for _, candidate := range state.Events {
		if candidate.Name == *event {
			return candidate.Action == "" && hookPath(machine, candidate.To)
		}
	}
	return *event == "" && state.Wait != gitflow.WaitManual && hookPath(machine, state.Next)
}

// Only hooks and wait checks are interruptible. Never interrupt built-in Git
// mutations (notably a push), review merges, or pipeline triggers; an external
// write can succeed even if the local command does not receive its response.
func hookPath(machine *gitflow.Machine, start string) bool {
	seen := make(map[string]bool)
	queue := []string{start}
	for len(queue) > 0 {
		id := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		state, ok := machine.State(id)
		if !ok {
			return false
		}
		switch state.Kind {
		case gitflow.StateRun:
			for _, step := range state.Run {
				if step.Kind != gitflow.StepHook {
					return false
				}
			}
			queue = append(queue, state.Next, state.OnError)
		case gitflow.StateWait:
			if state.Wait != gitflow.WaitManual {
				queue = append(queue, state.Next)
			}
		case gitflow.StateFinal:
		default:
			return false
		}
	}
	return true
}

func (m Model) taskCancellationSafe() bool {
	if m.flow == nil {
		return false
	}
	found := false
	for _, rule := range m.flow.BranchTypes {
		if rule.Machine == nil {
			continue
		}
		found = true
		for _, step := range rule.Machine.Steps() {
			if step.Kind != gitflow.StepHook {
				return false
			}
		}
	}
	return found
}
