package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

type GlabClient struct {
	worktreePath string
}

func NewGlabClient(worktreePath string) *GlabClient {
	return &GlabClient{worktreePath: worktreePath}
}

func (c *GlabClient) Provider() ForgeProvider {
	return ForgeProviderGitLab
}

func (c *GlabClient) IsAvailable(ctx context.Context) bool {
	return IsGlabAvailable(ctx)
}

func (c *GlabClient) CreateMR(ctx context.Context, params CreateMRParams) (MRInfo, error) {
	args := []string{
		"mr", "create",
		"--source-branch", params.SourceBranch,
		"--target-branch", params.TargetBranch,
		"--title", params.Title,
		"--description", params.Description,
		"--repo", params.Repo,
		"--yes",
	}

	if params.Draft {
		args = append(args, "--draft")
	}
	for _, label := range params.Labels {
		if strings.TrimSpace(label) != "" {
			args = append(args, "--label", label)
		}
	}
	for _, reviewer := range params.Reviewers {
		if strings.TrimSpace(reviewer) != "" {
			args = append(args, "--reviewer", reviewer)
		}
	}
	if params.RemoveSource {
		args = append(args, "--remove-source-branch")
	}

	stdout, stderr, err := c.run(ctx, pickWorktree(c.worktreePath, params.WorktreePath), args...)
	if err != nil {
		return MRInfo{}, err
	}

	url := extractFirstURL(stdout)
	if url == "" {
		url = extractFirstURL(stderr)
	}
	if url == "" {
		return MRInfo{}, &ForgeError{Category: ErrCategoryParseError, Cause: errors.New("glab mr create: missing MR URL in output"), Stderr: strings.TrimSpace(stdout)}
	}

	return MRInfo{
		Title:        params.Title,
		State:        "open",
		URL:          url,
		SourceBranch: params.SourceBranch,
		TargetBranch: params.TargetBranch,
	}, nil
}

func (c *GlabClient) MRStatus(ctx context.Context, sourceBranch, repo string) ([]MRInfo, error) {
	args := []string{"mr", "list", "--source-branch", sourceBranch, "--output", "json", "--repo", repo}
	stdout, _, err := c.run(ctx, c.worktreePath, args...)
	if err != nil {
		return nil, err
	}

	type glabMR struct {
		IID          int    `json:"iid"`
		ID           int    `json:"id"`
		Number       int    `json:"number"`
		Title        string `json:"title"`
		State        string `json:"state"`
		WebURL       string `json:"web_url"`
		URL          string `json:"url"`
		SourceBranch string `json:"source_branch"`
		TargetBranch string `json:"target_branch"`
	}

	var raw []glabMR
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		return nil, &ForgeError{Category: ErrCategoryParseError, Cause: err, Stderr: strings.TrimSpace(stdout)}
	}

	out := make([]MRInfo, 0, len(raw))
	for _, item := range raw {
		num := item.Number
		if num == 0 {
			num = item.IID
		}
		if num == 0 {
			num = item.ID
		}

		url := item.URL
		if url == "" {
			url = item.WebURL
		}

		out = append(out, MRInfo{
			Number:       num,
			Title:        item.Title,
			State:        item.State,
			URL:          url,
			SourceBranch: item.SourceBranch,
			TargetBranch: item.TargetBranch,
		})
	}

	return out, nil
}

