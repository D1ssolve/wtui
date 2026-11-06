package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	"github.com/D1ssolve/wtui/internal/tui/panels"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestCaptureMetadataEdgeViews(t *testing.T) {
	dir := os.Getenv("WTUI_CAPTURE_DIR")
	if dir == "" {
		t.Skip("WTUI_CAPTURE_DIR not set")
	}
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
	for _, width := range []int{80, 120} {
		for _, scenario := range []string{"mr-long-confirm", "close-edited-version", "hotfix-regenerated-body", "no-provider", "release-tag-editor", "release-tag-loading", "release-back-navigation", "hotfix-short"} {
			name := fmt.Sprintf("metadata-%s-%d", scenario, width)
			t.Run(name, func(t *testing.T) {
				height := 40
				var m Model
				contains := []string{}
				draft := ai.Draft{Title: "Accepted title", Description: "Accepted body", Services: []ai.ServiceDraft{{Name: "api", Version: "2.0.0", Bump: ai.BumpMajor, Reason: "Breaking API change", TagDescription: "Accepted tag"}}}
				switch scenario {
				case "mr-long-confirm":
					dialog := modal.NewMRCreateConfirmDialog("T-1", "Accepted title", []modal.MRCreateConfirmItem{{ServiceName: "api", Reason: "source is not pushed"}}, strings.Repeat("Accepted note\n", 100))
					m = captureModalModel(t, width, height, dialog)
					contains = []string{"source is not pushed", "[Esc/n] cancel"}
				case "close-edited-version":
					m = captureMetadataModel(t, width, height, "close")
					dialog := m.modal.(metadataModal)
					dialog.ApplyMetadataDraft(draft)
					m.modal.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
					for range 3 {
						m.modal.Update(tea.KeyMsg{Type: tea.KeyTab})
					}
					m.modal.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
					m.modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2.1.0")})
					m.modal.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
					contains = []string{"version 2.1.0", "Accepted tag"}
				case "hotfix-regenerated-body":
					m = captureMetadataModel(t, width, height, "hotfix")
					dialog := m.modal.(metadataModal)
					dialog.ApplyMetadataDraft(draft)
					m.modal.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
					m.modal.Update(tea.KeyMsg{Type: tea.KeyTab})
					draft.Description = "Regenerated description"
					dialog.ApplyMetadataDraft(draft)
					contains = []string{"Regenerated description", "(2/"}
				case "no-provider":
					m = captureMetadataModel(t, width, height, "mr")
					next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
					m = next.(Model)
					contains = []string{"No AI provider configured", "edit manually"}
				case "release-tag-editor", "release-tag-loading":
					m = captureMetadataModel(t, width, height, "release")
					m.modal.(metadataModal).ApplyMetadataDraft(draft)
					m.modal.Update(tea.KeyMsg{Type: tea.KeyTab})
					m.modal.Update(tea.KeyMsg{Type: tea.KeyTab})
					m.modal.Update(tea.KeyMsg{Type: tea.KeyEnter})
					contains = []string{"Tag description", "Accepted tag", "cancel"}
					if scenario == "release-tag-loading" {
						m.modal.(metadataModal).SetMetadataStatus(true, "")
						contains = []string{"Tag description", "Accepted tag", "Inputs paused", "Generating with AI"}
					}
				case "release-back-navigation":
					m = captureMetadataModel(t, width, height, "release")
					m.modal.(metadataModal).ApplyMetadataDraft(draft)
					m.modal.Update(tea.KeyMsg{Type: tea.KeyEsc})
					m.modal.Update(tea.KeyMsg{Type: tea.KeyEnter})
					m.modal.Update(panels.ReleaseVersionsLoadedMsg{Versions: map[string]string{"api": "1.0.2"}})
					m.modal.Update(tea.KeyMsg{Type: tea.KeyTab})
					m.modal.Update(tea.KeyMsg{Type: tea.KeyTab})
					contains = []string{"2.0.0", "Accepted tag", "Breaking API change"}
				case "hotfix-short":
					height = 24
					plan := task.ClosePlan{TaskID: "H-1", Services: []task.ServiceClosePlan{{ServiceName: "api", TagPlan: &task.TagPlan{Version: "1.0.1", TagName: "v1.0.1", SourceRef: "accepted-sha", Locked: true}}}}
					m = captureModalModel(t, width, height, modal.NewHotfixCloseModal(plan))
					m.modal.(metadataModal).ApplyMetadataDraft(draft)
					contains = []string{"1.0.1", "Ctrl+G"}
				default:
					t.Fatal("unknown capture scenario")
				}
				raw := m.View()
				plain := stripANSIForModel(raw)
				metadata := inspectCapture(name, plain, width, height, contains, nil)
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
