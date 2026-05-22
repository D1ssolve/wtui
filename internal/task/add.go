package task

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/D1ssolve/wtui/internal/domain"
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

	branchName := m.resolveBranchName("", params.TaskID)

	added, worktreeErrs := m.addWorktreesForServices(
		ctx, params.TaskID, params.Services, taskDir, branchName, "",
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
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return nil
	}

	var services []domain.Service
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		services = append(services, domain.Service{
			Name:         entry.Name(),
			WorktreePath: filepath.Join(taskDir, entry.Name()),
		})
	}
	return services
}
