package task

import (
	"context"
	"fmt"
	"os"

	"github.com/D1ssolve/wtui/internal/domain"
)

func (m *manager) ValidateTask(ctx context.Context, taskID string) (domain.TaskValidation, error) {
	if err := validateTaskID(taskID); err != nil {
		return domain.TaskValidation{}, err
	}

	taskDir := m.taskDir(taskID)
	if _, err := os.Stat(taskDir); os.IsNotExist(err) {
		return domain.TaskValidation{}, fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	} else if err != nil {
		return domain.TaskValidation{}, fmt.Errorf("validate task: stat task dir %s: %w", taskDir, err)
	}

	services, err := m.ListServices(ctx, taskID)
	if err != nil {
		return domain.TaskValidation{}, err
	}

	if m.validator == nil {
		return domain.TaskValidation{}, fmt.Errorf("validate task: validator is not configured")
	}

	return m.validator.ValidateTask(ctx, taskID, services, m.cfg.Validation)
}
