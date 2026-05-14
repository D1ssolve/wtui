package task

import (
	"context"
	"fmt"
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

	services, err := m.ListServices(ctx, taskID)
	if err != nil {
		return err
	}

	if len(services) == 0 {
		return nil
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)

	for _, svc := range services {
		wg.Go(func() {
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

			baseBranch := m.cfg.BaseBranch
			if baseBranch == "" {
				baseBranch = "develop"
			}
			upstream := "origin/" + baseBranch

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
