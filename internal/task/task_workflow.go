package task

import (
	"context"
	"fmt"
	"slices"
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
	targets []string // effective close targets, configured remote/-prefixed
	merged  []bool
	warning string
	err     error
	pinned  *ServiceMergeInspection
}

func (m *manager) workflowPlans(services []domain.Service) ([]serviceWorkflowPlan, bool, bool, error) {
	plans := make([]serviceWorkflowPlan, len(services))
	hasReview, hasDirect := false, false
	for i, svc := range services {
		plan := serviceWorkflowPlan{svc: svc, branchType: gitflow.BranchTypeUnknown}
		var rule gitflow.BranchTypeRule
		var ok bool
		if m.flow != nil {
			plan.branchType = gitflow.DetectBranchType(svc.Branch, m.flow)
			_, ok = m.flow.BranchTypes[plan.branchType]
			rule = m.flow.RuleFor(svc.Name, plan.branchType)
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
			return nil, false, false, fmt.Errorf("workflow: unsupported close strategy %q for service %s", rule.CloseStrategy, svc.Name)
		}
		plans[i] = plan
	}

	return plans, hasReview, hasDirect, nil
}

func (m *manager) TaskWorkflow(ctx context.Context, taskID string) (domain.WorkflowSummary, error) {
	services, err := m.ListServices(ctx, taskID)
	if err != nil {
		return domain.WorkflowSummary{}, err
	}
	if len(services) == 0 {
		return domain.WorkflowSummary{NextAction: "task has no services"}, nil
	}
	if tf, err := m.loadTaskFlow(ctx, taskID); err != nil {
		return domain.WorkflowSummary{Blocker: err.Error()}, nil
	} else if tf != nil {
		return m.taskMachineWorkflow(tf), nil
	}

	if m.allHotfixReview(services) {
		return m.hotfixReviewWorkflow(ctx, taskID)
	}

	plans, hasReview, hasDirect, err := m.workflowPlans(services)
	if err != nil {
		return domain.WorkflowSummary{}, err
	}

	states, err := m.workflowMergeStates(ctx, taskID, plans)
	if err != nil {
		return domain.WorkflowSummary{}, err
	}

	if workflowAllMerged(states) {
		return m.workflowAllMergedSummary(plans, states, hasReview), nil
	}
	if !hasReview {
		return m.directOnlyWorkflowSummary(plans, states, hasDirect), nil
	}
	return m.reviewWorkflowSummary(ctx, taskID, plans, states)
}

func (m *manager) integrationBranch() string {
	if m.flow != nil && m.flow.IntegrationBranch != "" {
		return m.flow.IntegrationBranch
	}
	return "develop"
}

func (m *manager) allHotfixReview(services []domain.Service) bool {
	if m.flow == nil || len(services) == 0 {
		return false
	}
	for _, svc := range services {
		if !m.isMultiTargetReview(svc.Name, svc.Branch) {
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
		row := reviewWorkflowRow(item)
		row.Detail = item.MR.TargetBranch + ": " + strings.Join(item.Blockers, "; ")
		if row.Blocker != "" {
			row.Blocker = item.MR.TargetBranch + ": " + row.Blocker
			blockers = appendUnique(blockers, item.ServiceName+": "+row.Blocker)
		}
		if row.NextAction != "" {
			row.NextAction = item.MR.TargetBranch + ": " + row.NextAction
		}
		rows = append(rows, row)
	}
	phase, next := domain.TaskWorkflowReviewCI, "Services → m → Merge MR"
	if missing {
		phase, next = domain.TaskWorkflowMR, "press C to create missing hotfix MRs"
	} else if allMerged {
		phase, next = domain.TaskWorkflowReleaseEligible, "press C to finalize hotfix"
	} else if ready {
		phase = domain.TaskWorkflowMerge
	}
	if len(blockers) > 0 || (!missing && !allMerged && !ready) {
		next = workflowBlockerActions(rows)
	}
	return workflowSummaryWithServices(steps, phase, next, strings.Join(blockers, "; "), false, len(blockers) > 0, rows), nil
}

// workflowMergeStates fetches every service and checks its branch against all
// effective close targets, including every target of a machine-gated review.
func (m *manager) workflowMergeStates(ctx context.Context, taskID string, plans []serviceWorkflowPlan) ([]serviceMergeState, error) {
	pins, err := m.loadTaskTrainPins(taskID)
	if err != nil {
		return nil, err
	}
	for _, pin := range pins {
		if !slices.ContainsFunc(plans, func(plan serviceWorkflowPlan) bool { return plan.svc.Name == pin.Service }) {
			return nil, fmt.Errorf("service %s for recorded train MR !%d is missing", pin.Service, pin.Number)
		}
	}
	states := make([]serviceMergeState, len(plans))
	m.runWorkflowChecks(len(plans), func(i int) {
		plan := &plans[i]
		st := &states[i]
		for _, pin := range pins {
			if pin.Service != plan.svc.Name {
				continue
			}
			item := m.inspectPinnedTaskTrain(ctx, plan.svc, pin)
			st.targets = append(st.targets, m.cfg.ServiceRemote().Tracking(pin.Target))
			st.merged = append(st.merged, item.Status == "merged")
			if st.pinned == nil || item.Status != "merged" {
				st.pinned = &item
			}
		}
		if st.pinned != nil {
			// Once there is a train pin, ancestry alone cannot satisfy a
			// review wait. All configured targets must have a verified pin.
			for _, target := range m.taskTrainTargets(plan.svc) {
				if _, exists := pins[taskTrainKey{plan.svc.Name, target}]; !exists {
					st.targets = append(st.targets, m.cfg.ServiceRemote().Tracking(target))
					st.merged = append(st.merged, false)
				}
			}
			return
		}
		switch plan.rule.CloseStrategy {
		case gitflow.CloseStrategyNone:
			return
		case gitflow.CloseStrategyDirectMerge:
			targets, warning := m.effectiveMergeTargets(ctx, plan.svc, plan.branchType, plan.rule.MergeTargets)
			st.warning = warning
			st.targets = m.remotePrefixed(targets)
		case gitflow.CloseStrategyReviewRequest:
			if len(plan.rule.ReviewTargets) == 0 {
				return
			}
			st.targets = m.remotePrefixed(plan.rule.ReviewTargets)
		}
		if st.err = m.git.Fetch(ctx, plan.svc.RepoPath); st.err != nil {
			return
		}
		st.merged = make([]bool, len(st.targets))
		for j, target := range st.targets {
			if st.merged[j], st.err = m.git.IsAncestor(ctx, plan.svc.RepoPath, plan.svc.Branch, "refs/remotes/"+target); st.err != nil {
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
