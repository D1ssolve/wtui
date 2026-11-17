package tui

import (
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/tui/modal"
)

func TestKeyMsgFor_RoundTripsEveryRegistryKey(t *testing.T) {
	for _, a := range actionRegistry {
		for _, k := range a.keys {
			if got := keyMsgFor(k).String(); got != k {
				t.Errorf("keyMsgFor(%q).String() = %q", k, got)
			}
		}
	}
}

// actionFixtures cover every state an action can require, so each registry
// entry is exercised against the real dispatch in at least one fixture.
func actionFixtures(t *testing.T) []Model {
	t.Helper()
	flow := &gitflow.ResolvedGitFlow{BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
		gitflow.BranchTypeFeature: {Prefixes: []string{"feature/"}},
		gitflow.BranchTypeHotfix:  {Prefixes: []string{"hotfix/"}},
	}}
	base := func(task domain.Task) Model {
		m := newTestModelWithOptions(t, &mockManager{}, Options{ResolvedFlow: flow})
		m = sendWindowSize(m, 160, 50)
		m.lazygitAvailable = true
		m.servicesPanel.SetLazygitAvailable(true)
		task.Dir = t.TempDir()
		m.tasksPanel.SetTasks([]domain.Task{task})
		m.servicesPanel.SetServices(task.ID, []domain.Service{{Name: "api", WorktreePath: t.TempDir()}})
		return m
	}
	fixtures := []Model{
		base(domain.Task{ID: "T-1", Services: []domain.Service{{Name: "api", Branch: "feature/T-1"}}}),
		base(domain.Task{ID: "T-2-hotfix", Phase: "hotfix"}),
	}
	for _, rel := range []domain.Release{
		{Status: domain.ReleaseStatusDraft},
		{Status: domain.ReleaseStatusPrepared},
		{Status: domain.ReleaseStatusAwaitingMasterMerge},
		{Status: domain.ReleaseStatusMasterMerged},
		{Status: domain.ReleaseStatusReleased},
		{Status: domain.ReleaseStatusFailed, Error: &domain.ReleaseError{Recoverable: true}},
	} {
		m := base(domain.Task{ID: "T-1"})
		rel.ID, rel.Dir = "rel-1", t.TempDir()
		m.releasesPanel.SetReleases([]domain.Release{rel})
		fixtures = append(fixtures, m)
	}
	return fixtures
}

func TestActionRegistry_EveryRunnableKeyIsHandled(t *testing.T) {
	for i, a := range actionRegistry {
		if a.nav || len(a.keys) == 0 {
			continue
		}
		exercised := false
		for _, m := range actionFixtures(t) {
			if focus, ok := scopeFocus[a.scope]; ok {
				m.setFocus(focus)
			}
			if !a.available(m) {
				continue
			}
			exercised = true
			for _, k := range a.keys {
				focusBefore := m.focus
				updated, cmd := m.Update(keyMsgFor(k))
				after := updated.(Model)
				filtering := after.tasksPanel.FilterActive() || after.servicesPanel.FilterActive()
				if cmd == nil && after.modal == nil && after.logOverlay == nil && after.focus == focusBefore && !filtering {
					t.Errorf("action %d (%s %q) key %q has no effect", i, a.scope, a.help, k)
				}
			}
			break
		}
		if !exercised {
			t.Errorf("action %d (%s %q) is not available in any fixture", i, a.scope, a.help)
		}
	}
}

func TestCommandPalette_RunsActionInItsPanel(t *testing.T) {
	m := actionFixtures(t)[0]
	m.setFocus(FocusReleases)

	updated, _ := m.Update(keyMsgFor(":"))
	m = updated.(Model)
	if _, ok := m.modal.(*modal.CommandPalette); !ok {
		t.Fatalf("':' must open the command palette, got %T", m.modal)
	}

	closeID := -1
	for _, item := range paletteItems(m) {
		if a := actionRegistry[item.ID]; a.scope == scopeTasks && a.keys[0] == "C" {
			closeID = item.ID
		}
	}
	if closeID < 0 {
		t.Fatal("palette must offer task close for the selected task")
	}
	updated, cmd := m.Update(modal.RunPaletteItemMsg{ID: closeID})
	m = updated.(Model)
	if m.focus != FocusTasks || m.modal != nil || cmd == nil {
		t.Fatalf("palette action must run in the tasks panel: focus=%s modal=%T cmd=%v", m.focus, m.modal, cmd != nil)
	}
}

func TestHelpSections_CurrentPanelFirstAndEnvironmentGated(t *testing.T) {
	m := actionFixtures(t)[0]
	m.setFocus(FocusServices)
	m.lazygitAvailable = false

	sections := helpSections(m)
	if sections[0].Title != string(scopeServices)+" (current)" {
		t.Fatalf("first section = %q, want current services panel", sections[0].Title)
	}
	for _, row := range sections[0].Rows {
		if strings.Contains(row.Desc, "lazygit") {
			t.Fatal("lazygit row shown without lazygit installed")
		}
	}
}
