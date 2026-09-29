package task

import (
	"context"
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func (m *manager) workflowAllMergedSummary(plans []serviceWorkflowPlan, states []serviceMergeState, hasReview bool) domain.WorkflowSummary {
	rows := make([]domain.ServiceWorkflow, len(plans))
	for i := range plans {
		rows[i] = mergedWorkflowRow(plans[i], states[i])
	}
	post := combinedPostAction(plans)
	if hasReview {
		if post != "" {
			return workflowSummaryWithServices(taskWorkflowSteps, domain.TaskWorkflowReleaseEligible, post, "", false, false, rows)
		}
		return workflowSummaryWithServices(taskWorkflowSteps, domain.TaskWorkflowReleaseEligible, "select in release (N)", "", true, false, rows)
	}
	steps := directMergeWorkflowSteps(combinedTagOnClose(plans))
	if post != "" {
		current := domain.TaskWorkflowMerge
		if combinedTagOnClose(plans) {
			current = domain.TaskWorkflowReleaseEligible
		}
		return workflowSummaryWithServices(steps, current, post, "", false, false, rows)
	}
	return workflowSummaryWithServices(steps, domain.TaskWorkflowMerge, "", "", true, false, rows)
}

// combinedPostAction names the close action that remains after every merge
// target is satisfied: tagging and/or pipeline trigger configured on the rule.
// Empty means close has nothing left to do.
func combinedPostAction(plans []serviceWorkflowPlan) string {
	tag, pipeline := combinedTagOnClose(plans), false
	for i := range plans {
		pipeline = pipeline || plans[i].rule.TriggerPipelineOnClose
	}
	switch {
	case tag && pipeline:
		return "press C to tag and trigger pipeline"
	case tag:
		return "press C to create tag"
	case pipeline:
		return "press C to trigger pipeline"
	default:
		return ""
	}
}

func combinedTagOnClose(plans []serviceWorkflowPlan) bool {
	for i := range plans {
		if plans[i].rule.TagOnClose {
			return true
		}
	}
	return false
}

func (m *manager) directOnlyWorkflowSummary(plans []serviceWorkflowPlan, states []serviceMergeState, hasDirect bool) domain.WorkflowSummary {
	rows := make([]domain.ServiceWorkflow, len(plans))
	if !hasDirect {
		for i := range plans {
			rows[i] = noneCloseRow(plans[i].svc.Name, plans[i].rule, states[i].warning)
		}
		if post := combinedPostAction(plans); post != "" {
			return domain.WorkflowSummary{NextAction: post, Services: rows}
		}
		return domain.WorkflowSummary{NextAction: "no close action configured", Services: rows}
	}
	pendingTargets := pendingTargetNames(plans, states)
	for i := range plans {
		rows[i] = directWorkflowRow(plans[i], states[i])
	}
	next := "press C to merge into " + strings.Join(pendingTargets, ", ")
	return workflowSummaryWithServices(directMergeWorkflowSteps(combinedTagOnClose(plans)), domain.TaskWorkflowMerge, next, "", false, false, rows)
}

// reviewWorkflowSummary aggregates review_request services via forge readiness
// with direct_merge/none services from git state, so forge is only touched for
// services that actually use merge requests.
func (m *manager) reviewWorkflowSummary(ctx context.Context, plans []serviceWorkflowPlan, states []serviceMergeState) (domain.WorkflowSummary, error) {
	rows := make([]domain.ServiceWorkflow, 0, len(plans))
	var reviewItems []ServiceMergeInspection
	missingMR, ready := false, false
	waiting := 0
	var blockedDetails []string
	pendingTargets := pendingTargetNames(plans, states)

	for i := range plans {
		plan := &plans[i]
		switch plan.rule.CloseStrategy {
		case gitflow.CloseStrategyNone:
			rows = append(rows, noneCloseRow(plan.svc.Name, plan.rule, states[i].warning))
		case gitflow.CloseStrategyDirectMerge:
			rows = append(rows, directWorkflowRow(*plan, states[i]))
		case gitflow.CloseStrategyReviewRequest:
			reviewTarget := ""
			if len(plan.rule.ReviewTargets) > 0 {
				reviewTarget = plan.rule.ReviewTargets[0]
			}
			item := m.inspectWorkflowReviewMR(ctx, plan.svc, reviewTarget)
			reviewItems = append(reviewItems, item)
			rows = append(rows, domain.ServiceWorkflow{ServiceName: item.ServiceName, Status: item.Status, Detail: strings.Join(item.Blockers, "; ")})
			switch item.Status {
			case "no_mr":
				missingMR = true
			case "ready":
				ready = true
			case "blocked", "failed":
				detail := strings.Join(item.Blockers, "; ")
				if detail == "" {
					detail = item.Status
				}
				blockedDetails = append(blockedDetails, item.ServiceName+": "+detail)
			default:
				waiting++
			}
		}
	}

	if len(blockedDetails) > 0 {
		return workflowSummaryWithServices(taskWorkflowSteps, domain.TaskWorkflowReviewCI,
			"fix blockers, then M", strings.Join(blockedDetails, "; "), false, true, rows), nil
	}
	if ready || len(pendingTargets) > 0 {
		var parts []string
		if ready {
			parts = append(parts, "press M to merge ready MRs")
		}
		if len(pendingTargets) > 0 {
			parts = append(parts, "press C to merge into "+strings.Join(pendingTargets, ", "))
		}
		if missingMR {
			parts = append(parts, "press C to create MRs")
		}
		return workflowSummaryWithServices(taskWorkflowSteps, domain.TaskWorkflowMerge, strings.Join(parts, "; "), "", false, false, rows), nil
	}
	if missingMR {
		return m.missingMRWorkflowSummary(ctx, plans, reviewItems, rows)
	}
	blocker := fmt.Sprintf("%d services waiting for approval/CI", waiting)
	if waiting == 1 {
		blocker = "1 service waiting for approval/CI"
	}
	return workflowSummaryWithServices(taskWorkflowSteps, domain.TaskWorkflowReviewCI, "wait for review/CI, then M", blocker, false, false, rows), nil
}

func (m *manager) missingMRWorkflowSummary(ctx context.Context, plans []serviceWorkflowPlan, reviewItems []ServiceMergeInspection, rows []domain.ServiceWorkflow) (domain.WorkflowSummary, error) {
	servicesByName := make(map[string]domain.Service, len(plans))
	for i := range plans {
		servicesByName[plans[i].svc.Name] = plans[i].svc
	}
	allPushed := true
	for _, item := range reviewItems {
		if item.Status != "no_mr" {
			continue
		}
		svc := servicesByName[item.ServiceName]
		pushed, err := m.git.RemoteBranchExists(ctx, svc.RepoPath, svc.Branch)
		if err != nil {
			return domain.WorkflowSummary{}, fmt.Errorf("workflow: check pushed branch for service %s: %w", item.ServiceName, err)
		}
		allPushed = allPushed && pushed
	}
	if allPushed {
		return workflowSummaryWithServices(taskWorkflowSteps, domain.TaskWorkflowMR, "press C to create MRs", "", false, false, rows), nil
	}
	return workflowSummaryWithServices(taskWorkflowSteps, domain.TaskWorkflowCode, "press C to create MRs", "", false, false, rows), nil
}

func pendingTargetNames(plans []serviceWorkflowPlan, states []serviceMergeState) []string {
	var pendingTargets []string
	seenPending := map[string]bool{}
	for i := range plans {
		if plans[i].rule.CloseStrategy != gitflow.CloseStrategyDirectMerge {
			continue
		}
		for _, target := range unmergedTargets(states[i]) {
			name := strings.TrimPrefix(target, "origin/")
			if !seenPending[name] {
				seenPending[name] = true
				pendingTargets = append(pendingTargets, name)
			}
		}
	}
	return pendingTargets
}

func mergedWorkflowRow(plan serviceWorkflowPlan, st serviceMergeState) domain.ServiceWorkflow {
	return workflowStateRow(plan, st, "merged", "merged into "+strings.Join(st.targets, ", "))
}

func directWorkflowRow(plan serviceWorkflowPlan, st serviceMergeState) domain.ServiceWorkflow {
	if unmerged := unmergedTargets(st); len(unmerged) > 0 {
		return workflowStateRow(plan, st, "pending", "pending: "+strings.Join(unmerged, ", "))
	}
	return workflowStateRow(plan, st, "merged", "merged into "+strings.Join(st.targets, ", "))
}

// noneCloseRow represents close_strategy none services: rows mirror the
// configured post-actions so they agree with the combined C guidance.
func noneCloseRow(name string, rule gitflow.BranchTypeRule, warning string) domain.ServiceWorkflow {
	status, detail := "none", "no close action"
	switch {
	case rule.TagOnClose && rule.TriggerPipelineOnClose:
		status, detail = "tag+pipeline", "tag and trigger pipeline"
	case rule.TagOnClose:
		status, detail = "tag", "create tag"
	case rule.TriggerPipelineOnClose:
		status, detail = "pipeline", "trigger pipeline"
	}
	if warning != "" {
		if detail != "" {
			detail += "; "
		}
		detail += warning
	}
	return domain.ServiceWorkflow{ServiceName: name, Status: status, Detail: detail}
}

func workflowStateRow(plan serviceWorkflowPlan, st serviceMergeState, status, detail string) domain.ServiceWorkflow {
	if len(st.targets) == 0 {
		return noneCloseRow(plan.svc.Name, plan.rule, st.warning)
	}
	if st.warning != "" {
		if detail != "" {
			detail += "; "
		}
		detail += st.warning
	}
	return domain.ServiceWorkflow{ServiceName: plan.svc.Name, Status: status, Detail: detail}
}

func unmergedTargets(st serviceMergeState) []string {
	var unmerged []string
	for i, target := range st.targets {
		if i < len(st.merged) && !st.merged[i] {
			unmerged = append(unmerged, target)
		}
	}
	return unmerged
}

func originPrefixed(targets []string) []string {
	prefixed := make([]string, len(targets))
	for i, target := range targets {
		prefixed[i] = "origin/" + target
	}
	return prefixed
}
