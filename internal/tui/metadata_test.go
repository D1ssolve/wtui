package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	tea "github.com/charmbracelet/bubbletea"
)

type metadataGeneratorFunc func(context.Context, task.MetadataRequest) (ai.Draft, error)

func (f metadataGeneratorFunc) Generate(ctx context.Context, req task.MetadataRequest) (ai.Draft, error) {
	return f(ctx, req)
}

func metadataModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t, &mockManager{})
	d := modal.NewForgeMenuModal("api", forge.ForgeProviderGitLab, 120, 40)
	d.SetTaskID("T-1")
	d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.modal = d
	return m
}

func TestMetadata_GenerateProducesEditableSubmit(t *testing.T) {
	m := metadataModel(t)
	m.MetadataGenerator = metadataGeneratorFunc(func(ctx context.Context, req task.MetadataRequest) (ai.Draft, error) {
		if req.Purpose != ai.PurposeMR || len(req.TaskIDs) != 1 || req.TaskIDs[0] != "T-1" || req.Title != "T-1" {
			t.Fatalf("request = %#v", req)
		}
		return ai.Draft{Title: "AI title", Description: "AI body"}, nil
	})
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("Ctrl+G did not generate")
	}
	reply := cmd()
	updated, _ = m.Update(reply)
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" accepted")})
	m = updated.(Model)
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	submit := cmd().(modal.ForgeCreateMRMsg)
	if submit.Title != "AI title accepted" || submit.Description != "AI body" {
		t.Fatalf("submit = %#v", submit)
	}
}

func TestMetadata_CancelPreservesDraftAndDiscardsLateReply(t *testing.T) {
	m := metadataModel(t)
	started := make(chan struct{})
	m.MetadataGenerator = metadataGeneratorFunc(func(ctx context.Context, req task.MetadataRequest) (ai.Draft, error) {
		close(started)
		<-ctx.Done()
		return ai.Draft{Title: "Late title", Description: "Late body"}, nil
	})
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("Ctrl+G did not generate")
	}
	reply := make(chan tea.Msg, 1)
	go func() { reply <- cmd() }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("generation did not start")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("blocked")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	var late tea.Msg
	select {
	case late = <-reply:
	case <-time.After(2 * time.Second):
		t.Fatal("Escape did not cancel the running generator")
	}
	updated, _ = m.Update(late)
	m = updated.(Model)
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	submit := cmd().(modal.ForgeCreateMRMsg)
	if submit.Title != "T-1" || submit.Description != "" {
		t.Fatalf("draft overwritten: %#v", submit)
	}
}

func TestMetadata_NavigationDiscardsResponse(t *testing.T) {
	m := metadataModel(t)
	m.MetadataGenerator = metadataGeneratorFunc(func(context.Context, task.MetadataRequest) (ai.Draft, error) {
		return ai.Draft{Title: "Old title", Description: "Old body"}, nil
	})
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("Ctrl+G did not generate")
	}
	reply := cmd()
	replacement := modal.NewForgeMenuModal("api", forge.ForgeProviderGitLab, 120, 40)
	replacement.SetTaskID("T-2")
	replacement.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.modal = replacement
	updated, _ = m.Update(reply)
	m = updated.(Model)
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := cmd().(modal.ForgeCreateMRMsg); got.Title != "T-2" {
		t.Fatalf("stale reply applied: %#v", got)
	}
}

func TestMetadata_ErrorKeepsManualEditing(t *testing.T) {
	m := metadataModel(t)
	m.MetadataGenerator = metadataGeneratorFunc(func(context.Context, task.MetadataRequest) (ai.Draft, error) {
		return ai.Draft{}, errors.New("Sign in to configured provider")
	})
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("Ctrl+G did not generate")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if !strings.Contains(m.modal.View(), "Sign in") {
		t.Fatal("error missing from dialog")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" manual")})
	m = updated.(Model)
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := cmd().(modal.ForgeCreateMRMsg); got.Title != "T-1 manual" {
		t.Fatalf("manual editing lost: %#v", got)
	}
}

func TestMetadata_GeneratingInsideEditorShowsAvailableControls(t *testing.T) {
	for _, kind := range []string{"mr", "release", "close", "hotfix", "hotfix-locked"} {
		t.Run(kind, func(t *testing.T) {
			m := captureMetadataModel(t, 120, 40, kind)
			m.MetadataGenerator = metadataGeneratorFunc(func(ctx context.Context, _ task.MetadataRequest) (ai.Draft, error) {
				if !errors.Is(ctx.Err(), context.Canceled) {
					t.Fatal("Escape did not cancel generation")
				}
				return ai.Draft{}, ctx.Err()
			})
			next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
			m = next.(Model)
			before, _ := m.modal.(metadataModal).MetadataRequest()
			next, generate := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
			m = next.(Model)
			if generate == nil || m.metadataRequest == nil {
				t.Fatal("generation did not start inside the editor")
			}
			view := m.modal.View()
			for _, blocked := range []string{"Tab: field", "Ctrl+S: apply", "Ctrl+U: clear", "Esc: back, keep draft"} {
				if strings.Contains(view, blocked) {
					t.Errorf("loading editor advertises blocked control %q", blocked)
				}
			}
			if !strings.Contains(view, "Generating with AI... Esc cancels") {
				t.Error("loading editor does not explain Escape")
			}
			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyTab}, {Type: tea.KeyCtrlS}, {Type: tea.KeyCtrlU},
				{Type: tea.KeyRunes, Runes: []rune("blocked")},
			} {
				next, _ = m.Update(key)
				m = next.(Model)
			}
			next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
			m = next.(Model)
			if m.metadataRequest != nil || !strings.Contains(m.modal.View(), "Tab: field") {
				t.Fatal("cancel did not restore the editor")
			}
			after, _ := m.modal.(metadataModal).MetadataRequest()
			if before.Title != after.Title || before.Description != after.Description {
				t.Fatalf("blocked inputs changed the draft: before %#v; after %#v", before, after)
			}
			m.Update(generate())
		})
	}
}
