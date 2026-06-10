package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type GhClient struct {
	worktreePath string
}

func NewGhClient(worktreePath string) *GhClient {
	return &GhClient{worktreePath: worktreePath}
}

func (c *GhClient) Provider() ForgeProvider {
	return ForgeProviderGitHub
}

func (c *GhClient) IsAvailable(ctx context.Context) bool {
	return IsGhAvailable(ctx)
}

func (c *GhClient) CreateMR(ctx context.Context, params CreateMRParams) (MRInfo, error) {
	args := []string{
		"pr", "create",
		"--base", params.TargetBranch,
		"--head", params.SourceBranch,
		"--title", params.Title,
		"--body", params.Description,
		"--repo", params.Repo,
	}
	if params.Draft {
		args = append(args, "--draft")
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
		return MRInfo{}, &ForgeError{Category: ErrCategoryParseError, Cause: fmt.Errorf("gh pr create: missing PR URL in output"), Stderr: strings.TrimSpace(stdout)}
	}

	return MRInfo{
		Title:        params.Title,
		State:        "open",
		URL:          url,
		SourceBranch: params.SourceBranch,
		TargetBranch: params.TargetBranch,
	}, nil
}

func (c *GhClient) MRStatus(ctx context.Context, sourceBranch, repo string) ([]MRInfo, error) {
	args := []string{"pr", "list", "--head", sourceBranch, "--json", "number,title,state,url", "--repo", repo}
	stdout, _, err := c.run(ctx, c.worktreePath, args...)
	if err != nil {
		return nil, err
	}

	type ghPR struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		State  string `json:"state"`
		URL    string `json:"url"`
	}

	var raw []ghPR
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		return nil, &ForgeError{Category: ErrCategoryParseError, Cause: err, Stderr: strings.TrimSpace(stdout)}
	}

	out := make([]MRInfo, 0, len(raw))
	for _, item := range raw {
		out = append(out, MRInfo{
			Number:       item.Number,
			Title:        item.Title,
			State:        strings.ToLower(item.State),
			URL:          item.URL,
			SourceBranch: sourceBranch,
		})
	}

	return out, nil
}

func (c *GhClient) PipelineStatus(ctx context.Context, branch, repo string) ([]PipelineStatus, error) {
	args := []string{"run", "list", "--branch", branch, "--json", "status,conclusion,url,workflowName", "--repo", repo}
	stdout, _, err := c.run(ctx, c.worktreePath, args...)
	if err != nil {
		return nil, err
	}

	type ghRun struct {
		Status       string `json:"status"`
		Conclusion   string `json:"conclusion"`
		URL          string `json:"url"`
		WorkflowName string `json:"workflowName"`
	}

	var raw []ghRun
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		return nil, &ForgeError{Category: ErrCategoryParseError, Cause: err, Stderr: strings.TrimSpace(stdout)}
	}

	out := make([]PipelineStatus, 0, len(raw))
	for _, item := range raw {
		out = append(out, PipelineStatus{
			Status:       item.Status,
			Conclusion:   item.Conclusion,
			Branch:       branch,
			URL:          item.URL,
			WorkflowName: item.WorkflowName,
		})
	}

	return out, nil
}

func (c *GhClient) TriggerPipeline(ctx context.Context, params TriggerPipelineParams) error {
	workflow := strings.TrimSpace(params.WorkflowFile)
	if workflow == "" {
		return &ForgeError{Category: ErrCategoryParseError, Cause: fmt.Errorf("workflow file is required for gh workflow run")}
	}

	args := []string{"workflow", "run", workflow}
	args = append(args, "--ref", params.Branch, "--repo", params.Repo)

	for k, v := range params.Variables {
		args = append(args, "-f", k+"="+v)
	}

	_, _, err := c.run(ctx, pickWorktree(c.worktreePath, params.WorktreePath), args...)
	return err
}

func (c *GhClient) ListIssues(ctx context.Context, params ListIssuesParams) ([]IssueInfo, error) {
	args := []string{"issue", "list", "--state", "open", "--json", "number,title,state,labels,url", "--repo", params.Repo}
	stdout, _, err := c.run(ctx, pickWorktree(c.worktreePath, params.WorktreePath), args...)
	if err != nil {
		return nil, err
	}

	type ghLabel struct {
		Name string `json:"name"`
	}
	type ghIssue struct {
		Number int       `json:"number"`
		Title  string    `json:"title"`
		State  string    `json:"state"`
		URL    string    `json:"url"`
		Labels []ghLabel `json:"labels"`
	}

	var raw []ghIssue
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		return nil, &ForgeError{Category: ErrCategoryParseError, Cause: err, Stderr: strings.TrimSpace(stdout)}
	}

	out := make([]IssueInfo, 0, len(raw))
	for _, item := range raw {
		labels := make([]string, 0, len(item.Labels))
		for _, lbl := range item.Labels {
			if lbl.Name != "" {
				labels = append(labels, lbl.Name)
			}
		}
		out = append(out, IssueInfo{
			Number: item.Number,
			Title:  item.Title,
			State:  strings.ToLower(item.State),
			URL:    item.URL,
			Labels: labels,
		})
	}

	return out, nil
}

func (c *GhClient) run(ctx context.Context, worktreePath string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
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
