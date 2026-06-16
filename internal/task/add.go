package task

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func (m *manager) Add(ctx context.Context, params AddParams) error {
	if err := validateTaskID(params.TaskID); err != nil {
		return err
	}

	taskDir := m.taskDir(params.TaskID)

	if _, err := os.Stat(taskDir); os.IsNotExist(err) {
		return fmt.Errorf("%w: %s", ErrTaskNotFound, params.TaskID)
	} else if err != nil {
		return fmt.Errorf("add: stat task directory %s: %w", taskDir, err)
	}

	branchType, rule := m.resolveInitRule(params.BranchType)
	branchPrefix := ""
	if len(rule.Prefixes) > 0 {
		branchPrefix = strings.TrimSpace(rule.Prefixes[0])
	}
	branchName := m.resolveBranchName(branchPrefix, params.TaskID)

	baseBranch := strings.TrimSpace(rule.BaseBranch)
	if branchType == gitflow.BranchTypeHotfix && m.flow != nil && strings.TrimSpace(m.flow.ProductionBranch) != "" {
		baseBranch = strings.TrimSpace(m.flow.ProductionBranch)
	}

	added, worktreeErrs := m.addWorktreesForServices(
		ctx, params.TaskID, params.Services, taskDir, branchName, baseBranch,
		params.RemoteBranchStrategies, params.BranchSuffixes, params.StatusCh,
	)
	if err := unresolvedRemoteBranchConflict(worktreeErrs); err != nil {
		return fmt.Errorf("add: remote branch conflicts for task %s: %w", params.TaskID, err)
	}

	if len(params.Services) > 0 && added == 0 {
		return fmt.Errorf("add: no worktrees added for task %s: %w",
			params.TaskID, errors.Join(worktreeErrs...))
	}

	if err := generateWorkspaceFile(params.TaskID, taskDir); err != nil {
		m.logger.WarnContext(ctx, "failed to regenerate workspace file",
			slog.String("error", err.Error()),
		)
	}

	allServices := buildServicesFromSubdirs(taskDir)
	if err := m.slnMgr.Generate(ctx, taskDir, params.TaskID, allServices); err != nil {
		m.logger.WarnContext(ctx, "sln generation failed during add",
			slog.String("error", err.Error()),
		)
	}

	return nil
}

func buildServicesFromSubdirs(taskDir string) []domain.Service {
	discovered, err := discoverServicesFromTaskDir(taskDir)
	if err != nil {
		return nil
	}

	services := make([]domain.Service, 0, len(discovered))
	for _, svc := range discovered {
		services = append(services, domain.Service{
			Name:         svc.Name,
			WorktreePath: svc.RepoPath,
		})
	}

	return services
}
