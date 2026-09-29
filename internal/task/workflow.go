package task

import (
	"fmt"

	"github.com/D1ssolve/wtui/internal/domain"
)

var taskWorkflowSteps = []domain.WorkflowStep{
	{Phase: domain.TaskWorkflowCode, Label: "code"},
	{Phase: domain.TaskWorkflowMR, Label: "MR"},
	{Phase: domain.TaskWorkflowReviewCI, Label: "review + CI"},
	{Phase: domain.TaskWorkflowMerge, Label: "merge"},
	{Phase: domain.TaskWorkflowReleaseEligible, Label: "release"},
}

var releaseWorkflowSteps = []domain.WorkflowStep{
	{Phase: domain.ReleaseWorkflowDevelop, Label: "develop"},
	{Phase: domain.ReleaseWorkflowReleaseBranch, Label: "release"},
	{Phase: domain.ReleaseWorkflowRegression, Label: "regression"},
	{Phase: domain.ReleaseWorkflowMasterMR, Label: "master MR"},
	{Phase: domain.ReleaseWorkflowTag, Label: "tag"},
}

func ReleaseWorkflow(release domain.Release) domain.WorkflowSummary {
	current := domain.ReleaseWorkflowDevelop
	next := "prepare release"
	done, blocked := false, false

	switch release.Status {
	case domain.ReleaseStatusValidating:
		next = "validating release"
	case domain.ReleaseStatusMerging:
		next = "preparing release"
	case domain.ReleaseStatusBranching:
		current, next = domain.ReleaseWorkflowReleaseBranch, "creating release branches"
	case domain.ReleaseStatusPushing:
		current, next = domain.ReleaseWorkflowReleaseBranch, "pushing release branches"
	case domain.ReleaseStatusAwaitingTaskMerge:
		next = "awaiting task MR confirmation"
	case domain.ReleaseStatusIntegratingTasks:
		next = "integrating task MRs (press R to review retry)"
	case domain.ReleaseStatusTaskMergeBlocked:
		next, blocked = "press R to review blocked task MRs", true
	case domain.ReleaseStatusTaskMergePartial:
		next, blocked = "press R to review partial task MRs", true
	case domain.ReleaseStatusPrepared:
		current, next = domain.ReleaseWorkflowRegression, "press F to create master MRs"
	case domain.ReleaseStatusAwaitingMasterMerge:
		current, next = domain.ReleaseWorkflowMasterMR, "press M to merge ready MRs"
	case domain.ReleaseStatusMasterMerged:
		current, next = domain.ReleaseWorkflowTag, "press F to sync develop and tag"
	case domain.ReleaseStatusSyncingDevelop:
		current, next = domain.ReleaseWorkflowTag, "syncing develop"
	case domain.ReleaseStatusTagging:
		current, next = domain.ReleaseWorkflowTag, "tagging release"
	case domain.ReleaseStatusReleased:
		current, next, done = domain.ReleaseWorkflowTag, "release complete", true
	case domain.ReleaseStatusFailed:
		next, blocked = "release failed", true
	case domain.ReleaseStatusRejected:
		next, blocked = "release rejected", true
	}

	blocker := ""
	if release.Error != nil {
		blocker = release.Error.Message
	}
	rows := make([]domain.ServiceWorkflow, len(release.Services))
	for i, service := range release.Services {
		detail := ""
		if service.Error != nil {
			detail = service.Error.Message
		} else if service.ProductionMR != nil {
			detail = fmt.Sprintf("MR #%d: %s", service.ProductionMR.Number, service.ProductionMR.State)
		}
		if summary := taskMergeProgressDetail(service.FeatureBranches); summary != "" {
			if detail != "" {
				detail += "  "
			}
			detail += summary
		}
		rows[i] = domain.ServiceWorkflow{ServiceName: service.Name, Status: string(service.Status), Detail: detail}
	}
	return workflowSummaryWithServices(releaseWorkflowSteps, current, next, blocker, done, blocked, rows)
}

func taskMergeProgressDetail(branches []domain.ReleaseFeatureBranch) string {
	total, merged := 0, 0
	for _, fb := range branches {
		if fb.TaskMergeStatus == "" {
			continue
		}
		total++
		if fb.TaskMergeStatus == taskMergeStatusMerged {
			merged++
		}
	}
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("task MRs: %d/%d merged", merged, total)
}

func workflowSummaryWithServices(template []domain.WorkflowStep, current domain.WorkflowPhase, nextAction, blocker string, done, blocked bool, services []domain.ServiceWorkflow) domain.WorkflowSummary {
	summary := workflowSummary(template, current, nextAction, blocker, done, blocked)
	summary.Services = services
	return summary
}

func workflowSummary(template []domain.WorkflowStep, current domain.WorkflowPhase, nextAction, blocker string, done, blocked bool) domain.WorkflowSummary {
	steps := make([]domain.WorkflowStep, len(template))
	seenCurrent := false
	for i, step := range template {
		steps[i] = step
		switch {
		case done:
			steps[i].State = "done"
		case step.Phase == current:
			seenCurrent = true
			if blocked {
				steps[i].State = "blocked"
			} else {
				steps[i].State = "now"
			}
		case seenCurrent:
			steps[i].State = "next"
		default:
			steps[i].State = "done"
		}
	}
	return domain.WorkflowSummary{Steps: steps, Current: current, NextAction: nextAction, Blocker: blocker}
}
