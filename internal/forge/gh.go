package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type GhClient struct {
	worktreePath string
}

type ghCheck struct {
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	State      string `json:"state"`
}

type ghPR struct {
	Number            int       `json:"number"`
	State             string    `json:"state"`
	URL               string    `json:"url"`
	HeadRefName       string    `json:"headRefName"`
	BaseRefName       string    `json:"baseRefName"`
	HeadRefOID        string    `json:"headRefOid"`
	Mergeable         string    `json:"mergeable"`
	ReviewDecision    string    `json:"reviewDecision"`
	StatusCheckRollup []ghCheck `json:"statusCheckRollup"`
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
	number, err := numberFromURL(url, "/pull/")
	if err != nil {
		return MRInfo{}, &ForgeError{Category: ErrCategoryParseError, Cause: fmt.Errorf("gh pr create: invalid PR URL: %w", err), Stderr: strings.TrimSpace(stdout)}
	}

	return MRInfo{
		Number:       number,
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

func (c *GhClient) MRReadiness(ctx context.Context, sourceBranch, repo, worktreePath string) (MRReadiness, error) {
	const fields = "number,title,state,url,headRefOid,mergeable,reviewDecision,statusCheckRollup,headRefName,baseRefName"
	args := []string{"pr", "list", "--head", sourceBranch, "--json", fields, "--repo", repo}
	stdout, _, err := c.run(ctx, pickWorktree(c.worktreePath, worktreePath), args...)
	if err != nil {
		return MRReadiness{}, err
	}

	var raw []ghPR
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		return MRReadiness{}, &ForgeError{Category: ErrCategoryParseError, Cause: err, Stderr: strings.TrimSpace(stdout)}
	}
	if len(raw) == 0 {
		return MRReadiness{SourceBranch: sourceBranch, SupportsSHAPin: true, Blockers: []string{"pull request not found"}}, nil
	}

	return ghReadiness(raw[0], sourceBranch), nil
}

func (c *GhClient) MRReadinessByNumber(ctx context.Context, number int, repo, worktreePath string) (MRReadiness, error) {
	const fields = "number,state,url,headRefOid,mergeable,reviewDecision,statusCheckRollup"
	stdout, _, err := c.run(ctx, pickWorktree(c.worktreePath, worktreePath), "pr", "view", strconv.Itoa(number), "--json", fields, "--repo", repo)
	if err != nil {
		return MRReadiness{}, err
	}

	var pr ghPR
	if err := json.Unmarshal([]byte(stdout), &pr); err != nil {
		return MRReadiness{}, &ForgeError{Category: ErrCategoryParseError, Cause: err, Stderr: strings.TrimSpace(stdout)}
	}
	return ghReadiness(pr, ""), nil
}

func ghReadiness(pr ghPR, sourceBranch string) MRReadiness {
	if pr.HeadRefName == "" {
		pr.HeadRefName = sourceBranch
	}
	state := strings.ToLower(pr.State)
	approved := strings.EqualFold(pr.ReviewDecision, "APPROVED")
	ciState := ghChecksState(pr.StatusCheckRollup)
	mergeable := strings.EqualFold(pr.Mergeable, "MERGEABLE")
	blockers := make([]string, 0, 4)
	if state != "open" {
		blockers = append(blockers, "pull request is "+state)
	}
	if !approved {
		blockers = append(blockers, "not approved")
	}
	switch ciState {
	case "pending":
		blockers = append(blockers, "checks pending")
	case "failure":
		blockers = append(blockers, "checks failing")
	}
	if !mergeable {
		if strings.EqualFold(pr.Mergeable, "CONFLICTING") {
			blockers = append(blockers, "conflicts")
		} else {
			blockers = append(blockers, "mergeability unknown")
		}
	}

	return MRReadiness{
		Number:         pr.Number,
		State:          state,
		URL:            pr.URL,
		SourceBranch:   pr.HeadRefName,
		TargetBranch:   pr.BaseRefName,
		HeadSHA:        pr.HeadRefOID,
		Approved:       approved,
		CIState:        ciState,
		Mergeable:      mergeable,
		Ready:          len(blockers) == 0,
		Blockers:       blockers,
		SupportsSHAPin: true,
	}
}

func ghChecksState(checks []ghCheck) string {
	pending := false
	for _, check := range checks {
		state := strings.ToUpper(check.State)
		if state != "" {
			switch state {
			case "SUCCESS", "NEUTRAL", "SKIPPED":
				continue
			case "PENDING", "EXPECTED":
				pending = true
				continue
			default:
				return "failure"
			}
		}
		if !strings.EqualFold(check.Status, "COMPLETED") {
			pending = true
			continue
		}
		switch strings.ToUpper(check.Conclusion) {
		case "SUCCESS", "NEUTRAL", "SKIPPED":
		default:
			return "failure"
		}
	}
	if pending {
		return "pending"
	}
	return "success"
}

func (c *GhClient) MergeMR(ctx context.Context, params MergeMRParams) (MRMergeResult, error) {
	worktreePath := pickWorktree(c.worktreePath, params.WorktreePath)
	number := strconv.Itoa(params.Number)
	method := params.Method
	if method == "" {
		method = "merge"
	}
	args := []string{"pr", "merge", number, "--" + method, "--repo", params.Repo}
	if params.ExpectedHeadSHA != "" {
		args = append(args, "--match-head-commit", params.ExpectedHeadSHA)
	}
	if _, _, err := c.run(ctx, worktreePath, args...); err != nil {
		return MRMergeResult{}, err
	}

	result := MRMergeResult{Merged: true}
	stdout, _, err := c.run(ctx, worktreePath, "pr", "view", number, "--json", "mergeCommit", "--repo", params.Repo)
	if err != nil {
		return result, nil
	}
	var view struct {
		MergeCommit *struct {
			OID string `json:"oid"`
		} `json:"mergeCommit"`
	}
	if json.Unmarshal([]byte(stdout), &view) == nil && view.MergeCommit != nil {
		result.MergeCommitSHA = view.MergeCommit.OID
	}
	return result, nil
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
