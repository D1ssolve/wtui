package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// EnqueueMR submits only to a GitLab merge train. Its response acknowledges
// enrollment (possibly scheduled), not delivery of this numbered MR.
func (c *GlabClient) EnqueueMR(ctx context.Context, params MergeMRParams) error {
	host, project, ok := strings.Cut(params.Repo, "/")
	if !ok || host == "" || !strings.Contains(project, "/") || params.Number <= 0 ||
		strings.TrimSpace(params.ExpectedHeadSHA) == "" || strings.TrimSpace(params.ExpectedTargetBranch) == "" {
		return errors.New("GitLab merge train requires a host-qualified project, MR number, reviewed head and target branch")
	}
	worktree := pickWorktree(c.worktreePath, params.WorktreePath)
	fresh, err := c.MRReadinessByNumber(ctx, params.Number, params.Repo, worktree)
	if err != nil {
		return fmt.Errorf("GitLab merge train preflight: %w", err)
	}
	if fresh.Number != params.Number || fresh.TargetBranch != params.ExpectedTargetBranch || fresh.HeadSHA != params.ExpectedHeadSHA {
		return errors.New("GitLab merge train: MR number, target or reviewed head changed; refresh before retrying")
	}
	if fresh.State != "open" {
		return fmt.Errorf("GitLab merge train: MR !%d is %s; refresh numbered MR status", params.Number, fresh.State)
	}

	endpoint := "projects/" + url.PathEscape(project) + "/merge_trains/merge_requests/" + strconv.Itoa(params.Number)
	args := []string{"api", endpoint, "--hostname", host}
	out, stderr, err := c.run(ctx, worktree, args...)
	if err == nil {
		var car struct {
			MergeRequest struct {
				IID int `json:"iid"`
			} `json:"merge_request"`
			Target string `json:"target_branch"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal([]byte(out), &car); err != nil {
			return fmt.Errorf("decode GitLab merge train status: %w", err)
		}
		if car.MergeRequest.IID != params.Number || car.Target != params.ExpectedTargetBranch {
			return errors.New("GitLab merge train returned a different MR or target")
		}
		switch car.Status {
		case "idle", "fresh", "stale", "merging", "merged", "skip_merged":
			// Even a completed car is not authoritative proof of delivery.
			return nil
		default:
			return fmt.Errorf("unexpected GitLab merge train status %q; refresh before retrying", car.Status)
		}
	}
	// GitLab distinguishes an MR with no car from a missing project/endpoint.
	// Treat only that specific 404 as permission to attempt enrollment.
	if !strings.Contains(strings.ToLower(stderr), "404 merge train merge request not found") {
		return fmt.Errorf("read GitLab merge train status (check project support and permissions): %w", err)
	}
	if !fresh.Ready {
		return fmt.Errorf("GitLab merge train: MR !%d is not ready: %s", params.Number, strings.Join(fresh.Blockers, "; "))
	}
	args = append(args, "--method", "POST", "--raw-field", "sha="+params.ExpectedHeadSHA, "--field", "auto_merge=true")
	if params.Method == "squash" {
		args = append(args, "--field", "squash=true")
	}
	// GitLab can return other cars on a successful POST (201 or 202).
	if _, _, err := c.run(ctx, worktree, args...); err != nil {
		return fmt.Errorf("submit GitLab merge train (check head, project support and permissions): %w", err)
	}
	return nil
}
