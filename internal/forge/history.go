package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// HistoryClient includes closed and merged requests without changing MRStatus.
type HistoryClient interface {
	MRHistory(context.Context, string, string) ([]MRInfo, error)
}

func (c *GlabClient) MRHistory(ctx context.Context, branch, repo string) ([]MRInfo, error) {
	var result []MRInfo
	for page := 1; ; page++ {
		out, _, err := c.run(ctx, c.worktreePath, "mr", "list", "--all", "--source-branch", branch, "--output", "json", "--repo", repo, "--per-page", "100", "--page", strconv.Itoa(page))
		if err != nil {
			return nil, err
		}
		var rows []struct {
			Number int    `json:"iid"`
			State  string `json:"state"`
			Source string `json:"source_branch"`
			Target string `json:"target_branch"`
			URL    string `json:"web_url"`
		}
		if err := json.Unmarshal([]byte(out), &rows); err != nil {
			return nil, fmt.Errorf("MR history: %w", err)
		}
		if rows == nil {
			return nil, fmt.Errorf("MR history: expected an array, got null")
		}
		for _, r := range rows {
			result = append(result, MRInfo{Number: r.Number, State: r.State, SourceBranch: r.Source, TargetBranch: r.Target, URL: r.URL})
		}
		if len(rows) < 100 {
			return result, nil
		}
	}
}

func (c *GhClient) MRHistory(ctx context.Context, branch, repo string) ([]MRInfo, error) {
	parts := strings.Split(strings.Trim(repo, "/"), "/")
	host := "github.com"
	if len(parts) == 3 {
		host, parts = parts[0], parts[1:]
	}
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid GitHub repository %q", repo)
	}
	var result []MRInfo
	for page := 1; ; page++ {
		endpoint := fmt.Sprintf("repos/%s/%s/pulls?state=all&head=%s&per_page=100&page=%d", url.PathEscape(parts[0]), url.PathEscape(parts[1]), url.QueryEscape(parts[0]+":"+branch), page)
		out, _, err := c.run(ctx, c.worktreePath, "api", "--hostname", host, endpoint)
		if err != nil {
			return nil, err
		}
		var rows []struct {
			Number   int     `json:"number"`
			State    string  `json:"state"`
			MergedAt *string `json:"merged_at"`
			URL      string  `json:"html_url"`
			Head     struct {
				Ref string `json:"ref"`
			} `json:"head"`
			Base struct {
				Ref string `json:"ref"`
			} `json:"base"`
		}
		if err := json.Unmarshal([]byte(out), &rows); err != nil {
			return nil, fmt.Errorf("PR history: %w", err)
		}
		if rows == nil {
			return nil, fmt.Errorf("PR history: expected an array, got null")
		}
		for _, r := range rows {
			if r.MergedAt != nil {
				r.State = "merged"
			}
			result = append(result, MRInfo{Number: r.Number, State: r.State, SourceBranch: r.Head.Ref, TargetBranch: r.Base.Ref, URL: r.URL})
		}
		if len(rows) < 100 {
			return result, nil
		}
	}
}
