package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/D1ssolve/wtui/internal/tracker"
)

type jira struct {
	base, email, token string
	http               *http.Client
}

func (j *jira) handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case tracker.MethodHandshake:
		return tracker.Handshake{Protocol: tracker.ProtocolVersion, Methods: []string{
			tracker.MethodListIssues, tracker.MethodGetIssue, tracker.MethodTransition, tracker.MethodComment,
		}}, nil
	case tracker.MethodListIssues:
		var p tracker.ListIssuesParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return j.listIssues(ctx, p)
	case tracker.MethodGetIssue:
		var p tracker.KeyParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return j.getIssue(ctx, p.Key)
	case tracker.MethodTransition:
		var p tracker.TransitionParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return nil, j.transition(ctx, p)
	case tracker.MethodComment:
		var p tracker.CommentParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return nil, j.do(ctx, http.MethodPost, issuePath(p.Key)+"/comment", map[string]any{"body": textADF(p.Body)}, nil)
	}
	return nil, fmt.Errorf("%w %q", tracker.ErrUnknownMethod, method)
}

type jiraFields struct {
	Summary   string `json:"summary"`
	IssueType struct {
		Name string `json:"name"`
	} `json:"issuetype"`
	Status struct {
		Name string `json:"name"`
	} `json:"status"`
	Description *adf `json:"description"`
	Comment     struct {
		Comments []struct {
			Author struct {
				DisplayName string `json:"displayName"`
			} `json:"author"`
			Created string `json:"created"`
			Body    *adf   `json:"body"`
		} `json:"comments"`
	} `json:"comment"`
}

type jiraIssue struct {
	Key    string     `json:"key"`
	Fields jiraFields `json:"fields"`
}

func (j *jira) issue(raw jiraIssue) tracker.Issue {
	return tracker.Issue{
		Key: raw.Key, Summary: raw.Fields.Summary, Type: raw.Fields.IssueType.Name,
		Status: raw.Fields.Status.Name, URL: j.base + "/browse/" + raw.Key,
	}
}

func (j *jira) listIssues(ctx context.Context, p tracker.ListIssuesParams) ([]tracker.Issue, error) {
	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}
	q := url.Values{"jql": {p.Query}, "fields": {"summary,issuetype,status"}, "maxResults": {strconv.Itoa(limit)}}
	var resp struct {
		Issues []jiraIssue `json:"issues"`
	}
	if err := j.do(ctx, http.MethodGet, "/rest/api/3/search/jql?"+q.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	issues := make([]tracker.Issue, 0, len(resp.Issues))
	for _, raw := range resp.Issues {
		issues = append(issues, j.issue(raw))
	}
	return issues, nil
}

func (j *jira) getIssue(ctx context.Context, key string) (tracker.IssueDetail, error) {
	var raw jiraIssue
	if err := j.do(ctx, http.MethodGet, issuePath(key)+"?fields=summary,issuetype,status,description,comment", nil, &raw); err != nil {
		return tracker.IssueDetail{}, err
	}
	detail := tracker.IssueDetail{Issue: j.issue(raw), Description: raw.Fields.Description.text()}
	for _, c := range raw.Fields.Comment.Comments {
		detail.Comments = append(detail.Comments, tracker.Comment{Author: c.Author.DisplayName, Created: c.Created, Body: c.Body.text()})
	}
	return detail, nil
}

// transition accepts either the target status or the transition name, since Jira workflows name them independently.
func (j *jira) transition(ctx context.Context, p tracker.TransitionParams) error {
	var resp struct {
		Transitions []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			To   struct {
				Name string `json:"name"`
			} `json:"to"`
		} `json:"transitions"`
	}
	if err := j.do(ctx, http.MethodGet, issuePath(p.Key)+"/transitions", nil, &resp); err != nil {
		return err
	}
	var names []string
	for _, t := range resp.Transitions {
		if strings.EqualFold(t.To.Name, p.Status) || strings.EqualFold(t.Name, p.Status) {
			return j.do(ctx, http.MethodPost, issuePath(p.Key)+"/transitions", map[string]any{"transition": map[string]string{"id": t.ID}}, nil)
		}
		names = append(names, t.To.Name)
	}
	return fmt.Errorf("%s: no transition to %q; available: %s", p.Key, p.Status, strings.Join(names, ", "))
}

func issuePath(key string) string {
	return "/rest/api/3/issue/" + url.PathEscape(key)
}

func (j *jira) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, j.base+path, reader)
	if err != nil {
		return err
	}
	req.SetBasicAuth(j.email, j.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := j.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return jiraError(resp.StatusCode, data)
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

func jiraError(status int, data []byte) error {
	var body struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
	}
	_ = json.Unmarshal(data, &body)
	msgs := body.ErrorMessages
	for field, msg := range body.Errors {
		msgs = append(msgs, field+": "+msg)
	}
	hint := ""
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		hint = " (check JIRA_EMAIL and JIRA_API_TOKEN)"
	}
	if len(msgs) == 0 {
		return fmt.Errorf("jira: HTTP %d%s", status, hint)
	}
	return errors.New("jira: HTTP " + strconv.Itoa(status) + ": " + strings.Join(msgs, "; ") + hint)
}

// adf is a node of Atlassian Document Format, the rich-text JSON Jira Cloud uses for descriptions and comments.
type adf struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Content []adf  `json:"content"`
}

func (n *adf) text() string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	n.write(&b)
	return strings.TrimSpace(b.String())
}

func (n *adf) write(b *strings.Builder) {
	switch n.Type {
	case "text":
		b.WriteString(n.Text)
	case "hardBreak":
		b.WriteString("\n")
	case "listItem":
		b.WriteString("- ")
	}
	for i := range n.Content {
		n.Content[i].write(b)
	}
	switch n.Type {
	case "paragraph", "heading", "codeBlock", "blockquote":
		b.WriteString("\n")
	}
}

func textADF(s string) map[string]any {
	var paragraphs []any
	for _, line := range strings.Split(s, "\n") {
		p := map[string]any{"type": "paragraph"}
		if line != "" {
			p["content"] = []any{map[string]any{"type": "text", "text": line}}
		}
		paragraphs = append(paragraphs, p)
	}
	return map[string]any{"type": "doc", "version": 1, "content": paragraphs}
}
