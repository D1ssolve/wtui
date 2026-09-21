package task

import (
	"context"
	"fmt"

	"github.com/D1ssolve/wtui/internal/domain"
)

func (m *manager) validateSourceWorktreeState(ctx context.Context, svc domain.Service) error {
	requireClean := m.cfg != nil && m.cfg.Release != nil && m.cfg.Release.RequireCleanBeforeMerge != nil && *m.cfg.Release.RequireCleanBeforeMerge
	if requireClean {
		dirty, err := m.git.IsDirty(ctx, svc.WorktreePath)
		if err != nil {
			return fmt.Errorf("release plan: check dirty service=%s: %w", svc.Name, err)
		}
		if dirty {
			return fmt.Errorf("%w: service=%s", ErrReleaseDirtyWorktree, svc.Name)
		}
	}

	states, err := m.git.OperationState(ctx, svc.WorktreePath)
	if err != nil {
		return fmt.Errorf("release plan: check git operation state service=%s: %w", svc.Name, err)
	}
	for _, state := range states {
		if isBlockingReleaseRepoState(state) {
			return fmt.Errorf("%w: service=%s state=%d", ErrReleaseOperationInProgress, svc.Name, state)
		}
	}

	return nil
}

func isBlockingReleaseRepoState(state domain.RepoState) bool {
	switch state {
	case domain.RepoStateConflicted,
		domain.RepoStateMerging,
		domain.RepoStateRebasing,
		domain.RepoStateCherryPick,
		domain.RepoStateReverting,
		domain.RepoStateBisect:
		return true
	default:
		return false
	}
}
