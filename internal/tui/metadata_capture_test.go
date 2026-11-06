package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	"github.com/D1ssolve/wtui/internal/tui/panels"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestCaptureMetadataFullViews(t *testing.T) {
	dir := os.Getenv("WTUI_CAPTURE_DIR")
	if dir == "" {
		t.Skip("WTUI_CAPTURE_DIR not set")
	}
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
	for _, width := range []int{80, 120} {
		for _, kind := range []string{"mr", "release", "close", "hotfix", "hotfix-locked"} {
			for _, state := range []string{"editing", "editing-loading", "loading", "error", "populated", "discard"} {
				name := fmt.Sprintf("metadata-%s-%s-%d", kind, state, width)
				t.Run(name, func(t *testing.T) {
					m := captureMetadataModel(t, width, 40, kind)
					dialog := m.modal.(metadataModal)
					switch state {
					case "editing":
						m.modal.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
					case "editing-loading":
						m.modal.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
						dialog.SetMetadataStatus(true, "")
					case "loading":
						dialog.SetMetadataStatus(true, "")
					case "error":
						dialog.SetMetadataStatus(false, errors.New("Authentication required. Run the provider subscription login command, then generate again. Your typed text is preserved.").Error())
					case "populated", "discard":
						dialog.ApplyMetadataDraft(ai.Draft{Title: "Accepted title", Description: "Accepted body", Services: []ai.ServiceDraft{{Name: "api", Version: "2.0.0", Bump: ai.BumpMajor, Reason: "Breaking API change", TagDescription: "Accepted tag"}}})
						if state == "discard" {
							if kind == "mr" || kind == "release" {
								m.modal.Update(tea.KeyMsg{Type: tea.KeyEsc})
							}
							m.modal.Update(tea.KeyMsg{Type: tea.KeyEsc})
						}
					}
					raw := m.View()
					plain := stripANSIForModel(raw)
					contains := []string{"Ctrl+G"}
					if state == "discard" {
						contains = []string{"Discard draft", "[Esc/n] keep editing"}
					}
					switch state {
					case "editing":
						contains = append(contains, "Tab: field")
					case "loading", "editing-loading":
						contains = append(contains, "Generating with AI")
					case "error":
						contains = append(contains, "Authentication required")
					case "populated":
						contains = append(contains, "Accepted title", "Accepted body")
					}
					metadata := inspectCapture(name, plain, width, 40, contains, nil)
					for _, check := range metadata.Checks {
						if !check.Passed {
							t.Errorf("check failed: %s", check.Name)
						}
					}
					writeCaptureFile(t, filepath.Join(dir, name, "view.ansi.txt"), []byte(raw))
					writeCaptureFile(t, filepath.Join(dir, name, "view.txt"), []byte(plain))
					writeCaptureJSON(t, filepath.Join(dir, name, "metadata.json"), metadata)
				})
			}
		}
	}
}

func captureMetadataModel(t *testing.T, width, height int, kind string) Model {
	t.Helper()
	var dialog modal.Modal
	switch kind {
	case "mr":
		d := modal.NewForgeMenuModal("api", forge.ForgeProviderGitLab, width, height)
		d.SetTaskID("T-1")
		d.Update(tea.KeyMsg{Type: tea.KeyEnter})
		dialog = d
	case "release":
		d := modal.NewCreateReleaseDialog([]domain.Task{{ID: "T-1", Phase: "feature", Services: []domain.Service{{Name: "api"}}}}, width, height)
		d.Update(tea.KeyMsg{Type: tea.KeySpace})
		d.Update(tea.KeyMsg{Type: tea.KeyEnter})
		d.Update(panels.ReleaseVersionsLoadedMsg{Versions: map[string]string{"api": "1.0.1"}})
		dialog = d
	case "close":
		dialog = modal.NewCloseTaskConfirmModal(domain.Task{ID: "T-1", Phase: "feature"}, task.ClosePlan{TaskID: "T-1", Services: []task.ServiceClosePlan{{ServiceName: "api", TagPlan: &task.TagPlan{Version: "1.0.1", TagName: "v1.0.1"}}}}, width, height)
	case "hotfix", "hotfix-locked":
		dialog = modal.NewHotfixCloseModal(task.ClosePlan{TaskID: "H-1", Fingerprint: "accepted-sha", Services: []task.ServiceClosePlan{{ServiceName: "api", TagPlan: &task.TagPlan{Version: "1.0.1", TagName: "v1.0.1", SourceRef: "accepted-sha", Locked: kind == "hotfix-locked", Message: "Saved tag message"}}}})
	}
	return captureModalModel(t, width, height, dialog)
}
