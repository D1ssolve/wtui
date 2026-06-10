package task

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

type SyncStrategy int

const (
	SyncStrategyMerge SyncStrategy = iota

	SyncStrategyRebase

	SyncStrategyNoop
)

func (s SyncStrategy) String() string {
	switch s {
	case SyncStrategyMerge:
		return "merge"
	case SyncStrategyRebase:
		return "rebase"
	case SyncStrategyNoop:
		return "noop"
	default:
		return "unknown"
	}
}

func (m *manager) SyncTask(ctx context.Context, taskID string, strategy SyncStrategy, lineCh chan<- string) error {
	defer close(lineCh)

	if err := validateTaskID(taskID); err != nil {
		return err
	}

	if strategy == SyncStrategyNoop {
		sendLine(ctx, lineCh, "sync skipped.")
		return nil
	}

	validationResult, err := m.ValidateTask(ctx, taskID)
	if err != nil {
		return err
	}

	m.logger.InfoContext(ctx, "sync task validation finished",
		slog.String("task_id", taskID),
		slog.Bool("blocking", validationResult.Blocking),
		slog.Bool("all_clean", validationResult.AllClean),
		slog.Int("services", len(validationResult.Services)),
	)

	if validationResult.Blocking {
		m.logger.WarnContext(ctx, "sync task blocked by validation",
			slog.String("task_id", taskID),
		)
		return fmt.Errorf("sync task %s: %w", taskID, ErrValidationFailed)
	}

	services, err := m.ListServices(ctx, taskID)
	if err != nil {
		return err
	}

	if len(services) == 0 {
		return nil
	}

	baseBranch := m.cfg.BaseBranch
	if baseBranch == "" {
		baseBranch = "develop"
	}
	upstream := "origin/" + baseBranch

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)

	for _, svc := range services {
		wg.Go(func() {
			// Skip stale services (worktree path missing)
			if svc.Stale {
				sendLine(ctx, lineCh, fmt.Sprintf("[%s] worktree missing, skipping.", svc.Name))
				return
			}

			// Skip dirty services — merge/rebase will fail on dirty working tree
			if svc.IsDirty {
				sendLine(ctx, lineCh, fmt.Sprintf("[%s] dirty working tree, stash or commit first.", svc.Name))
				return
			}

			if !sendLine(ctx, lineCh, fmt.Sprintf("[%s] fetching...", svc.Name)) {
				mu.Lock()
				if firstErr == nil {
					firstErr = ctx.Err()
				}
				mu.Unlock()
				return
			}
			if err := m.git.Fetch(ctx, svc.WorktreePath); err != nil {
				sendLine(ctx, lineCh, fmt.Sprintf("[%s] fetch error: %v", svc.Name, err))

				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}

			// Re-check behind count after fetch
			_, behind, abErr := m.git.RevListAheadBehind(ctx, svc.WorktreePath, upstream)
			if abErr != nil {
				// Can't determine status, proceed with merge/rebase anyway
				sendLine(ctx, lineCh, fmt.Sprintf("[%s] could not determine ahead/behind, proceeding...", svc.Name))
			} else if behind == 0 {
				sendLine(ctx, lineCh, fmt.Sprintf("[%s] already up to date.", svc.Name))
				return
			}

			switch strategy {
			case SyncStrategyMerge:
				if !sendLine(ctx, lineCh, fmt.Sprintf("[%s] merging %s...", svc.Name, upstream)) {
					mu.Lock()
					if firstErr == nil {
						firstErr = ctx.Err()
					}
					mu.Unlock()
					return
				}
				if err := m.git.Merge(ctx, svc.WorktreePath, upstream); err != nil {
					sendLine(ctx, lineCh, fmt.Sprintf("[%s] merge error: %v", svc.Name, err))

					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					return
				}
			case SyncStrategyRebase:
				if !sendLine(ctx, lineCh, fmt.Sprintf("[%s] rebasing onto %s...", svc.Name, upstream)) {
					mu.Lock()
					if firstErr == nil {
						firstErr = ctx.Err()
					}
					mu.Unlock()
					return
				}
				if err := m.git.Rebase(ctx, svc.WorktreePath, upstream); err != nil {
					sendLine(ctx, lineCh, fmt.Sprintf("[%s] rebase error: %v", svc.Name, err))

					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					return
				}
			}

			sendLine(ctx, lineCh, fmt.Sprintf("[%s] done.", svc.Name))
		})
	}

	wg.Wait()
	return firstErr
}
