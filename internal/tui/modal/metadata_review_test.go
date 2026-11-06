package modal

import (
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/panels"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestMetadata_ReleaseBackNavigationPreservesAcceptedServiceMetadata(t *testing.T) {
	for _, addTask := range []bool{false, true} {
		t.Run(map[bool]string{false: "same selection", true: "additional service"}[addTask], func(t *testing.T) {
			dialog := NewCreateReleaseDialog([]domain.Task{
				{ID: "T-1", Phase: "feature", Services: []domain.Service{{Name: "api"}}},
				{ID: "T-2", Phase: "feature", Services: []domain.Service{{Name: "web"}}},
			}, 120, 40)
			dialog.Update(sendKey(" "))
			dialog.Update(sendSpecialKey(tea.KeyEnter))
			dialog.Update(panels.ReleaseVersionsLoadedMsg{Versions: map[string]string{"api": "1.0.1"}})
			dialog.ApplyMetadataDraft(metadataDraft())
			dialog.Update(sendSpecialKey(tea.KeyEsc))
			if addTask {
				dialog.Update(sendSpecialKey(tea.KeyDown))
				dialog.Update(sendKey(" "))
			}
			dialog.Update(sendSpecialKey(tea.KeyEnter))
			dialog.Update(panels.ReleaseVersionsLoadedMsg{Versions: map[string]string{"api": "1.0.2", "web": "0.1.0"}})
			_, submit := dialog.Update(sendSpecialKey(tea.KeyEnter))
			if submit == nil {
				t.Fatal("accepted release cannot be submitted after back navigation")
			}
			got := submit().(SubmitCreateReleaseMsg)
			if got.Versions["api"] != "2.0.0" || got.TagDescriptions["api"] != "Generated tag" ||
				got.Title != "Generated title" || got.Description != "Generated\nbody" {
				t.Fatalf("back navigation lost accepted metadata: %#v", got)
			}
			if addTask && got.Versions["web"] != "0.1.0" {
				t.Fatalf("new service was not initialized: %#v", got.Versions)
			}
		})
	}
}

func TestMetadata_ReleaseTagEditorLoadingShowsOnlyAvailableControls(t *testing.T) {
	dialog := NewCreateReleaseDialog([]domain.Task{
		{ID: "T-1", Phase: "feature", Services: []domain.Service{{Name: "api"}}},
	}, 120, 40)
	dialog.Update(sendKey(" "))
	dialog.Update(sendSpecialKey(tea.KeyEnter))
	dialog.Update(panels.ReleaseVersionsLoadedMsg{Versions: map[string]string{"api": "1.0.1"}})
	dialog.Update(sendSpecialKey(tea.KeyTab))
	dialog.Update(sendSpecialKey(tea.KeyTab))
	dialog.Update(sendSpecialKey(tea.KeyEnter))
	dialog.SetMetadataStatus(true, "")

	view := stripAnsi(dialog.View())

	if strings.Contains(view, "[Ctrl+S] save") || strings.Contains(view, "[Esc] cancel") ||
		!strings.Contains(view, "Generating with AI... Esc cancels") {
		t.Fatalf("loading tag editor advertises unavailable controls:\n%s", view)
	}
	if !dialog.descriptionEditor.Focused() {
		t.Fatal("rendering loading state must preserve focus for cancellation")
	}
	dialog.SetMetadataStatus(false, "")
	if !strings.Contains(stripAnsi(dialog.View()), "[Ctrl+S] save") {
		t.Fatal("editor controls were not restored after generation")
	}
}

func TestMetadata_HotfixRegenerationKeepsDescriptionInSelectedEditor(t *testing.T) {
	dialog := NewHotfixCloseModal(task.ClosePlan{TaskID: "H-1", Services: []task.ServiceClosePlan{{ServiceName: "api", TagPlan: &task.TagPlan{Version: "1.0.1"}}}})
	dialog.SetTerminalSize(120, 40)
	dialog.ApplyMetadataDraft(metadataDraft())
	dialog.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	dialog.Update(tea.KeyMsg{Type: tea.KeyTab})
	next := metadataDraft()
	next.Title, next.Description = "Second title", "Second description"

	dialog.ApplyMetadataDraft(next)
	dialog.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	_, submit := dialog.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if got := submit().(SubmitCloseTaskMsg); got.ReviewDescription != "Second description" {
		t.Fatalf("regeneration replaced description with another field: %#v", got)
	}
}

func TestMetadata_DoesNotShowRationaleForUnusedVersion(t *testing.T) {
	mr := NewForgeMenuModal("api", forge.ForgeProviderGitLab, 120, 40)
	mr.SetTaskID("T-1")
	mr.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tests := []interface {
		Modal
		ApplyMetadataDraft(ai.Draft)
	}{
		mr,
		NewCloseTaskConfirmModal(domain.Task{ID: "T-1"}, task.ClosePlan{TaskID: "T-1", Services: []task.ServiceClosePlan{{ServiceName: "api"}}}, 120, 40),
		NewHotfixCloseModal(task.ClosePlan{TaskID: "H-1", Services: []task.ServiceClosePlan{{ServiceName: "api", TagPlan: &task.TagPlan{Version: "1.0.1", Locked: true}}}}),
	}
	for _, dialog := range tests {
		dialog.SetTerminalSize(120, 40)
		draft := metadataDraft()
		draft.Services[0].Reason = "Unused version rationale"
		dialog.ApplyMetadataDraft(draft)
		if strings.Contains(dialog.View(), "Unused version rationale") {
			t.Fatalf("%T shows rationale for a version it will not apply", dialog)
		}
	}
}

