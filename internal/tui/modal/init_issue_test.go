package modal

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
)

func TestIssuePrefillAndEditing(t *testing.T) {
	dialog := NewInitDialog("feature/", []domain.Repo{{Name: "api"}}, 100, 30)
	dialog.SetIssueConfig(&config.Config{Issues: &config.IssuesConfig{TaskPrefix: "TEST"}})
	link := domain.Issue{Provider: domain.IssueGitHub, Key: "12", URL: "https://github.com/org/api/issues/12", Title: "Fix issue"}
	dialog.PrefillIssue(link, "api")
	dialog.focusField(dialog.lastFieldIndex())
	_, cmd := dialog.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("prefilled issue did not submit")
	}
	msg, ok := cmd().(SubmitInitMsg)
	if !ok || msg.TaskID != "TEST-12" || msg.Issue == nil || *msg.Issue != link || len(msg.Services) != 1 || msg.Services[0] != "api" {
		t.Fatalf("prefilled submission = %+v", msg)
	}
	dialog.fields[4].SetValue("not a url or key!")
	if _, cmd := dialog.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil || dialog.errorMsg == "" {
		t.Fatalf("invalid issue did not block submission with a diagnostic")
	}
	dialog.fields[4].SetValue("OTHER-13")
	_, cmd = dialog.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("edited issue did not submit")
	}
	msg, ok = cmd().(SubmitInitMsg)
	if !ok || msg.Issue == nil || msg.Issue.Key != "OTHER-13" || msg.Issue.Title != "" {
		t.Fatalf("edited submission carried stale issue data: %+v", msg)
	}
}
