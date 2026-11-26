package task

import (
	"context"
	"errors"
	"fmt"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"slices"
	"strings"
)

type targetReview struct{ target, state, sha string }
type preparedReviews struct {
	client  forge.ForgeClient
	repo    string
	targets []targetReview
}

func (m *manager) prepareServiceReviews(ctx context.Context, svc domain.Service, plan ServiceClosePlan) (preparedReviews, error) {
	client, err := m.forgeClientForService(ctx, svc)
	if err != nil {
		return preparedReviews{}, err
	}
	r := preparedReviews{client: client, repo: forge.ExtractRepoPath(svc.RemoteURL)}
	if r.repo == "" {
		return r, fmt.Errorf("invalid remote for %s", svc.Name)
	}
	history, ok := client.(forge.HistoryClient)
	if !ok {
		return r, errors.New("forge does not support MR history")
	}
	rows, err := history.MRHistory(ctx, svc.Branch, r.repo)
	if err != nil {
		return r, err
	}
	for _, row := range rows {
		if openMRState(row.State) && (row.SourceBranch != svc.Branch || !slices.Contains(plan.ReviewTargets, row.TargetBranch)) {
			return r, fmt.Errorf("unexpected open MR !%d from %s into %s; expected %s into %v", row.Number, row.SourceBranch, row.TargetBranch, svc.Branch, plan.ReviewTargets)
		}
	}
	for _, target := range appendUnique(nil, plan.ReviewTargets...) {
		state, sha, err := m.classifyReview(ctx, svc, target, r.repo, client, rows)
		if err != nil {
			return r, err
		}
		r.targets = append(r.targets, targetReview{target, state, sha})
	}
	if len(r.targets) == 0 {
		return r, ErrNoMergeTargets
	}
	return r, nil
}

func (m *manager) executeServiceReviews(ctx context.Context, svc domain.Service, sp ServiceClosePlan, reviews preparedReviews, p CloseTaskParams, issue *domain.Issue, result *CloseTaskResult, step func(string, StepStatus, string)) (bool, string, error) {
	if m.cfg.Close == nil || m.cfg.Close.PushSourceBeforeReview {
		var pushed bool
		var err error
		if m.sourceRemovedOnMerge(svc.Name, svc.Branch) || m.hasServiceRule(svc) {
			// Prefer the leased update for an existing open review over a new source push.
			publication := reviewSourcePublication{state: reviewCloseVerified}
			for _, r := range reviews.targets {
				if r.state != reviewCloseVerified {
					publication = reviewSourcePublication{r.state, r.sha}
					if r.state == reviewCloseWaiting {
						break
					}
				}
			}
			if publication.state != reviewCloseVerified {
				pushed, err = m.publishReviewSource(ctx, svc, publication)
			}
		} else {
			err = m.pushBranch(ctx, svc.WorktreePath)
			pushed = err == nil
		}
		if err != nil {
			return false, "", err
		}
		if pushed {
			step(svc.Name+":push-source", StepStatusOK, "source pushed")
		}
	}
	verified := true
	var accepted string
	for _, r := range reviews.targets {
		switch r.state {
		case reviewCloseVerified:
			if len(reviews.targets) == 1 || sp.TagPlan != nil && sp.TagPlan.SourceRef == r.target {
				accepted = r.sha
			}
			step(svc.Name+":review-request", StepStatusOK, "merged MR verified at "+r.sha)
		case reviewCloseWaiting:
			verified = false
			step(svc.Name+":review-request", StepStatusSkipped, "waiting for merge")
		case reviewCloseCreate:
			verified = false
			title, desc := sp.ForgePlan.Title, sp.ForgePlan.Description
			if strings.TrimSpace(p.ReviewTitle) != "" {
				title = p.ReviewTitle
			}
			if strings.TrimSpace(p.ReviewDescription) != "" {
				desc = p.ReviewDescription
			}
			params := withMRPolicy(forge.CreateMRParams{WorktreePath: svc.WorktreePath, SourceBranch: svc.Branch, TargetBranch: r.target, Title: title, Description: desc, Repo: reviews.repo}, sp.MR)
			mr, err := reviews.client.CreateMR(ctx, m.withIssueReference(params, issue, svc.RemoteURL))
			if err != nil {
				return false, "", err
			}
			result.MRURLs = append(result.MRURLs, mr.URL)
			step(svc.Name+":review-request", StepStatusOK, "created "+mr.URL)
		}
	}
	if verified && sp.TagPlan != nil && accepted == "" {
		return false, "", fmt.Errorf("no verified MR into tag source %s", sp.TagPlan.SourceRef)
	}
	return verified, accepted, nil
}
