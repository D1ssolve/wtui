package task

import (
	"context"
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
)

func (m *manager) ForgeCreateMR(ctx context.Context, taskID, serviceName string, params forge.CreateMRParams) (forge.MRInfo, error) {
	svc, err := m.findService(ctx, taskID, serviceName)
	if err != nil {
		return forge.MRInfo{}, err
	}

	client, err := m.forgeClientForService(ctx, svc)
	if err != nil {
		return forge.MRInfo{}, err
	}

	if strings.TrimSpace(params.WorktreePath) == "" {
		params.WorktreePath = svc.WorktreePath
	}
	if strings.TrimSpace(params.SourceBranch) == "" {
		params.SourceBranch = svc.Branch
	}
	if strings.TrimSpace(params.TargetBranch) == "" {
		params.TargetBranch = svc.BaseBranch
	}
	if strings.TrimSpace(params.Repo) == "" {
		params.Repo = forge.ExtractRepoPath(svc.RemoteURL)
		if params.Repo == "" {
			return forge.MRInfo{}, fmt.Errorf("resolve repository path for %s: remote URL %q is not parseable", svc.Name, svc.RemoteURL)
		}
	}

	return client.CreateMR(ctx, params)
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