func (c *GlabClient) PipelineStatus(ctx context.Context, branch, repo string) ([]PipelineStatus, error) {
	args := []string{"ci", "status", "--branch", branch, "--output", "json", "--repo", repo}
	stdout, _, err := c.run(ctx, c.worktreePath, args...)
	if err != nil {
		return nil, err
	}

	type glabPipeline struct {
		ID         any    `json:"id"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		Ref        string `json:"ref"`
		Branch     string `json:"branch"`
		WebURL     string `json:"web_url"`
		URL        string `json:"url"`
		Name       string `json:"name"`
		Pipeline   string `json:"pipeline"`
	}

	var raw []glabPipeline
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		return nil, &ForgeError{Category: ErrCategoryParseError, Cause: err, Stderr: strings.TrimSpace(stdout)}
	}

	out := make([]PipelineStatus, 0, len(raw))
	for _, item := range raw {
		b := item.Branch
		if b == "" {
			b = item.Ref
		}
		url := item.URL
		if url == "" {
			url = item.WebURL
		}
		wf := item.Name
		if wf == "" {
			wf = item.Pipeline
		}

		out = append(out, PipelineStatus{
			ID:           fmt.Sprint(item.ID),
			Status:       item.Status,
			Conclusion:   item.Conclusion,
			Branch:       b,
			URL:          url,
			WorkflowName: wf,
		})
	}

	return out, nil
}

func (c *GlabClient) TriggerPipeline(ctx context.Context, params TriggerPipelineParams) error {
	args := []string{"ci", "run", "--branch", params.Branch, "--repo", params.Repo}
	_, _, err := c.run(ctx, pickWorktree(c.worktreePath, params.WorktreePath), args...)
	return err
}

func (c *GlabClient) ListIssues(ctx context.Context, params ListIssuesParams) ([]IssueInfo, error) {
	args := []string{"issue", "list", "--output", "json", "--repo", params.Repo}
	stdout, _, err := c.run(ctx, pickWorktree(c.worktreePath, params.WorktreePath), args...)
	if err != nil {
		return nil, err
	}

	type glabLabel struct {
		Title string `json:"title"`
		Name  string `json:"name"`
	}
	type glabIssue struct {
		IID    int         `json:"iid"`
		ID     int         `json:"id"`
		Number int         `json:"number"`
		Title  string      `json:"title"`
		State  string      `json:"state"`
		WebURL string      `json:"web_url"`
		URL    string      `json:"url"`
		Labels []glabLabel `json:"labels"`
	}

	var raw []glabIssue
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		return nil, &ForgeError{Category: ErrCategoryParseError, Cause: err, Stderr: strings.TrimSpace(stdout)}
	}

	out := make([]IssueInfo, 0, len(raw))
	for _, item := range raw {
		num := item.Number
		if num == 0 {
			num = item.IID
		}
		if num == 0 {
			num = item.ID
		}
		url := item.URL
		if url == "" {
			url = item.WebURL
		}

		labels := make([]string, 0, len(item.Labels))
		for _, lbl := range item.Labels {
			name := lbl.Name
			if name == "" {
				name = lbl.Title
			}
			if name != "" {
				labels = append(labels, name)
			}
		}

		out = append(out, IssueInfo{
			Number: num,
			Title:  item.Title,
			State:  item.State,
			URL:    url,
			Labels: labels,
		})
	}

	return out, nil
}

func (c *GlabClient) run(ctx context.Context, worktreePath string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "glab", args...)
	cmd.Dir = worktreePath

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", strings.TrimSpace(stderr.String()), classifyForgeExecError(err, strings.TrimSpace(stderr.String()))
	}

	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), nil
}

func classifyForgeExecError(err error, stderr string) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	if errors.Is(err, exec.ErrNotFound) {
		return &ForgeError{Category: ErrCategoryNotInstalled, Cause: fmt.Errorf("%w: %w", ErrForgeUnavailable, err), Stderr: stderr}
	}

	lower := strings.ToLower(stderr)
	if lower == "" {
		lower = strings.ToLower(err.Error())
	}

	if strings.Contains(lower, "not authenticated") || strings.Contains(lower, "unauthorized") || strings.Contains(lower, "401") {
		return &ForgeError{Category: ErrCategoryAuthError, Cause: err, Stderr: stderr}
	}

	if strings.Contains(lower, "timeout") || strings.Contains(lower, "no such host") || strings.Contains(lower, "connection") || strings.Contains(lower, "temporary failure") {
		return &ForgeError{Category: ErrCategoryNetwork, Cause: err, Stderr: stderr}
	}

	return &ForgeError{Category: ErrCategoryUnknown, Cause: err, Stderr: stderr}
}

var firstURLRe = regexp.MustCompile(`https?://[^\s]+`)

func extractFirstURL(s string) string {
	m := firstURLRe.FindString(strings.TrimSpace(s))
	return strings.TrimRight(m, ").,")
}

func pickWorktree(defaultPath, override string) string {
	if strings.TrimSpace(override) != "" {
		return override
	}
	return defaultPath
}
