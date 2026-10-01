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
	current, next, _ := releaseServiceGuidance(release.Status, release.Error)
	done := release.Status == domain.ReleaseStatusReleased
	blocked := release.Status == domain.ReleaseStatusTaskMergeBlocked ||
		release.Status == domain.ReleaseStatusTaskMergePartial ||
		release.Status == domain.ReleaseStatusFailed ||
		release.Status == domain.ReleaseStatusRejected
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
		rowCurrent, rowNext, rowBlocker := releaseServiceGuidance(service.Status, service.Error)
		rows[i] = domain.ServiceWorkflow{
			ServiceName: service.Name,
			Status:      string(service.Status),
			Detail:      detail,
			Current:     rowCurrent,
			NextAction:  rowNext,
			Blocker:     rowBlocker,
		}
	}
	return workflowSummaryWithServices(releaseWorkflowSteps, current, next, blocker, done, blocked, rows)
}

// releaseServiceGuidance maps a release status to its current phase, next
// action, and per-service blocker, shared by the release summary and rows.
func releaseServiceGuidance(status domain.ReleaseStatus, serviceErr *domain.ReleaseError) (domain.WorkflowPhase, string, string) {
	blocker := ""
	if serviceErr != nil {
		blocker = serviceErr.Message
	}
	switch status {
	case domain.ReleaseStatusValidating:
		return domain.ReleaseWorkflowDevelop, "validating release", blocker
	case domain.ReleaseStatusMerging:
		return domain.ReleaseWorkflowDevelop, "preparing release", blocker
	case domain.ReleaseStatusBranching:
		return domain.ReleaseWorkflowReleaseBranch, "creating release branches", blocker
	case domain.ReleaseStatusPushing:
		return domain.ReleaseWorkflowReleaseBranch, "pushing release branches", blocker
	case domain.ReleaseStatusAwaitingTaskMerge:
		return domain.ReleaseWorkflowDevelop, "awaiting task MR confirmation", blocker
	case domain.ReleaseStatusIntegratingTasks:
		return domain.ReleaseWorkflowDevelop, "integrating task MRs (press R to review retry)", blocker
	case domain.ReleaseStatusTaskMergeBlocked:
		return domain.ReleaseWorkflowDevelop, "press R to review blocked task MRs", blocker
	case domain.ReleaseStatusTaskMergePartial:
		return domain.ReleaseWorkflowDevelop, "press R to review partial task MRs", blocker
	case domain.ReleaseStatusPrepared:
		return domain.ReleaseWorkflowRegression, "press F to create master MRs", blocker
	case domain.ReleaseStatusAwaitingMasterMerge:
		return domain.ReleaseWorkflowMasterMR, "merge ready MRs in forge, then press M to reconcile", blocker
	case domain.ReleaseStatusMasterMerged:
		return domain.ReleaseWorkflowTag, "press F to sync develop and tag", blocker
	case domain.ReleaseStatusSyncingDevelop:
		return domain.ReleaseWorkflowTag, "syncing develop", blocker
	case domain.ReleaseStatusTagging:
		return domain.ReleaseWorkflowTag, "tagging release", blocker
	case domain.ReleaseStatusReleased:
		return domain.ReleaseWorkflowTag, "release complete", blocker
	case domain.ReleaseStatusFailed:
		return domain.ReleaseWorkflowDevelop, "release failed", blocker
	case domain.ReleaseStatusRejected:
		return domain.ReleaseWorkflowDevelop, "release rejected", blocker
	default:
		return domain.ReleaseWorkflowDevelop, "prepare release", blocker
	}
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
