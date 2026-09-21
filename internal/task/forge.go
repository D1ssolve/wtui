package task

import (
	"context"
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

type TaskMRCreateResult struct {
	TaskID   string
	Title    string
	Services []ServiceMRCreateResult
}

type ServiceMRCreateResult struct {
	ServiceName string
	Status      string
	Reason      string
	MR          forge.MRInfo
	Err         error
}

func (m *manager) ForgeCreateMissingMRs(ctx context.Context, taskID, title string, force bool) (TaskMRCreateResult, error) {
	services, err := m.ListServices(ctx, taskID)
	if err != nil {
		return TaskMRCreateResult{}, err
	}
	if strings.TrimSpace(title) == "" {
		title = taskID
	}

	result := TaskMRCreateResult{TaskID: taskID, Title: title, Services: make([]ServiceMRCreateResult, 0, len(services))}
	for _, svc := range services {
		item := ServiceMRCreateResult{ServiceName: svc.Name}
		client, clientErr := m.forgeClientForService(ctx, svc)
		if clientErr != nil {
			item.Status, item.Err = "failed", clientErr
			result.Services = append(result.Services, item)
			continue
		}

		repo := forge.ExtractRepoPath(svc.RemoteURL)
		if repo == "" {
			item.Status = "failed"
			item.Err = fmt.Errorf("resolve repository path for %s: remote URL %q is not parseable", svc.Name, svc.RemoteURL)
			result.Services = append(result.Services, item)
			continue
		}

		mrs, statusErr := client.MRStatus(ctx, svc.Branch, repo)
		if statusErr != nil {
			item.Status, item.Err = "failed", statusErr
			result.Services = append(result.Services, item)
			continue
		}
		if len(mrs) > 0 {
			item.Status, item.MR = "existing", mrs[0]
			result.Services = append(result.Services, item)
			continue
		}

		target := m.reviewTarget(svc.Branch)
		if !force {
			if reason := m.mrCreateConfirmReason(ctx, svc, client, repo, target); reason != "" {
				item.Status, item.Reason = "confirm", reason
				result.Services = append(result.Services, item)
				continue
			}
		}

		item.MR, item.Err = client.CreateMR(ctx, forge.CreateMRParams{
			WorktreePath: svc.WorktreePath,
			SourceBranch: svc.Branch,
			TargetBranch: target,
			Title:        title,
			Repo:         repo,
		})
		if item.Err != nil {
			item.Status = "failed"
		} else {
			item.Status = "created"
		}
		result.Services = append(result.Services, item)
	}

	return result, nil
}

// mrCreateConfirmReason reports why creating an MR for svc needs explicit
// confirmation: a previous MR to the same target was closed without merge, or
// the branch has no changes vs the target.
func (m *manager) mrCreateConfirmReason(ctx context.Context, svc domain.Service, client forge.ForgeClient, repo, target string) string {
	if history, ok := client.(forge.HistoryClient); ok {
		if rows, err := history.MRHistory(ctx, svc.Branch, repo); err == nil {
			_, closed := matchTargetMRs(rows, svc.Branch, target)
			if len(closed) > 0 {
				return fmt.Sprintf("MR #%d to %s was closed without merge", closed[0].Number, target)
			}
		}
	}
	if err := m.git.Fetch(ctx, svc.WorktreePath); err == nil {
		ahead, err := m.git.RevListCount(ctx, svc.WorktreePath, svc.Branch, "origin/"+target)
		if err == nil && ahead == 0 {
			return fmt.Sprintf("no changes vs %s; MR will be empty", target)
		}
	}
	return ""
}

func (m *manager) reviewTarget(branch string) string {
	if m.flow != nil {
		if rule, ok := m.flow.BranchTypes[gitflow.DetectBranchType(branch, m.flow)]; ok {
			if len(rule.ReviewTargets) > 0 {
				return rule.ReviewTargets[0]
			}
			if rule.BaseBranch != "" {
				return rule.BaseBranch
			}
		}
	}
	if m.cfg != nil {
		return m.cfg.BaseBranch
	}
	return ""
}

func (m *manager) ForgePipelineStatus(ctx context.Context, taskID, serviceName string, branch string) ([]forge.PipelineStatus, error) {
	svc, err := m.findService(ctx, taskID, serviceName)
	if err != nil {
		return nil, err
	}

	client, err := m.forgeClientForService(ctx, svc)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(branch) == "" {
		branch = svc.Branch
	}

	repo := forge.ExtractRepoPath(svc.RemoteURL)
	if repo == "" {
		return nil, fmt.Errorf("resolve repository path for %s: remote URL %q is not parseable", svc.Name, svc.RemoteURL)
	}

	return client.PipelineStatus(ctx, branch, repo)
}

func (m *manager) ForgeListIssues(ctx context.Context, taskID, serviceName string, params forge.ListIssuesParams) ([]forge.IssueInfo, error) {
	svc, err := m.findService(ctx, taskID, serviceName)
	if err != nil {
		return nil, err
	}

	client, err := m.forgeClientForService(ctx, svc)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(params.WorktreePath) == "" {
		params.WorktreePath = svc.WorktreePath
	}
	if strings.TrimSpace(params.Repo) == "" {
		params.Repo = forge.ExtractRepoPath(svc.RemoteURL)
		if params.Repo == "" {
			return nil, fmt.Errorf("resolve repository path for %s: remote URL %q is not parseable", svc.Name, svc.RemoteURL)
		}
	}

	return client.ListIssues(ctx, params)
}

func (m *manager) forgeClientForService(ctx context.Context, svc domain.Service) (forge.ForgeClient, error) {
	if len(m.forgeClients) == 0 {
		return nil, forge.ErrForgeUnavailable
	}

	remoteURL, err := m.git.RemoteURL(ctx, svc.WorktreePath, "origin")
	if err != nil {
		return nil, fmt.Errorf("resolve forge provider for %s: %w", svc.Name, err)
	}

	provider := forge.DetectProvider(remoteURL, m.cfg.Forge)
	if provider == forge.ForgeProviderUnknown {
		return nil, fmt.Errorf("%w: unsupported provider for remote %s", forge.ErrForgeUnavailable, remoteURL)
	}

	client, ok := m.forgeClients[provider]
	if !ok || client == nil {
		return nil, fmt.Errorf("%w: provider %s", forge.ErrForgeUnavailable, provider)
	}

	return client, nil
}
