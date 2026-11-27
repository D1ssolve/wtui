package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/tracker"
	"github.com/D1ssolve/wtui/internal/tui/modal"
)

func runCmd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	updated, _ := m.Update(cmd())
	return updated.(Model)
}

func trackerModel(t *testing.T, mgr *mockManager) Model {
	m := sendWindowSize(newTestModel(t, mgr), 140, 50)
	flow, err := gitflow.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	m.flow = flow
	m.cfg.Tracker = &config.TrackerConfig{Plugin: "jira", BranchTypes: map[string]string{"Bug": "hotfix"},
		Queries: []config.TrackerQuery{{Name: "Free", Query: "assignee IS empty"}, {Name: "Mine", Query: "assignee = me"}}}
	return m
}

func TestTrackerInbox_JThenDetailsThenEnter_PrefillsInitDialog(t *testing.T) {
	mgr := &mockManager{trackerIssues: []tracker.Issue{{Key: "WT-7", Summary: "Crash on save", Type: "Bug", Status: "To Do", URL: "https://x.atlassian.net/browse/WT-7"}}}
	m := trackerModel(t, mgr)

	updated, cmd := m.Update(sendKey("J"))
	m = runCmd(t, updated.(Model), cmd)
	inbox, ok := m.modal.(*modal.TrackerInbox)
	if !ok || !strings.Contains(inbox.View(), "WT-7  Crash on save") {
		t.Fatalf("modal = %T, want the tracker inbox listing WT-7", m.modal)
	}

	updated, cmd = m.Update(sendKey("v"))
	m = runCmd(t, updated.(Model), cmd)
	m = runCmd(t, m, func() tea.Msg { return trackerIssueCmd(mgr, "WT-7")() })
	view, ok := m.modal.(*modal.TrackerIssueView)
	if !ok || !strings.Contains(view.View(), "details of WT-7") {
		t.Fatalf("modal = %T, want the issue details", m.modal)
	}

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = runCmd(t, updated.(Model), cmd)
	dialog, ok := m.modal.(*modal.InitDialog)
	if !ok {
		t.Fatalf("modal = %T, want the init dialog", m.modal)
	}
	if v := dialog.View(); !strings.Contains(v, "WT-7") || !strings.Contains(v, "< hotfix >") {
		t.Fatalf("init dialog not prefilled from the issue:\n%s", v)
	}
}

func TestTrackerInbox_Tab_SwitchesFilter(t *testing.T) {
	mgr := &mockManager{trackerIssues: []tracker.Issue{{Key: "WT-1"}}}
	m := trackerModel(t, mgr)
	updated, cmd := m.Update(sendKey("J"))
	m = runCmd(t, updated.(Model), cmd)
	if !strings.Contains(m.outputPanel.View(), "Loading tracker issues: Free") {
		t.Fatalf("output = %q", m.outputPanel.View())
	}
	if v := m.modal.View(); !strings.Contains(v, "[Free]") || !strings.Contains(v, " Mine ") {
		t.Fatalf("inbox header does not mark the first filter:\n%s", v)
	}

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = runCmd(t, updated.(Model), cmd)
	m = runCmd(t, m, func() tea.Msg { return trackerIssuesCmd(mgr, 1, "assignee = me")() })
	if v := m.modal.View(); !strings.Contains(v, "[Mine]") {
		t.Fatalf("Tab did not switch to the second filter:\n%s", v)
	}
	if want := []string{"assignee IS empty", "assignee = me"}; strings.Join(mgr.trackerQueries[:2], "|") != strings.Join(want, "|") {
		t.Fatalf("queries = %q, want %q", mgr.trackerQueries, want)
	}
}

func TestTrackerInbox_LoadFailure_ReportsWithoutModal(t *testing.T) {
	m := trackerModel(t, &mockManager{trackerErr: errors.New("set JIRA_EMAIL")})
	updated, cmd := m.Update(sendKey("J"))
	m = runCmd(t, updated.(Model), cmd)
	if m.modal != nil || !strings.Contains(m.outputPanel.View(), "set JIRA_EMAIL") {
		t.Fatalf("modal = %T, output = %q", m.modal, m.outputPanel.View())
	}
}

func TestTrackerInbox_JIgnoredWithoutTracker(t *testing.T) {
	m := sendWindowSize(newTestModel(t, &mockManager{}), 140, 50)
	updated, _ := m.Update(sendKey("J"))
	if updated.(Model).modal != nil {
		t.Fatal("J must do nothing when no tracker is configured")
	}
}
