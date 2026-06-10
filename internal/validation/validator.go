package validation

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
)

const defaultValidationConcurrency = 8

type TaskValidator struct {
	gitClient git.Client
}

func NewTaskValidator(gitClient git.Client) *TaskValidator {
	return &TaskValidator{gitClient: gitClient}
}

func (v *TaskValidator) ValidateTask(ctx context.Context, taskID string, services []domain.Service, cfg *config.ValidationConfig) (domain.TaskValidation, error) {
	result := domain.TaskValidation{
		TaskID:    taskID,
		Services:  make([]domain.ServiceValidation, len(services)),
		AllClean:  true,
		Blocking:  false,
	}

	concurrency := defaultValidationConcurrency
	if cfg != nil && cfg.Concurrency > 0 {
		concurrency = cfg.Concurrency
	}

	sem := make(chan struct{}, concurrency)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup

	for i, svc := range services {
		if err := ctx.Err(); err != nil {
			return domain.TaskValidation{}, err
		}

		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return domain.TaskValidation{}, ctx.Err()
		}

		wg.Add(1)
		go func(index int, service domain.Service) {
			defer wg.Done()
			defer func() { <-sem }()

			serviceValidation, err := v.validateService(ctx, service, cfg)
			result.Services[index] = serviceValidation
			if err != nil {
				select {
				case errCh <- err:
				default:
				}
			}
		}(i, svc)
	}

	wg.Wait()

	select {
	case err := <-errCh:
		return domain.TaskValidation{}, err
	default:
	}

	for _, serviceValidation := range result.Services {
		if !isExactlyClean(serviceValidation.States) {
			result.AllClean = false
		}
		if isBlocking(serviceValidation, cfg) {
			result.Blocking = true
		}
	}

	return result, nil
}

func (v *TaskValidator) validateService(ctx context.Context, svc domain.Service, cfg *config.ValidationConfig) (domain.ServiceValidation, error) {
	serviceValidation := domain.ServiceValidation{
		ServiceName:  svc.Name,
		WorktreePath: svc.WorktreePath,
		Branch:       svc.Branch,
	}

	status, err := v.gitClient.RepoStatus(ctx, svc.WorktreePath)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return domain.ServiceValidation{}, err
		}
		serviceValidation.Err = err
		serviceValidation.States = []domain.RepoState{domain.RepoStateUnreachable}
		return serviceValidation, nil
	}

	serviceValidation.Branch = status.Branch

	operationStates, err := v.gitClient.OperationState(ctx, svc.WorktreePath)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return domain.ServiceValidation{}, err
		}
		serviceValidation.Err = err
		serviceValidation.States = []domain.RepoState{domain.RepoStateUnreachable}
		return serviceValidation, nil
	}

	states, changedCount, untrackedCount, conflictPaths := ParsePorcelainV2(renderPorcelain(status))
	serviceValidation.States = append(serviceValidation.States, states...)
	serviceValidation.ChangedCount = changedCount
	serviceValidation.UntrackedCount = untrackedCount
	serviceValidation.ConflictPaths = conflictPaths

	if status.Branch == "HEAD" {
		serviceValidation.States = appendUniqueState(serviceValidation.States, domain.RepoStateDetached)
	}

	for _, opState := range operationStates {
		serviceValidation.States = appendUniqueState(serviceValidation.States, opState)
	}

	if len(serviceValidation.States) > 1 && containsState(serviceValidation.States, domain.RepoStateClean) {
		serviceValidation.States = slices.DeleteFunc(serviceValidation.States, func(state domain.RepoState) bool {
			return state == domain.RepoStateClean
		})
	}

	return serviceValidation, nil
}

func renderPorcelain(status git.RawStatus) string {
	var lines []string
	for _, entry := range status.ChangedEntries {
		lines = append(lines, entry.XY+" "+entry.Path)
	}
	for _, path := range status.UntrackedPaths {
		lines = append(lines, "?? "+path)
	}
	for _, path := range status.ConflictPaths {
		if !containsPathInEntries(status.ChangedEntries, path) {
			lines = append(lines, "UU "+path)
		}
	}
	return strings.Join(lines, "\n")
}

func containsPathInEntries(entries []git.StatusEntry, target string) bool {
	for _, entry := range entries {
		if entry.Path == target {
			return true
		}
	}
	return false
}

func appendUniqueState(states []domain.RepoState, state domain.RepoState) []domain.RepoState {
	if containsState(states, state) {
		return states
	}
	return append(states, state)
}

func containsState(states []domain.RepoState, state domain.RepoState) bool {
	for _, s := range states {
		if s == state {
			return true
		}
	}
	return false
}

func isExactlyClean(states []domain.RepoState) bool {
	return len(states) == 1 && states[0] == domain.RepoStateClean
}

func isBlocking(serviceValidation domain.ServiceValidation, cfg *config.ValidationConfig) bool {
	if containsState(serviceValidation.States, domain.RepoStateUnreachable) {
		return true
	}
	if containsState(serviceValidation.States, domain.RepoStateDirty) {
		return true
	}
	if containsState(serviceValidation.States, domain.RepoStateConflicted) {
		return true
	}

	if shouldBlockDetachedHead(cfg) && containsState(serviceValidation.States, domain.RepoStateDetached) {
		return true
	}

	if shouldBlockUntracked(cfg) && containsState(serviceValidation.States, domain.RepoStateUntracked) {
		return true
	}

	if shouldBlockInterruptedOps(cfg) {
		if containsState(serviceValidation.States, domain.RepoStateMerging) ||
			containsState(serviceValidation.States, domain.RepoStateRebasing) ||
			containsState(serviceValidation.States, domain.RepoStateCherryPick) ||
			containsState(serviceValidation.States, domain.RepoStateReverting) ||
			containsState(serviceValidation.States, domain.RepoStateBisect) {
			return true
		}
	}

	return false
}

func shouldBlockUntracked(cfg *config.ValidationConfig) bool {
	if cfg == nil {
		return false
	}
	return cfg.BlockUntracked
}

func shouldBlockDetachedHead(cfg *config.ValidationConfig) bool {
	if cfg == nil {
		return true
	}
	return cfg.BlockDetachedHead
}

func shouldBlockInterruptedOps(cfg *config.ValidationConfig) bool {
	if cfg == nil {
		return true
	}
	return cfg.BlockInterruptedOperations
}
