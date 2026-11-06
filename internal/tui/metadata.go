package tui

import (
	"context"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	tea "github.com/charmbracelet/bubbletea"
)

type metadataModal interface {
	modal.Modal
	MetadataRequest() (task.MetadataRequest, bool)
	ApplyMetadataDraft(ai.Draft)
	SetMetadataStatus(bool, string)
}

type metadataRequest struct {
	origin     metadataModal
	generation uint64
	cancel     context.CancelFunc
}

type metadataGeneratedMsg struct {
	origin     metadataModal
	generation uint64
	draft      ai.Draft
	err        error
}

func (m *Model) handleMetadata(msg tea.Msg) (bool, tea.Cmd) {
	if _, closing := msg.(modal.CloseModalMsg); closing && m.metadataRequest != nil {
		m.metadataRequest.cancel()
		m.metadataRequest.origin.SetMetadataStatus(false, "")
		m.metadataRequest = nil
	}
	if pending := m.metadataRequest; pending != nil && m.modal != pending.origin {
		pending.cancel()
		pending.origin.SetMetadataStatus(false, "")
		m.metadataRequest = nil
	}
	if result, ok := msg.(metadataGeneratedMsg); ok {
		pending := m.metadataRequest
		if pending == nil || result.origin != pending.origin || result.generation != pending.generation {
			return true, nil
		}
		pending.cancel()
		m.metadataRequest = nil
		if result.err != nil {
			pending.origin.SetMetadataStatus(false, result.err.Error())
			return true, nil
		}
		pending.origin.ApplyMetadataDraft(result.draft)
		pending.origin.SetMetadataStatus(false, "")
		return true, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	if pending := m.metadataRequest; pending != nil {
		if key.String() == "esc" {
			pending.cancel()
			pending.origin.SetMetadataStatus(false, "")
			m.metadataRequest = nil
		}
		return true, nil
	}
	if key.String() != "ctrl+g" {
		return false, nil
	}
	dialog, ok := m.modal.(metadataModal)
	if !ok || m.opRunning {
		return false, nil
	}
	request, ready := dialog.MetadataRequest()
	if !ready {
		return true, nil
	}
	if m.MetadataGenerator == nil {
		dialog.SetMetadataStatus(false, "No AI provider configured; edit manually.")
		return true, nil
	}
	m.metadataGeneration++
	generation := m.metadataGeneration
	ctx, cancel := context.WithCancel(context.Background())
	m.metadataRequest = &metadataRequest{origin: dialog, generation: generation, cancel: cancel}
	dialog.SetMetadataStatus(true, "")
	generator := m.MetadataGenerator
	return true, func() tea.Msg {
		defer cancel()
		draft, err := generator.Generate(ctx, request)
		return metadataGeneratedMsg{origin: dialog, generation: generation, draft: draft, err: err}
	}
}
