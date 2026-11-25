package config

import (
	"strings"
	"testing"
)

func TestIssuesConfig(t *testing.T) {
	for _, tc := range []struct {
		name, jiraURL string
		valid         bool
	}{
		{"omitted", "", true},
		{"template", "https://jira.example/browse/{{.Key}}", true},
		{"missing key", "https://jira.example/browse/APP-12", false},
		{"key in host", "https://{{.Key}}.example/browse", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTempConfig(t, "issues:\n  task_prefix: APP\n  jira_url: '"+tc.jiraURL+"'\n")
			cfg, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			_, err = cfg.Effective()
			switch {
			case tc.valid && err != nil:
				t.Fatal(err)
			case !tc.valid && (err == nil || !strings.Contains(err.Error(), "issues.jira_url")):
				t.Fatalf("Effective error = %v, want issues.jira_url", err)
			}
		})
	}
}
