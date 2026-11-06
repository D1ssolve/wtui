package modal

import (
	"testing"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/task"
	tea "github.com/charmbracelet/bubbletea"
)

func metadataDraft() ai.Draft {
	return ai.Draft{Title: "Generated title", Description: "Generated\nbody", Services: []ai.ServiceDraft{{Name: "api", Version: "2.0.0", TagDescription: "Generated tag"}}}
}

func TestMetadata_MRGeneratedBodyRemainsEditable(t *testing.T) {
	d := NewForgeMenuModal("api", forge.ForgeProviderGitLab, 120, 40)
	d.SetTaskID("T-1")
	d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	d.ApplyMetadataDraft(metadataDraft())
	d.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	d.Update(tea.KeyMsg{Type: tea.KeyTab})
	d.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Accepted body")})
	d.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := cmd().(ForgeCreateMRMsg)
	if got.Title != "Generated title" || got.Description != "Accepted body" {
		t.Fatalf("accepted MR = %#v", got)
	}
}

func TestMetadata_ReleaseUsesDraftVersionsAndDescriptions(t *testing.T) {
	d := NewCreateReleaseDialog([]domain.Task{{ID: "T-1", Phase: "feature", Services: []domain.Service{{Name: "api"}}}}, 120, 40)
	d.Update(tea.KeyMsg{Type: tea.KeySpace})
	d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	d.applyVersions(map[string]string{"api": "1.0.1"})
	d.ApplyMetadataDraft(metadataDraft())
	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := cmd().(SubmitCreateReleaseMsg)
	if got.Title != "Generated title" || got.Description != "Generated\nbody" || got.Versions["api"] != "2.0.0" || got.TagDescriptions["api"] != "Generated tag" {
		t.Fatalf("accepted release = %#v", got)
	}
}

func TestMetadata_ClosePreservesLockedVersionsAndFingerprint(t *testing.T) {
	for _, locked := range []bool{false, true} {
		t.Run(map[bool]string{false: "editable", true: "locked"}[locked], func(t *testing.T) {
			d := NewHotfixCloseModal(task.ClosePlan{TaskID: "H-1", Fingerprint: "accepted-sha", Services: []task.ServiceClosePlan{{ServiceName: "api", TagPlan: &task.TagPlan{Version: "1.0.1", Locked: locked}}}})
			d.ApplyMetadataDraft(metadataDraft())
			_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
			got := cmd().(SubmitCloseTaskMsg)
			want := "2.0.0"
			if locked {
				want = "1.0.1"
			}
			wantMessage := "Generated tag"
			if locked {
				wantMessage = ""
			}
			if got.TagVersions["api"] != want || got.Fingerprint != "accepted-sha" || got.ReviewTitle != "Generated title" || got.ReviewDescription != "Generated\nbody" || got.TagMessages["api"] != wantMessage {
				t.Fatalf("accepted close = %#v", got)
			}
		})
	}
}

func TestMetadata_NormalCloseKeepsServiceVersionsEditable(t *testing.T) {
	plan := task.ClosePlan{TaskID: "T-1", Services: []task.ServiceClosePlan{{ServiceName: "api", TagPlan: &task.TagPlan{Version: "1.0.1"}}}}
	d := NewCloseTaskConfirmModal(domain.Task{ID: "T-1"}, plan, 120, 40)
	d.ApplyMetadataDraft(metadataDraft())
	d.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	// Title, description, tag message, then the service version.
	for range 3 {
		d.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	d.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2.1.0")})
	d.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := cmd().(SubmitCloseTaskMsg)
	if got.TagVersions["api"] != "2.1.0" {
		t.Fatalf("service version not editable: %#v", got)
	}
}

func TestMetadata_NormalCloseCannotEditLockedVersion(t *testing.T) {
	plan := task.ClosePlan{TaskID: "T-1", Services: []task.ServiceClosePlan{{ServiceName: "api", TagPlan: &task.TagPlan{Version: "1.0.1", Locked: true}}}}
	d := NewCloseTaskConfirmModal(domain.Task{ID: "T-1"}, plan, 120, 40)
	d.ApplyMetadataDraft(metadataDraft())
	d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("9")})
	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := cmd().(SubmitCloseTaskMsg)
	if got.TagVersion != "" && got.TagVersion != "1.0.1" {
		t.Fatalf("locked version edited: %#v", got)
	}
	if got.TagVersions["api"] != "" && got.TagVersions["api"] != "1.0.1" {
		t.Fatalf("locked service version edited: %#v", got)
	}
}

func TestMetadata_NormalCloseNavigationKeepsPerServiceDraftVersions(t *testing.T) {
	plan := task.ClosePlan{TaskID: "T-1", Services: []task.ServiceClosePlan{
		{ServiceName: "api", TagPlan: &task.TagPlan{Version: "1.0.1"}},
		{ServiceName: "web", TagPlan: &task.TagPlan{Version: "1.0.1"}},
	}}
	d := NewCloseTaskConfirmModal(domain.Task{ID: "T-1"}, plan, 120, 40)
	draft := metadataDraft()
	draft.Services = append(draft.Services, ai.ServiceDraft{Name: "web", Version: "3.0.0", TagDescription: "Web changes"})
	d.ApplyMetadataDraft(draft)
	d.Update(tea.KeyMsg{Type: tea.KeyLeft})
	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := cmd().(SubmitCloseTaskMsg)
	if got.TagVersions["api"] != "2.0.0" || got.TagVersions["web"] != "3.0.0" {
		t.Fatalf("navigation overwrote service versions: %#v", got)
	}
}
