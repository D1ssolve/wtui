package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/tracker"
)

func fakeJira(t *testing.T, routes map[string]string, posted *[]string) *jira {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); !ok || user != "me@x" || pass != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			*posted = append(*posted, r.URL.Path+" "+string(body))
			w.WriteHeader(http.StatusNoContent)
			return
		}
		resp, ok := routes[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"errorMessages":["Issue does not exist"]}`)
			return
		}
		if r.URL.Path == "/rest/api/3/search/jql" && r.URL.Query().Get("jql") != "project = WT" {
			t.Errorf("jql = %q", r.URL.Query().Get("jql"))
		}
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(srv.Close)
	return &jira{base: srv.URL, email: "me@x", token: "tok", http: srv.Client()}
}

func call(t *testing.T, j *jira, method string, params any) (any, error) {
	raw, _ := json.Marshal(params)
	return j.handle(context.Background(), method, raw)
}

func TestJira_ListAndGet_MapFields(t *testing.T) {
	j := fakeJira(t, map[string]string{
		"/rest/api/3/search/jql": `{"issues":[{"key":"WT-1","fields":{"summary":"Fix it","issuetype":{"name":"Bug"},"status":{"name":"To Do"}}}]}`,
		"/rest/api/3/issue/WT-1": `{"key":"WT-1","fields":{"summary":"Fix it","issuetype":{"name":"Bug"},"status":{"name":"To Do"},
			"description":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Line one"},{"type":"hardBreak"},{"type":"text","text":"two"}]},
				{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"item"}]}]}]}]},
			"comment":{"comments":[{"author":{"displayName":"Ann"},"created":"2026-11-01","body":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"ok"}]}]}}]}}}`,
	}, nil)
	got, err := call(t, j, tracker.MethodListIssues, tracker.ListIssuesParams{Query: "project = WT"})
	issues := got.([]tracker.Issue)
	if err != nil || len(issues) != 1 || issues[0] != (tracker.Issue{Key: "WT-1", Summary: "Fix it", Type: "Bug", Status: "To Do", URL: j.base + "/browse/WT-1"}) {
		t.Fatalf("issues = %+v, %v", issues, err)
	}
	got, err = call(t, j, tracker.MethodGetIssue, tracker.KeyParams{Key: "WT-1"})
	d := got.(tracker.IssueDetail)
	if err != nil || d.Description != "Line one\ntwo\n- item" || len(d.Comments) != 1 || d.Comments[0].Author != "Ann" || d.Comments[0].Body != "ok" {
		t.Fatalf("detail = %+v, %v", d, err)
	}
}

func TestJira_Transition_MatchesTargetStatus(t *testing.T) {
	var posted []string
	j := fakeJira(t, map[string]string{
		"/rest/api/3/issue/WT-1/transitions": `{"transitions":[{"id":"11","name":"Start","to":{"name":"In Progress"}},{"id":"31","name":"Finish","to":{"name":"Done"}}]}`,
	}, &posted)
	if _, err := call(t, j, tracker.MethodTransition, tracker.TransitionParams{Key: "WT-1", Status: "in progress"}); err != nil {
		t.Fatal(err)
	}
	if len(posted) != 1 || posted[0] != `/rest/api/3/issue/WT-1/transitions {"transition":{"id":"11"}}` {
		t.Fatalf("posted = %v", posted)
	}
	_, err := call(t, j, tracker.MethodTransition, tracker.TransitionParams{Key: "WT-1", Status: "Review"})
	if err == nil || !strings.Contains(err.Error(), "available: In Progress, Done") {
		t.Fatalf("err = %v", err)
	}
}

func TestJira_Comment_PostsADF(t *testing.T) {
	var posted []string
	j := fakeJira(t, nil, &posted)
	if _, err := call(t, j, tracker.MethodComment, tracker.CommentParams{Key: "WT-1", Body: "MR: x"}); err != nil {
		t.Fatal(err)
	}
	if len(posted) != 1 || !strings.Contains(posted[0], `{"content":[{"text":"MR: x","type":"text"}],"type":"paragraph"}`) {
		t.Fatalf("posted = %v", posted)
	}
}

func TestJira_Errors_CarryJiraMessageAndAuthHint(t *testing.T) {
	j := fakeJira(t, map[string]string{}, nil)
	_, err := call(t, j, tracker.MethodGetIssue, tracker.KeyParams{Key: "WT-9"})
	if err == nil || !strings.Contains(err.Error(), "HTTP 404: Issue does not exist") {
		t.Fatalf("err = %v", err)
	}
	j.token = "bad"
	_, err = call(t, j, tracker.MethodGetIssue, tracker.KeyParams{Key: "WT-9"})
	if err == nil || !strings.Contains(err.Error(), "JIRA_API_TOKEN") {
		t.Fatalf("err = %v", err)
	}
}

func TestFromEnv_RequiresCredentials(t *testing.T) {
	t.Setenv("JIRA_BASE_URL", "https://x.atlassian.net/")
	t.Setenv("JIRA_EMAIL", "")
	t.Setenv("JIRA_API_TOKEN", "t")
	if _, err := fromEnv(); err == nil || !strings.Contains(err.Error(), "JIRA_EMAIL") {
		t.Fatalf("err = %v", err)
	}
	t.Setenv("JIRA_EMAIL", "e")
	if j, err := fromEnv(); err != nil || j.base != "https://x.atlassian.net" {
		t.Fatalf("j = %+v, %v", j, err)
	}
}
