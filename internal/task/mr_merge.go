package task

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

type TaskMergeInspection struct {
	TaskID   string
	Services []ServiceMergeInspection
}

type ServiceMergeInspection struct {
	ServiceName string
	Status      string
	MR          forge.MRReadiness
	Blockers    []string
}

type TaskMergeResult struct {
	TaskID  string
	Merged  []string
	Skipped []string
	Steps   []string
	Errs    map[string]error
}

func (m *manager) InspectTaskMerge(ctx context.Context, taskID string) (TaskMergeInspection, error) {
	inspection, _, err := m.inspectTaskMerge(ctx, taskID)
	return inspection, err
}

func (m *manager) inspectTaskMerge(ctx context.Context, taskID string) (TaskMergeInspection, map[string]domain.Service, error) {
	services, err := m.ListServices(ctx, taskID)
	if err != nil {
		return TaskMergeInspection{}, nil, err
	}

	inspection := TaskMergeInspection{TaskID: taskID, Services: make([]ServiceMergeInspection, len(services))}
	servicesByName := make(map[string]domain.Service, len(services))
	for _, svc := range services {
		servicesByName[svc.Name] = svc
	}
	sem := make(chan struct{}, m.concurrency())
	var wg sync.WaitGroup
	for i, svc := range services {
		i, svc := i, svc
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			item := ServiceMergeInspection{ServiceName: svc.Name}

			client, clientErr := m.forgeClientForService(ctx, svc)
			if clientErr != nil {
				item.Status = "failed"
				item.Blockers = []string{clientErr.Error()}
				inspection.Services[i] = item
				return
			}

			repo := forge.ExtractRepoPath(svc.RemoteURL)
			if repo == "" {
				item.Status = "failed"
				item.Blockers = []string{fmt.Sprintf("resolve repository path for %s: remote URL %q is not parseable", svc.Name, svc.RemoteURL)}
				inspection.Services[i] = item
				return
			}

			var readErr error
			item.MR, readErr = client.MRReadiness(ctx, svc.Branch, repo, svc.WorktreePath)
			item.Blockers = append([]string(nil), item.MR.Blockers...)
			switch {
			case readErr != nil:
				item.Status = "failed"
				item.Blockers = []string{readErr.Error()}
			case item.MR.Number == 0:
				item.Status = "no_mr"
			case item.MR.Ready:
				item.Status = "ready"
			case waitingBlockers(item.Blockers):
				item.Status = "waiting"
			default:
				item.Status = "blocked"
			}
			inspection.Services[i] = item
		}()
	}
	wg.Wait()

	return inspection, servicesByName, nil
}

func (m *manager) MergeTaskMRs(ctx context.Context, taskID string) (TaskMergeResult, error) {
	return m.mergeTaskMRs(ctx, taskID, "")
}

func (m *manager) MergeServiceMR(ctx context.Context, taskID, serviceName string) (TaskMergeResult, error) {
	return m.mergeTaskMRs(ctx, taskID, serviceName)
}

func (m *manager) mergeTaskMRs(ctx context.Context, taskID, serviceName string) (TaskMergeResult, error) {
	inspection, services, err := m.inspectTaskMerge(ctx, taskID)
	if err != nil {
		return TaskMergeResult{}, err
	}

	result := TaskMergeResult{TaskID: taskID, Errs: make(map[string]error)}
	for _, item := range inspection.Services {
		if serviceName != "" && item.ServiceName != serviceName {
			continue
		}
		if item.Status != "ready" {
			result.Skipped = append(result.Skipped, item.ServiceName)
			result.Steps = append(result.Steps, item.ServiceName+": "+item.Status)
			if item.Status == "failed" {
				result.Errs[item.ServiceName] = errors.New(strings.Join(item.Blockers, "; "))
			}
			continue
		}

		svc := services[item.ServiceName]
		client, clientErr := m.forgeClientForService(ctx, svc)
		if clientErr != nil {
			recordMergeFailure(&result, item.ServiceName, clientErr)
			continue
		}
		params := forge.MergeMRParams{
			WorktreePath: svc.WorktreePath,
			Repo:         forge.ExtractRepoPath(svc.RemoteURL),
			Number:       item.MR.Number,
			Method:       m.mergeMethodForBranch(svc.Branch),
		}
		if item.MR.SupportsSHAPin {
			params.ExpectedHeadSHA = item.MR.HeadSHA
		} else {
			fresh, readinessErr := client.MRReadinessByNumber(ctx, item.MR.Number, params.Repo, svc.WorktreePath)
			if readinessErr != nil {
				recordMergeFailure(&result, item.ServiceName, readinessErr)
				continue
			}
			if fresh.HeadSHA != item.MR.HeadSHA {
				recordMergeFailure(&result, item.ServiceName, fmt.Errorf("head SHA drift: inspected=%s current=%s", item.MR.HeadSHA, fresh.HeadSHA))
				continue
			}
			if m.logger != nil {
				m.logger.WarnContext(ctx, "forge does not support SHA-pinned merge", slog.String("service", item.ServiceName))
			}
		}

		merged, mergeErr := client.MergeMR(ctx, params)
		if mergeErr != nil {
			recordMergeFailure(&result, item.ServiceName, mergeErr)
			continue
		}
		if !merged.Merged {
			recordMergeFailure(&result, item.ServiceName, errors.New("forge did not report merge success"))
			continue
		}

		result.Merged = append(result.Merged, item.ServiceName)
		result.Steps = append(result.Steps, item.ServiceName+": merged")
	}

	return result, nil
}

func (m *manager) mergeMethodForBranch(branch string) string {
	if m.flow == nil {
		return ""
	}
	rule, ok := m.flow.BranchTypes[gitflow.DetectBranchType(branch, m.flow)]
	if !ok {
		return ""
	}
	return forgeMergeMethod(rule.MergeStrategy)
}

func forgeMergeMethod(strategy gitflow.MergeStrategy) string {
	if strategy == gitflow.MergeStrategyMerge {
		return "merge"
	}
	return string(strategy)
}

func waitingBlockers(blockers []string) bool {
	if len(blockers) == 0 {
		return false
	}
	for _, blocker := range blockers {
		switch strings.ToLower(strings.TrimSpace(blocker)) {
		case "not approved", "checks pending", "pipeline pending":
		default:
			return false
		}
	}
	return true
}

func recordMergeFailure(result *TaskMergeResult, service string, err error) {
	result.Skipped = append(result.Skipped, service)
	result.Errs[service] = err
	result.Steps = append(result.Steps, service+": failed: "+err.Error())
}
