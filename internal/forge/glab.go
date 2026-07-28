package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type GlabClient struct {
	worktreePath       string
	shaPinOnce         sync.Once
	supportsSHAPinFlag bool
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
	number, err := numberFromURL(url, "/merge_requests/")
	if err != nil {
		return MRInfo{}, &ForgeError{Category: ErrCategoryParseError, Cause: fmt.Errorf("glab mr create: invalid MR URL: %w", err), Stderr: strings.TrimSpace(stdout)}
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

func (c *GlabClient) MRReadiness(ctx context.Context, sourceBranch, repo, worktreePath string) (MRReadiness, error) {
	worktreePath = pickWorktree(c.worktreePath, worktreePath)
	supportsSHAPin := c.supportsSHAPin(ctx, worktreePath)
	stdout, _, err := c.run(ctx, worktreePath, "mr", "list", "--source-branch", sourceBranch, "--output", "json", "--repo", repo)
	if err != nil {
		return MRReadiness{}, err
	}

	type listedMR struct {
		IID    int    `json:"iid"`
		ID     int    `json:"id"`
		Number int    `json:"number"`
		State  string `json:"state"`
	}
	var listed []listedMR
	if err := json.Unmarshal([]byte(stdout), &listed); err != nil {
		return MRReadiness{}, &ForgeError{Category: ErrCategoryParseError, Cause: err, Stderr: strings.TrimSpace(stdout)}
	}

	number := 0
	for _, mr := range listed {
		if !strings.EqualFold(mr.State, "opened") && !strings.EqualFold(mr.State, "open") {
			continue
		}
		number = mr.Number
		if number == 0 {
			number = mr.IID
		}
		if number == 0 {
			number = mr.ID
		}
		if number != 0 {
			break
		}
	}
	if number == 0 {
		return MRReadiness{SourceBranch: sourceBranch, SupportsSHAPin: supportsSHAPin, Blockers: []string{"merge request not found"}}, nil
	}
	return c.mrReadinessByNumber(ctx, number, repo, worktreePath, sourceBranch, supportsSHAPin)
}

func (c *GlabClient) MRReadinessByNumber(ctx context.Context, number int, repo, worktreePath string) (MRReadiness, error) {
	worktreePath = pickWorktree(c.worktreePath, worktreePath)
	return c.mrReadinessByNumber(ctx, number, repo, worktreePath, "", c.supportsSHAPin(ctx, worktreePath))
}

func (c *GlabClient) mrReadinessByNumber(ctx context.Context, number int, repo, worktreePath, sourceBranch string, supportsSHAPin bool) (MRReadiness, error) {
	stdout, _, err := c.run(ctx, worktreePath, "mr", "view", strconv.Itoa(number), "--output", "json", "--repo", repo)
	if err != nil {
		return MRReadiness{}, err
	}
	type pipeline struct {
		Status string `json:"status"`
	}
	var detail struct {
		IID          int    `json:"iid"`
		ID           int    `json:"id"`
		Number       int    `json:"number"`
		State        string `json:"state"`
		WebURL       string `json:"web_url"`
		URL          string `json:"url"`
		SourceBranch string `json:"source_branch"`
		TargetBranch string `json:"target_branch"`
		SHA          string `json:"sha"`
		DiffRefs     struct {
			HeadSHA string `json:"head_sha"`
		} `json:"diff_refs"`
		BlockingDiscussionsResolved *bool     `json:"blocking_discussions_resolved"`
		DetailedMergeStatus         string    `json:"detailed_merge_status"`
		MergeStatus                 string    `json:"merge_status"`
		HasConflicts                bool      `json:"has_conflicts"`
		HeadPipeline                *pipeline `json:"head_pipeline"`
		Pipeline                    *pipeline `json:"pipeline"`
	}
	if err := json.Unmarshal([]byte(stdout), &detail); err != nil {
		return MRReadiness{}, &ForgeError{Category: ErrCategoryParseError, Cause: err, Stderr: strings.TrimSpace(stdout)}
	}

	if detail.Number != 0 {
		number = detail.Number
	} else if detail.IID != 0 {
		number = detail.IID
	} else if detail.ID != 0 {
		number = detail.ID
	}
	state := strings.ToLower(detail.State)
	if state == "opened" {
		state = "open"
	}
	url := detail.URL
	if url == "" {
		url = detail.WebURL
	}
	headSHA := detail.SHA
	if headSHA == "" {
		headSHA = detail.DiffRefs.HeadSHA
	}
	if detail.SourceBranch == "" {
		detail.SourceBranch = sourceBranch
	}
	mergeStatus := strings.ToLower(detail.DetailedMergeStatus)
	if mergeStatus == "" {
		mergeStatus = strings.ToLower(detail.MergeStatus)
	}
	mergeable := !detail.HasConflicts && (mergeStatus == "mergeable" || mergeStatus == "can_be_merged")
	approved := mergeStatus != "" && mergeStatus != "not_approved"

	ciState := "success"
	pipelineStatus := ""
	pipelinePresent := false
	if detail.HeadPipeline != nil {
		pipelinePresent = true
		pipelineStatus = strings.ToLower(detail.HeadPipeline.Status)
	} else if detail.Pipeline != nil {
		pipelinePresent = true
		pipelineStatus = strings.ToLower(detail.Pipeline.Status)
	}
	if pipelinePresent && pipelineStatus != "success" {
		switch pipelineStatus {
		case "created", "waiting_for_resource", "preparing", "pending", "running", "scheduled", "manual":
			ciState = "pending"
		default:
			ciState = "failure"
		}
	}

	blockers := make([]string, 0, 4)
	if state != "open" {
		if state == "" {
			blockers = append(blockers, "merge request state unknown")
		} else {
			blockers = append(blockers, "merge request is "+state)
		}
	}
	if !mergeable {
		switch mergeStatus {
		case "not_approved":
			blockers = append(blockers, "not approved")
		case "conflict", "conflicts", "cannot_be_merged":
			blockers = append(blockers, "conflicts")
		case "ci_must_pass", "ci_still_running", "checking", "unchecked", "blocked_status":
			// ponytail: approval-only policy, CI statuses do not block merge readiness
		case "":
			blockers = append(blockers, "mergeability unknown")
		default:
			blockers = append(blockers, "merge blocked: "+strings.ReplaceAll(mergeStatus, "_", " "))
		}
	}
	if detail.BlockingDiscussionsResolved == nil {
		blockers = append(blockers, "discussion status unknown")
	} else if !*detail.BlockingDiscussionsResolved {
		blockers = append(blockers, "unresolved discussions")
	}

	return MRReadiness{
		Number:         number,
		State:          state,
		URL:            url,
		SourceBranch:   detail.SourceBranch,
		TargetBranch:   detail.TargetBranch,
		HeadSHA:        headSHA,
		Approved:       approved,
		CIState:        ciState,
		Mergeable:      mergeable,
		Ready:          len(blockers) == 0,
		Blockers:       blockers,
		SupportsSHAPin: supportsSHAPin,
	}, nil
}

func (c *GlabClient) MergeMR(ctx context.Context, params MergeMRParams) (MRMergeResult, error) {
	worktreePath := pickWorktree(c.worktreePath, params.WorktreePath)
	number := strconv.Itoa(params.Number)
	args := []string{"mr", "merge", number, "--auto-merge=false", "--yes", "--repo", params.Repo}
	switch params.Method {
	case "squash":
		args = append(args, "--squash")
	case "rebase":
		args = append(args, "--rebase")
	}
	if params.ExpectedHeadSHA != "" && c.supportsSHAPin(ctx, worktreePath) {
		args = append(args, "--sha", params.ExpectedHeadSHA)
	}
	if _, _, err := c.run(ctx, worktreePath, args...); err != nil {
		return MRMergeResult{}, err
	}

	result := MRMergeResult{Merged: true}
	stdout, _, err := c.run(ctx, worktreePath, "mr", "view", number, "--output", "json", "--repo", params.Repo)
	if err != nil {
		return result, nil
	}
	var view struct {
		MergeCommitSHA string `json:"merge_commit_sha"`
	}
	if json.Unmarshal([]byte(stdout), &view) == nil {
		result.MergeCommitSHA = view.MergeCommitSHA
	}
	return result, nil
}

func (c *GlabClient) supportsSHAPin(ctx context.Context, worktreePath string) bool {
	c.shaPinOnce.Do(func() {
		stdout, stderr, err := c.run(ctx, worktreePath, "mr", "merge", "--help")
		c.supportsSHAPinFlag = err == nil && strings.Contains(stdout+"\n"+stderr, "--sha")
	})
	return c.supportsSHAPinFlag
}

func (c *GlabClient) PipelineStatus(ctx context.Context, branch, repo string) ([]PipelineStatus, error) {
	args := []string{"ci", "status", "--branch", branch, "--output", "json", "--repo", repo}
	stdout, _, err := c.run(ctx, c.worktreePath, args...)
	if err != nil {
		return nil, err
	}

	type glabPipeline struct {
		ID     any    `json:"id"`
		Status string `json:"status"`
		Ref    string `json:"ref"`
		Name   string `json:"name"`
		WebURL string `json:"web_url"`
	}
	type glabCIStatus struct {
		Pipeline *glabPipeline `json:"pipeline"`
	}

	var raw glabCIStatus
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		return nil, &ForgeError{Category: ErrCategoryParseError, Cause: err, Stderr: strings.TrimSpace(stdout)}
	}
	if raw.Pipeline == nil {
		return nil, nil
	}

	p := raw.Pipeline
	workflowName := p.Name
	if workflowName == "" {
		workflowName = "pipeline"
	}

	return []PipelineStatus{{
		ID:           fmt.Sprint(p.ID),
		Status:       p.Status,
		Branch:       p.Ref,
		URL:          p.WebURL,
		WorkflowName: workflowName,
	}}, nil
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

func numberFromURL(rawURL, marker string) (int, error) {
	path := strings.TrimSuffix(rawURL, "/")
	i := strings.LastIndex(path, marker)
	if i < 0 {
		return 0, fmt.Errorf("missing %q path", marker)
	}
	number, err := strconv.Atoi(path[i+len(marker):])
	if err != nil || number <= 0 {
		return 0, fmt.Errorf("invalid number in %q", rawURL)
	}
	return number, nil
}

func pickWorktree(defaultPath, override string) string {
	if strings.TrimSpace(override) != "" {
		return override
	}
	return defaultPath
}
