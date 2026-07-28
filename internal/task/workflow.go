package task

import (
	"context"
	"fmt"
	"strings"
	"sync"

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

func (m *manager) TaskWorkflow(ctx context.Context, taskID string) (domain.WorkflowSummary, error) {
	services, err := m.ListServices(ctx, taskID)
	if err != nil {
		return domain.WorkflowSummary{}, err
	}

	integrationBranch := "develop"
	if m.flow != nil && m.flow.IntegrationBranch != "" {
		integrationBranch = m.flow.IntegrationBranch
	} else if m.cfg != nil && m.cfg.BaseBranch != "" {
		integrationBranch = m.cfg.BaseBranch
	}
	remoteIntegrationBranch := "origin/" + integrationBranch

	mergedResults := make([]bool, len(services))
	mergeErrs := make([]error, len(services))
	m.runWorkflowChecks(len(services), func(i int) {
		svc := services[i]
		mergedResults[i], mergeErrs[i] = m.git.IsAncestor(ctx, svc.RepoPath, svc.Branch, remoteIntegrationBranch)
	})
	allMerged := len(services) > 0
	for i, svc := range services {
		if mergeErrs[i] != nil {
			return domain.WorkflowSummary{}, fmt.Errorf("workflow: check service %s merged into %s: %w", svc.Name, remoteIntegrationBranch, mergeErrs[i])
		}
		allMerged = allMerged && mergedResults[i]
	}
	if allMerged {
		rows := make([]domain.ServiceWorkflow, len(services))
		for i, svc := range services {
			rows[i] = domain.ServiceWorkflow{ServiceName: svc.Name, Status: "merged", Detail: "merged into " + remoteIntegrationBranch}
		}
		return workflowSummaryWithServices(taskWorkflowSteps, domain.TaskWorkflowReleaseEligible, "select in release (N)", "", true, false, rows), nil
	}

	inspection, err := m.InspectTaskMerge(ctx, taskID)
	if err != nil {
		return domain.WorkflowSummary{}, err
	}

	missingMR, ready, waiting := false, false, 0
	servicesByName := make(map[string]domain.Service, len(services))
	for _, svc := range services {
		servicesByName[svc.Name] = svc
	}
	for _, item := range inspection.Services {
		if item.MR.Number == 0 {
			missingMR = true
			continue
		}
		if item.Status == "ready" {
			ready = true
		} else {
			waiting++
		}
	}
	rows := make([]domain.ServiceWorkflow, len(inspection.Services))
	for i, item := range inspection.Services {
		rows[i] = domain.ServiceWorkflow{ServiceName: item.ServiceName, Status: item.Status, Detail: strings.Join(item.Blockers, "; ")}
	}

	if ready {
		return workflowSummaryWithServices(taskWorkflowSteps, domain.TaskWorkflowMerge, "press M to merge ready MRs", "", false, false, rows), nil
	}
	if missingMR {
		pushedResults := make([]bool, len(inspection.Services))
		pushErrs := make([]error, len(inspection.Services))
		m.runWorkflowChecks(len(inspection.Services), func(i int) {
			item := inspection.Services[i]
			if item.MR.Number == 0 {
				svc := servicesByName[item.ServiceName]
				pushedResults[i], pushErrs[i] = m.git.RemoteBranchExists(ctx, svc.RepoPath, svc.Branch)
			} else {
				pushedResults[i] = true
			}
		})
		allMissingBranchesPushed := true
		for i, item := range inspection.Services {
			if pushErrs[i] != nil {
				svc := servicesByName[item.ServiceName]
				return domain.WorkflowSummary{}, fmt.Errorf("workflow: check pushed branch for service %s: %w", svc.Name, pushErrs[i])
			}
			allMissingBranchesPushed = allMissingBranchesPushed && pushedResults[i]
		}
		if allMissingBranchesPushed {
			return workflowSummaryWithServices(taskWorkflowSteps, domain.TaskWorkflowMR, "press C to create MRs", "", false, false, rows), nil
		}
		return workflowSummaryWithServices(taskWorkflowSteps, domain.TaskWorkflowCode, "press C to create MRs", "", false, false, rows), nil
	}
	blocker := fmt.Sprintf("%d services waiting for approval/CI", waiting)
	if waiting == 1 {
		blocker = "1 service waiting for approval/CI"
	}
	return workflowSummaryWithServices(taskWorkflowSteps, domain.TaskWorkflowReviewCI, "wait for review/CI, then M", blocker, false, false, rows), nil
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
		rows[i] = domain.ServiceWorkflow{ServiceName: service.Name, Status: string(service.Status), Detail: detail}
	}
	return workflowSummaryWithServices(releaseWorkflowSteps, current, next, blocker, done, blocked, rows)
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
