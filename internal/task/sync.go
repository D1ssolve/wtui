package task

import (
	"context"
	"fmt"
	"sync"
)

// SyncTask fetches and rebases all service worktrees of taskID in parallel.
// Progress lines are written to lineCh. lineCh is closed when all goroutines finish.
func (m *manager) SyncTask(ctx context.Context, taskID string, lineCh chan<- string) error {
	defer close(lineCh)

	if err := validateTaskID(taskID); err != nil {
		return err
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

			lineCh <- fmt.Sprintf("[%s] fetching...", svc.Name)
			if err := m.git.Fetch(ctx, svc.WorktreePath); err != nil {
				lineCh <- fmt.Sprintf("[%s] fetch error: %v", svc.Name, err)

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
			lineCh <- fmt.Sprintf("[%s] rebasing onto %s...", svc.Name, upstream)
			if err := m.git.Rebase(ctx, svc.WorktreePath, upstream); err != nil {
				lineCh <- fmt.Sprintf("[%s] rebase error: %v", svc.Name, err)

				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}

			lineCh <- fmt.Sprintf("[%s] done.", svc.Name)
		})
	}

	wg.Wait()
	return firstErr
}
