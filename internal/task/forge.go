package task

import (
	"context"
	"fmt"
	"strconv"
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

func (m *manager) ForgeCreateMissingMRs(ctx context.Context, taskID, title string, force bool, descriptions ...string) (TaskMRCreateResult, error) {
	services, err := m.ListServices(ctx, taskID)
	if err != nil {
		return TaskMRCreateResult{}, err
	}
	if strings.TrimSpace(title) == "" {
		title = taskID
	}
	description := ""
	if len(descriptions) > 0 {
		description = descriptions[0]
	}
	issue := m.taskIssue(ctx, m.taskDir(taskID))

	result := TaskMRCreateResult{TaskID: taskID, Title: title, Services: make([]ServiceMRCreateResult, 0, len(services))}
	for _, svc := range services {
		item := ServiceMRCreateResult{ServiceName: svc.Name}
		if m.hasServiceRule(svc) {
			rule := m.branchRule(svc.Name, svc.Branch)
			if rule.CloseStrategy != gitflow.CloseStrategyReviewRequest || len(rule.ReviewTargets) != 1 {
				item.Status = "failed"
				item.Err = fmt.Errorf("%s: use the configured close workflow", svc.Name)
				result.Services = append(result.Services, item)
				continue
			}
		}
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
		target := m.reviewTarget(svc.Name, svc.Branch)
		if len(mrs) > 0 {
			if m.hasServiceRule(svc) && (len(mrs) != 1 || mrs[0].SourceBranch != svc.Branch || mrs[0].TargetBranch != target) {
				item.Status = "failed"
				item.Err = fmt.Errorf("%s: expected one existing MR from %s into %s", svc.Name, svc.Branch, target)
				result.Services = append(result.Services, item)
				continue
			}
			item.Status, item.MR = "existing", mrs[0]
			result.Services = append(result.Services, item)
			continue
		}

		if !force {
			if reason := m.mrCreateConfirmReason(ctx, svc, client, repo, target); reason != "" {
				item.Status, item.Reason = "confirm", reason
				result.Services = append(result.Services, item)
				continue
			}
		}

		params := withMRPolicy(forge.CreateMRParams{
			WorktreePath: svc.WorktreePath,
			SourceBranch: svc.Branch,
			TargetBranch: target,
			Title:        title,
			Description:  description,
			Repo:         repo,
		}, m.branchRule(svc.Name, svc.Branch).MR)
		item.MR, item.Err = client.CreateMR(ctx, m.withIssueReference(params, issue, svc.RemoteURL))
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
		ahead, err := m.git.RevListCount(ctx, svc.WorktreePath, svc.Branch, m.cfg.ServiceRemote().Tracking(target))
		if err == nil && ahead == 0 {
			return fmt.Sprintf("no changes vs %s; MR will be empty", target)
		}
	}
	return ""
}

func (m *manager) reviewTarget(service, branch string) string {
	rule := m.branchRule(service, branch)
	if len(rule.ReviewTargets) > 0 {
		return rule.ReviewTargets[0]
	}
	if rule.BaseBranch != "" {
		return rule.BaseBranch
	}
	return m.integrationBranch()
}

func (m *manager) hasServiceRule(svc domain.Service) bool {
	if m.flow == nil {
		return false
	}
	_, ok := m.flow.ServiceOverride(svc.Name, gitflow.DetectBranchType(svc.Branch, m.flow))
	return ok
}

// branchRule is the configured rule for branch's type; zero without a flow or a matching type.
func (m *manager) branchRule(service, branch string) gitflow.BranchTypeRule {
	if m.flow == nil {
		return gitflow.BranchTypeRule{}
	}
	return m.flow.RuleFor(service, gitflow.DetectBranchType(branch, m.flow))
}

// withMRPolicy applies the task type's or release's configured MR defaults to params.
func withMRPolicy(params forge.CreateMRParams, policy gitflow.MRPolicy) forge.CreateMRParams {
	params.Draft, params.RemoveSource = policy.Draft, policy.RemoveSourceBranch
	params.Labels, params.Reviewers, params.Assignees = policy.Labels, policy.Reviewers, policy.Assignees
	return params
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

	rows, err := client.ListIssues(ctx, params)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		link, _ := forge.ParseIssue(rows[i].URL, nil, m.cfg.Forge)
		if link != nil && link.Provider != domain.IssueJira && link.Key == strconv.Itoa(rows[i].Number) {
			link.Title = rows[i].Title
			rows[i].Link = link
		}
	}
	return rows, nil
}

func (m *manager) forgeClientForService(ctx context.Context, svc domain.Service) (forge.ForgeClient, error) {
	if len(m.forgeClients) == 0 {
		return nil, forge.ErrForgeUnavailable
	}

	remoteURL, err := m.git.RemoteURL(ctx, svc.WorktreePath, m.cfg.ServiceRemote().String())
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
