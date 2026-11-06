package forge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadinessGuidance_GitHubBlockers(t *testing.T) {
	for _, tc := range []struct {
		name, state, approval, mergeable, checks, action string
	}{
		{"ready", "OPEN", "APPROVED", "MERGEABLE", "[]", ""},
		{"closed", "CLOSED", "APPROVED", "MERGEABLE", "[]", "forge"},
		{"merged", "MERGED", "APPROVED", "MERGEABLE", "[]", "reconcile"},
		{"state unknown", "", "APPROVED", "MERGEABLE", "[]", "refresh"},
		{"approval", "OPEN", "REVIEW_REQUIRED", "MERGEABLE", "[]", "approval"},
		{"pending checks", "OPEN", "APPROVED", "MERGEABLE", `[{"state":"PENDING"}]`, "wait"},
		{"failed checks", "OPEN", "APPROVED", "MERGEABLE", `[{"state":"FAILURE"}]`, "failed"},
		{"conflicts", "OPEN", "APPROVED", "CONFLICTING", "[]", "conflict"},
		{"unknown mergeability", "OPEN", "APPROVED", "UNKNOWN", "[]", "refresh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := t.TempDir()
			body := `[{"number":7,"state":"` + tc.state + `","reviewDecision":"` + tc.approval + `","mergeable":"` + tc.mergeable + `","statusCheckRollup":` + tc.checks + `}]`
			if err := os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\nprintf '%s' '"+body+"'\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin)
			got, err := NewGhClient(t.TempDir()).MRReadiness(t.Context(), "feature/a", "group/repo", "")
			if err != nil {
				t.Fatal(err)
			}
			checkReadinessRecovery(t, got, tc.action)
		})
	}
}

func TestReadinessGuidance_GitLabBlockersKeepCINonBlocking(t *testing.T) {
	for _, tc := range []struct {
		name, state, merge, discussion, checks, action string
	}{
		{"failed pipeline allowed", "opened", "mergeable", "true", "[]", ""},
		{"approval", "opened", "not_approved", "true", "[]", "approval"},
		{"conflicts", "opened", "conflict", "true", "[]", "conflict"},
		{"unknown mergeability", "opened", "", "true", "[]", "refresh"},
		{"other merge status", "opened", "draft_status", "true", "[]", "requirements"},
		{"ci status allowed", "opened", "ci_must_pass", "true", "[]", ""},
		{"discussion missing", "opened", "mergeable", "null", "[]", "discussion"},
		{"discussion unresolved", "opened", "mergeable", "false", "[]", "resolve"},
		{"state missing", "", "mergeable", "true", "[]", "refresh"},
		{"closed", "closed", "mergeable", "true", "[]", "forge"},
		{"merged", "merged", "mergeable", "true", "[]", "reconcile"},
		{"checks failed", "opened", "status_checks_must_pass", "true", `[{"name":"Architecture","status":"failed"}]`, "Architecture"},
		{"checks pending", "opened", "status_checks_must_pass", "true", `[{"name":"Security","status":"pending"}]`, "Security"},
		{"checks generic", "opened", "status_checks_must_pass", "true", "[]", "external"},
		{"check decode failure", "opened", "status_checks_must_pass", "true", "invalid json", "access"},
		{"check API failure", "opened", "status_checks_must_pass", "true", "API_FAIL", "access"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := t.TempDir()
			body := `{"iid":7,"state":"` + tc.state + `","detailed_merge_status":"` + tc.merge + `","blocking_discussions_resolved":` + tc.discussion + `,"head_pipeline":{"status":"failed"}}`
			script := "#!/bin/sh\nif [ \"$1 $2 $3\" = 'mr merge --help' ]; then printf '%s' '--sha'; elif [ \"$1 $2\" = 'mr list' ]; then printf '%s' '[{\"iid\":7,\"state\":\"opened\"}]'; elif [ \"$1\" = 'api' ]; then "
			if tc.checks == "API_FAIL" {
				script += "printf '%s' 'checks unavailable' >&2; exit 1"
			} else {
				script += "printf '%s' '" + tc.checks + "'"
			}
			script += "; else printf '%s' '" + body + "'; fi\n"
			if err := os.WriteFile(filepath.Join(bin, "glab"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin)
			got, err := NewGlabClient(t.TempDir()).MRReadiness(t.Context(), "feature/a", "group/repo", "")
			if err != nil {
				t.Fatal(err)
			}
			checkReadinessRecovery(t, got, tc.action)
			if got.CIState != "failure" {
				t.Errorf("CI evidence changed: %+v", got)
			}
		})
	}
}

func checkReadinessRecovery(t *testing.T, got MRReadiness, action string) {
	t.Helper()
	next := got.NextAction
	if action == "" {
		if !got.Ready || len(got.Blockers) != 0 || next != "" {
			t.Errorf("non-blocking readiness changed: %+v", got)
		}
	} else if got.Ready || len(got.Blockers) == 0 || !strings.Contains(next, action) {
		t.Errorf("blocker recovery = %+v, want action containing %q", got, action)
	}
}
