package task

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func (m *manager) SyncService(ctx context.Context, taskID, serviceName string, strategy SyncStrategy, lineCh chan<- string) error {
	defer close(lineCh)

	if err := validateTaskID(taskID); err != nil {
		return err
	}

	if strategy == SyncStrategyNoop {
		sendLine(ctx, lineCh, "sync skipped.")
		return nil
	}

	worktreePath := filepath.Join(m.taskDir(taskID), serviceName)
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return fmt.Errorf("%w: service %s not in task %s", ErrServiceNotFound, serviceName, taskID)
	} else if err != nil {
		return fmt.Errorf("sync service: stat worktree %s: %w", worktreePath, err)
	}

	// Check dirty state before syncing
	dirty, dirtyErr := m.git.IsDirty(ctx, worktreePath)
	if dirtyErr != nil {
		sendLine(ctx, lineCh, fmt.Sprintf("[%s] could not check dirty state, proceeding...", serviceName))
	} else if dirty {
		sendLine(ctx, lineCh, fmt.Sprintf("[%s] dirty working tree, stash or commit first.", serviceName))
		return nil
	}

	if !sendLine(ctx, lineCh, fmt.Sprintf("[%s] fetching...", serviceName)) {
		return ctx.Err()
	}
	if err := m.git.Fetch(ctx, worktreePath); err != nil {
		sendLine(ctx, lineCh, fmt.Sprintf("[%s] fetch error: %v", serviceName, err))
		return fmt.Errorf("sync %s/%s: fetch: %w", taskID, serviceName, err)
	}

	baseBranch := m.cfg.BaseBranch
	if baseBranch == "" {
		baseBranch = "develop"
	}
	upstream := "origin/" + baseBranch

	// Re-check behind count after fetch
	_, behind, abErr := m.git.RevListAheadBehind(ctx, worktreePath, upstream)
	if abErr != nil {
		sendLine(ctx, lineCh, fmt.Sprintf("[%s] could not determine ahead/behind, proceeding...", serviceName))
	} else if behind == 0 {
		sendLine(ctx, lineCh, fmt.Sprintf("[%s] already up to date.", serviceName))
		return nil
	}

	switch strategy {
	case SyncStrategyMerge:
		if !sendLine(ctx, lineCh, fmt.Sprintf("[%s] merging %s...", serviceName, upstream)) {
			return ctx.Err()
		}
		if err := m.git.Merge(ctx, worktreePath, upstream); err != nil {
			sendLine(ctx, lineCh, fmt.Sprintf("[%s] merge error: %v", serviceName, err))
			return fmt.Errorf("sync %s/%s: merge: %w", taskID, serviceName, err)
		}
	case SyncStrategyRebase:
		if !sendLine(ctx, lineCh, fmt.Sprintf("[%s] rebasing onto %s...", serviceName, upstream)) {
			return ctx.Err()
		}
		if err := m.git.Rebase(ctx, worktreePath, upstream); err != nil {
			sendLine(ctx, lineCh, fmt.Sprintf("[%s] rebase error: %v", serviceName, err))
			return fmt.Errorf("sync %s/%s: rebase: %w", taskID, serviceName, err)
		}
	}

	sendLine(ctx, lineCh, fmt.Sprintf("[%s] done.", serviceName))
	return nil
}