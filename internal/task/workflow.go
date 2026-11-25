package task

import (
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

var taskWorkflowSteps = []domain.WorkflowStep{
	{Phase: domain.TaskWorkflowCode, Label: "code"},
	{Phase: domain.TaskWorkflowMR, Label: "MR"},
	{Phase: domain.TaskWorkflowReviewCI, Label: "review + CI"},
	{Phase: domain.TaskWorkflowMerge, Label: "merge"},
	{Phase: domain.TaskWorkflowReleaseEligible, Label: "release"},
}

func ReleaseWorkflow(release domain.Release, flow *gitflow.ResolvedGitFlow) domain.WorkflowSummary {
	mach := releaseMachineOf(flow)
	current := releaseStateIDOf(mach, release)
	next, blocker := releaseGuidance(mach, current, release.Status, release.Error)
	done := release.Status == domain.ReleaseStatusReleased
	blocked := release.Status == domain.ReleaseStatusTaskMergeBlocked ||
		release.Status == domain.ReleaseStatusTaskMergePartial ||
		release.Status == domain.ReleaseStatusFailed ||
		release.Status == domain.ReleaseStatusRejected
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
		rowState := current
		if _, ok := mach.State(string(service.Status)); ok {
			rowState = string(service.Status)
		}
		rowNext, rowBlocker := releaseGuidance(mach, rowState, service.Status, service.Error)
		if detail == "" && rowBlocker != "" {
			detail = rowBlocker
		}
		rows[i] = domain.ServiceWorkflow{
			ServiceName: service.Name,
			Status:      string(service.Status),
			Detail:      detail,
			Current:     domain.WorkflowPhase(rowState),
			NextAction:  rowNext,
			Blocker:     rowBlocker,
		}
	}
	if actions := workflowBlockerActions(rows); blocked && actions != "" {
		next = actions
	}
	return workflowSummaryWithServices(releasePath(mach, current), domain.WorkflowPhase(current), next, blocker, done, blocked, rows)
}

// releasePath is the machine's main line from the initial state, following each state's
// next or first event; the current state is spliced in when it lies off that line.
func releasePath(mach *gitflow.Machine, current string) []domain.WorkflowStep {
	var steps []domain.WorkflowStep
	seen := map[string]bool{}
	for id := mach.Initial; id != "" && !seen[id]; {
		seen[id] = true
		st, ok := mach.State(id)
		if !ok {
			break
		}
		if st.Kind != gitflow.StateFinal {
			steps = append(steps, domain.WorkflowStep{Phase: domain.WorkflowPhase(st.ID), Label: st.Label, Kind: releaseStepKind(st)})
		}
		id = st.Next
		if st.Kind == gitflow.StateWait && st.Wait == gitflow.WaitManual && len(st.Events) > 0 {
			id = st.Events[0].To
		}
	}
	if st, ok := mach.State(current); ok && !seen[current] && st.Kind != gitflow.StateFinal {
		steps = append(steps, domain.WorkflowStep{Phase: domain.WorkflowPhase(st.ID), Label: st.Label, Kind: releaseStepKind(st)})
	}
	return steps
}

func releaseStepKind(st *gitflow.State) string {
	if st.Kind == gitflow.StateWait {
		if st.Wait == gitflow.WaitReviewMerged {
			return "review"
		}
		return "gate"
	}
	for _, step := range st.Run {
		switch step.Kind {
		case gitflow.StepTag:
			return "tag"
		case gitflow.StepReview:
			return "review"
		}
	}
	return ""
}

// releaseGuidance names the next action and blocker for a release or service status in state.
func releaseGuidance(mach *gitflow.Machine, state string, status domain.ReleaseStatus, serviceErr *domain.ReleaseError) (string, string) {
	blocker := ""
	if serviceErr != nil {
		blocker = serviceErr.Message
	}
	switch status {
	case domain.ReleaseStatusDraft:
		return "prepare release", blocker
	case domain.ReleaseStatusValidating:
		return "validating release", blocker
	case domain.ReleaseStatusMerging:
		return "preparing release", blocker
	case domain.ReleaseStatusBranching:
		return "creating release branches", blocker
	case domain.ReleaseStatusPushing:
		return "pushing", blocker
	case domain.ReleaseStatusAwaitingTaskMerge:
		return "awaiting task MR confirmation", blocker
	case domain.ReleaseStatusIntegratingTasks:
		return "integrating task MRs (press ctrl+r to review retry)", blocker
	case domain.ReleaseStatusTaskMergeBlocked:
		return "press ctrl+r to review blocked task MRs", blocker
	case domain.ReleaseStatusTaskMergePartial:
		return "press ctrl+r to review partial task MRs", blocker
	case domain.ReleaseStatusSyncingDevelop:
		return "back-merging", blocker
	case domain.ReleaseStatusTagging:
		return "tagging release", blocker
	case domain.ReleaseStatusReleased:
		return "release complete", blocker
	case domain.ReleaseStatusFailed:
		if blocker == "" {
			blocker = "Release failed; completion is unverified"
		}
		if serviceErr != nil && serviceErr.Recoverable {
			return "inspect failure and remote state; press ctrl+r to review recovery", blocker
		}
		return "inspect failure and remote state; manual recovery required", blocker
	case domain.ReleaseStatusRejected:
		if blocker == "" {
			blocker = "Release was rejected"
		}
		return "review rejection; prepare a new release if intended", blocker
	}
	st, ok := mach.State(state)
	if !ok {
		return "prepare release", blocker
	}
	switch {
	case st.Kind == gitflow.StateRun:
		return "running " + st.Label, blocker
	case st.Kind == gitflow.StateFinal:
		return "release complete", blocker
	case st.Wait == gitflow.WaitReviewMerged:
		return "merge ready MRs in forge, then press M to reconcile", blocker
	case st.Wait == gitflow.WaitCIGreen:
		return "waiting for green CI on " + st.WaitOn + "; press F to re-check", blocker
	}
	actions := make([]string, 0, len(st.Events))
	for _, ev := range st.Events {
		actions = append(actions, "press "+ev.Key+" to "+ev.Label)
	}
	return strings.Join(actions, " · "), blocker
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
