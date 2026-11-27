package config

import (
	"strings"
	"testing"
)

func TestTrackerConfig_Effective(t *testing.T) {
	for _, tc := range []struct {
		name, yaml, wantErr string
		want                []TrackerQuery
	}{
		{"omitted", "", "", nil},
		{"default query", "tracker:\n  plugin: jira\n", "", []TrackerQuery{{"My issues", defaultTrackerQuery}}},
		{"named queries", "tracker:\n  plugin: jira\n  queries:\n    - {name: Free, query: project = APP}\n    - query: |\n        project = APP\n        AND assignee = currentUser()\n",
			"", []TrackerQuery{{"Free", "project = APP"}, {"Query 2", "project = APP\nAND assignee = currentUser()"}}},
		{"empty query", "tracker:\n  plugin: jira\n  queries: [{name: x}]\n", "tracker.queries[0].query", nil},
		{"missing plugin", "tracker:\n  branch_types: {Bug: hotfix}\n", "tracker.plugin", nil},
		{"path as plugin", "tracker:\n  plugin: ../evil\n", "tracker.plugin", nil},
		{"unknown key", "tracker:\n  plugin: jira\n  query: x\n", "query", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load(writeTempConfig(t, tc.yaml))
			if err == nil {
				cfg, err = cfg.Effective()
			}
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var got []TrackerQuery
			if cfg.Tracker != nil {
				got = cfg.Tracker.Queries
			}
			if len(got) != len(tc.want) {
				t.Fatalf("queries = %+v, want %+v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("queries = %+v, want %+v", got, tc.want)
				}
			}
		})
	}
}

func TestTrackerConfig_MappingsParsed(t *testing.T) {
	cfg, err := Load(writeTempConfig(t, "tracker:\n  plugin: jira\n  comment_mrs: true\n  branch_types: {Bug: bugfix}\n  transitions:\n    task_created: In Progress\n    mr_merged: Done\n"))
	if err != nil {
		t.Fatal(err)
	}
	tr := cfg.Tracker
	if tr.BranchTypes["Bug"] != "bugfix" || tr.Transitions.TaskCreated != "In Progress" || tr.Transitions.MRMerged != "Done" || !tr.CommentMRs {
		t.Fatalf("tracker = %+v", tr)
	}
}
