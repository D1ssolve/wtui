package forge

import (
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
)

func TestIssueLinkIdentity(t *testing.T) {
	settings := &config.Config{
		Forge:  &config.ForgeConfig{GitLabHost: "forge.example"},
		Issues: &config.IssuesConfig{JiraURL: "https://jira.example/browse/{{.Key}}"},
	}
	for _, tc := range []struct{ input, remote, id, reference string }{
		{"https://github.com/ORG/API/issues/12", "git@github.com:org/api.git", "API-12", "Closes #12"},
		{"https://forge.example/org/sub/api/-/issues/12", "https://forge.example/org/sub/api.git", "API-12", "Closes #12"},
		{"https://forge.example/org/sub/api/-/issues/12", "https://forge.example/org/api.git", "API-12", "Issue: https://forge.example/org/sub/api/-/issues/12"},
		{"APP-12", "git@github.com:org/api.git", "APP-12", "Issue: https://jira.example/browse/APP-12"},
		{"https://jira.example/browse/APP-12", "git@github.com:org/api.git", "APP-12", "Issue: https://jira.example/browse/APP-12"},
	} {
		t.Run(tc.input+" "+tc.remote, func(t *testing.T) {
			issue, err := ParseIssue(tc.input, settings.Issues, settings.Forge)
			if err != nil || issue == nil {
				t.Fatalf("ParseIssue = %v, %v", issue, err)
			}
			if id, ref := IssueTaskID(*issue, settings), IssueReference(issue, tc.remote, settings.Forge); id != tc.id || ref != tc.reference {
				t.Fatalf("task ID = %q, reference = %q", id, ref)
			}
		})
	}
}
