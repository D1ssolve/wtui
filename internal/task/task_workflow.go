package task

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

// directMergeWorkflowSteps renders the close path for direct_merge tasks:
// no MR and no release steps; a tag step is shown only when a rule tags on close.
func directMergeWorkflowSteps(tagOnClose bool) []domain.WorkflowStep {
	steps := []domain.WorkflowStep{
		{Phase: domain.TaskWorkflowCode, Label: "code"},
		{Phase: domain.TaskWorkflowMerge, Label: "merge"},
	}
	if tagOnClose {
		steps = append(steps, domain.WorkflowStep{Phase: domain.TaskWorkflowReleaseEligible, Label: "tag"})
	}
	return steps
}

type serviceWorkflowPlan struct {
	svc        domain.Service
	branchType gitflow.BranchType
	rule       gitflow.BranchTypeRule
}

type serviceMergeState struct {
	targets []string // effective close targets, origin/-prefixed
	merged  []bool
	warning string
	err     error
}

func (m *manager) TaskWorkflow(ctx context.Context, taskID string) (domain.WorkflowSummary, error) {
	services, err := m.ListServices(ctx, taskID)
	if err != nil {
		return domain.WorkflowSummary{}, err
	}
	if len(services) == 0 {
		return domain.WorkflowSummary{NextAction: "task has no services"}, nil
	}

	if m.allHotfixReview(services) {
		return m.hotfixReviewWorkflow(ctx, taskID)
	}

	plans := make([]serviceWorkflowPlan, len(services))
	hasReview, hasDirect := false, false
	for i, svc := range services {
		plan := serviceWorkflowPlan{svc: svc, branchType: gitflow.BranchTypeUnknown}
		var rule gitflow.BranchTypeRule
		var ok bool
		if m.flow != nil {
			plan.branchType = gitflow.DetectBranchType(svc.Branch, m.flow)
			rule, ok = m.flow.BranchTypes[plan.branchType]
		}
		if !ok {
			// No rule for this branch type (sparse or missing flow): assume the
			// legacy review flow against the integration branch.
			rule = gitflow.BranchTypeRule{
				CloseStrategy: gitflow.CloseStrategyReviewRequest,
				ReviewTargets: []string{m.integrationBranch()},
			}
		}
		plan.rule = rule
		switch rule.CloseStrategy {
		case gitflow.CloseStrategyReviewRequest:
			hasReview = true
		case gitflow.CloseStrategyDirectMerge:
			hasDirect = true
		case gitflow.CloseStrategyNone:
		default:
			return domain.WorkflowSummary{}, fmt.Errorf("workflow: unsupported close strategy %q for service %s", rule.CloseStrategy, svc.Name)
		}
		plans[i] = plan
	}

	states, err := m.workflowMergeStates(ctx, plans)
	if err != nil {
		return domain.WorkflowSummary{}, err
	}

	if workflowAllMerged(states) {
		return m.workflowAllMergedSummary(plans, states, hasReview), nil
	}
	if !hasReview {
		return m.directOnlyWorkflowSummary(plans, states, hasDirect), nil
	}
	return m.reviewWorkflowSummary(ctx, plans, states)
}

func (m *manager) integrationBranch() string {
	if m.flow != nil && m.flow.IntegrationBranch != "" {
		return m.flow.IntegrationBranch
	}
	if m.cfg != nil && m.cfg.BaseBranch != "" {
		return m.cfg.BaseBranch
	}
	return "develop"
}

func (m *manager) allHotfixReview(services []domain.Service) bool {
	if m.flow == nil || len(services) == 0 {
		return false
	}
	for _, svc := range services {
		if !m.isHotfixReview(svc.Branch) {
			return false
		}
	}
	return true
}

// hotfixReviewWorkflow keeps the per-target MR flow for review-request hotfixes:
// missing/ready/waiting/blocked/merged rows per target and final C tagging.
func (m *manager) hotfixReviewWorkflow(ctx context.Context, taskID string) (domain.WorkflowSummary, error) {
	inspection, err := m.InspectTaskMerge(ctx, taskID)
	if err != nil {
		return domain.WorkflowSummary{}, err
	}
	steps := append([]domain.WorkflowStep(nil), taskWorkflowSteps...)
	steps[len(steps)-1].Label = "tag"
	allMerged := len(inspection.Services) > 0
	missing, ready := false, false
	var rows []domain.ServiceWorkflow
	var blockers []string
	for _, item := range inspection.Services {
		allMerged = allMerged && item.Status == "merged"
		missing = missing || item.Status == "no_mr"
		ready = ready || item.Status == "ready"
		if item.Status == "failed" || item.Status == "blocked" {
			blockers = append(blockers, strings.Join(item.Blockers, "; "))
		}
		rows = append(rows, domain.ServiceWorkflow{ServiceName: item.ServiceName, Status: item.Status, Detail: item.MR.TargetBranch + ": " + strings.Join(item.Blockers, "; ")})
	}
	phase, next := domain.TaskWorkflowReviewCI, "Services → m → Merge MR"
	if missing {
		phase, next = domain.TaskWorkflowMR, "press C to create missing hotfix MRs"
	} else if allMerged {
		phase, next = domain.TaskWorkflowReleaseEligible, "press C to finalize hotfix"
	} else if ready {
		phase = domain.TaskWorkflowMerge
	}
	return workflowSummaryWithServices(steps, phase, next, strings.Join(blockers, "; "), false, len(blockers) > 0, rows), nil
}

// workflowMergeStates fetches every service and checks its branch against all
// effective close targets. Review services check the first review target only,
// mirroring ordinary review close semantics.
func (m *manager) workflowMergeStates(ctx context.Context, plans []serviceWorkflowPlan) ([]serviceMergeState, error) {
	states := make([]serviceMergeState, len(plans))
	m.runWorkflowChecks(len(plans), func(i int) {
		plan := &plans[i]
		st := &states[i]
		switch plan.rule.CloseStrategy {
		case gitflow.CloseStrategyNone:
			return
		case gitflow.CloseStrategyDirectMerge:
			targets, warning := m.effectiveMergeTargets(ctx, plan.svc, plan.branchType, plan.rule.MergeTargets)
			st.warning = warning
			st.targets = originPrefixed(targets)
		case gitflow.CloseStrategyReviewRequest:
			if len(plan.rule.ReviewTargets) == 0 {
				return
			}
			st.targets = []string{"origin/" + plan.rule.ReviewTargets[0]}
		}
		if st.err = m.git.Fetch(ctx, plan.svc.RepoPath); st.err != nil {
			return
		}
		st.merged = make([]bool, len(st.targets))
		for j, target := range st.targets {
			if st.merged[j], st.err = m.git.IsAncestor(ctx, plan.svc.RepoPath, plan.svc.Branch, target); st.err != nil {
				return
			}
		}
	})
	for i := range states {
		if states[i].err != nil {
			return nil, fmt.Errorf("workflow: check service %s: %w", plans[i].svc.Name, states[i].err)
		}
	}
	return states, nil
}

// workflowAllMerged reports whether every service with close targets is merged
// into all of them. Vacuously true is avoided: a task with no targets at all
// (close_strategy none) is not "merged".
func workflowAllMerged(states []serviceMergeState) bool {
	anyTargets := false
	for i := range states {
		if len(states[i].targets) == 0 {
			continue
		}
		anyTargets = true
		for _, merged := range states[i].merged {
			if !merged {
				return false
			}
		}
	}
	return anyTargets
}

func (m *manager) runWorkflowChecks(count int, check func(int)) {
	sem := make(chan struct{}, m.concurrency())
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			check(i)
		}()
	}
	wg.Wait()
}
