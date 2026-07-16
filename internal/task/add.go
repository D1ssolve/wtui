package task

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func (m *manager) Add(ctx context.Context, params AddParams) (PartialFailureResult, error) {
	if err := validateTaskID(params.TaskID); err != nil {
		return PartialFailureResult{}, err
	}

	taskDir := m.taskDir(params.TaskID)

	if _, err := os.Stat(taskDir); os.IsNotExist(err) {
		return PartialFailureResult{}, fmt.Errorf("%w: %s", ErrTaskNotFound, params.TaskID)
	} else if err != nil {
		return PartialFailureResult{}, fmt.Errorf("add: stat task directory %s: %w", taskDir, err)
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

	results := m.addWorktreesForServices(
		ctx, params.TaskID, params.Services, taskDir, branchName, baseBranch,
		params.RemoteBranchStrategies, params.BranchSuffixes, params.StatusCh,
	)
	summary := summarizeServiceResults(params.TaskID, "add", params.Services, results)

	if len(params.Services) > 0 && len(summary.succeededServices) == 0 {
		return PartialFailureResult{}, fmt.Errorf("add: no worktrees added for task %s: %w",
			params.TaskID, summary.JoinedError())
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

	if summary.HasPartialFailure() {
		return summary.PartialResult(), &ErrPartialFailure{Result: summary.PartialResult()}
	}

	return PartialFailureResult{}, nil
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
