package task

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/diss0x/wtui/internal/domain"
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

	m.addWorktreesForServices(ctx, params.Services, taskDir, branchName, "")

	if err := generateWorkspaceFile(params.TaskID, taskDir); err != nil {
		m.logger.WarnContext(ctx, "failed to regenerate workspace file",
			"task_id", params.TaskID,
			"error", err.Error(),
		)
	}

	allServices := buildServicesFromSubdirs(taskDir)
	if err := m.slnMgr.Generate(ctx, taskDir, params.TaskID, allServices); err != nil {
		m.logger.WarnContext(ctx, "sln generation failed during add",
			"task_id", params.TaskID,
			"error", err.Error(),
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