func TestMetadata_TagMessageIsVisibleBeforeConfirmation(t *testing.T) {
	plan := task.ClosePlan{TaskID: "T-1", Services: []task.ServiceClosePlan{{ServiceName: "api", TagPlan: &task.TagPlan{Version: "1.0.1"}}}}
	for _, dialog := range []interface {
		Modal
		ApplyMetadataDraft(ai.Draft)
	}{
		NewCloseTaskConfirmModal(domain.Task{ID: "T-1"}, plan, 120, 40),
		NewHotfixCloseModal(plan),
	} {
		dialog.SetTerminalSize(120, 40)
		dialog.ApplyMetadataDraft(metadataDraft())
		if !strings.Contains(dialog.View(), "Generated tag") {
			t.Fatalf("%T hides the tag message before confirmation", dialog)
		}
	}
}

func TestMetadata_NormalClosePreviewMatchesEditedServiceVersions(t *testing.T) {
	dialog := NewCloseTaskConfirmModal(domain.Task{ID: "T-1"}, task.ClosePlan{TaskID: "T-1", Services: []task.ServiceClosePlan{
		{ServiceName: "api", TagPlan: &task.TagPlan{Version: "1.0.1", TagName: "v1.0.1"}},
		{ServiceName: "web", TagPlan: &task.TagPlan{Version: "1.0.1", TagName: "v1.0.1"}},
	}}, 120, 40)
	draft := metadataDraft()
	draft.Services = append(draft.Services, ai.ServiceDraft{Name: "web", Version: "3.0.0", TagDescription: "Web changes"})
	dialog.ApplyMetadataDraft(draft)
	dialog.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	for range 3 {
		dialog.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	dialog.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	dialog.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2.1.0")})
	dialog.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	view := dialog.View()

	if !strings.Contains(view, "2.1.0") || !strings.Contains(view, "3.0.0") ||
		strings.Contains(view, "2.0.0") || strings.Contains(view, "v1.0.1") {
		t.Fatalf("confirmation does not show accepted service versions:\n%s", view)
	}
}

func TestMetadata_LongMRConfirmationKeepsReasonsAndActionsVisible(t *testing.T) {
	description := strings.Repeat("Accepted release note\n", 100)
	dialog := NewMRCreateConfirmDialog("T-1", "Accepted title", []MRCreateConfirmItem{{ServiceName: "api", Reason: "source is not pushed"}}, description)
	dialog.SetTerminalSize(80, 40)

	view := dialog.View()

	_, height := overlayContentSize(80, 40)
	if lipgloss.Height(view) > height || !strings.Contains(view, "source is not pushed") || !strings.Contains(view, "[Esc/n] cancel") {
		t.Fatalf("confirmation reasons or actions are hidden:\n%s", view)
	}
	_, submit := dialog.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := submit().(ForgeConfirmCreateMRMsg); got.Description != description {
		t.Fatal("bounded preview truncated the accepted MR description")
	}
}

func TestMetadata_DiscardRequiresExplicitConfirmation(t *testing.T) {
	for _, kind := range []string{"close", "hotfix"} {
		t.Run(kind, func(t *testing.T) {
			var dialog interface {
				Modal
				ApplyMetadataDraft(ai.Draft)
			}
			if kind == "close" {
				dialog = NewCloseTaskConfirmModal(domain.Task{ID: "T-1"}, task.ClosePlan{TaskID: "T-1"}, 120, 40)
			} else {
				dialog = NewHotfixCloseModal(task.ClosePlan{TaskID: "H-1"})
				dialog.SetTerminalSize(120, 40)
			}
			dialog.ApplyMetadataDraft(metadataDraft())

			_, first := dialog.Update(tea.KeyMsg{Type: tea.KeyEsc})

			if first != nil || !strings.Contains(dialog.View(), "Discard") {
				t.Fatal("Escape silently discards a generated draft")
			}
			dialog.Update(tea.KeyMsg{Type: tea.KeyEsc})
			if !strings.Contains(dialog.View(), "Generated title") {
				t.Fatal("keeping the draft lost accepted metadata")
			}
			dialog.Update(tea.KeyMsg{Type: tea.KeyEsc})
			_, discard := dialog.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
			if discard == nil {
				t.Fatal("explicit discard did not close the modal")
			}
			if _, ok := discard().(CloseModalMsg); !ok {
				t.Fatal("discard produced an unexpected action")
			}
		})
	}
}

func TestMetadata_ConfirmedDiscardCannotSubmitBeforeCloseMessage(t *testing.T) {
	dialog := NewCloseTaskConfirmModal(domain.Task{ID: "T-1"}, task.ClosePlan{TaskID: "T-1"}, 120, 40)
	dialog.ApplyMetadataDraft(metadataDraft())
	dialog.Update(tea.KeyMsg{Type: tea.KeyEsc})
	_, closeCommand := dialog.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if closeCommand == nil {
		t.Fatal("discard must schedule closing the modal")
	}

	_, submit := dialog.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if submit != nil {
		t.Fatal("discarded draft can execute before its queued close message")
	}
}
