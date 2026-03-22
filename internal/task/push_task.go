package task

import (
	"context"
	"fmt"
	"sync"
)

// PushTask pushes all service worktrees of taskID in parallel.
// Analogous to SyncTask but for push operations.
// Lines written to lineCh describe progress. lineCh is closed when done.
func (m *manager) PushTask(ctx context.Context, taskID string, lineCh chan<- string) error {
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

			lineCh <- fmt.Sprintf("[%s] pushing...", svc.Name)
			if err := m.git.Push(ctx, svc.WorktreePath, lineCh); err != nil {
				lineCh <- fmt.Sprintf("[%s] push error: %v", svc.Name, err)

				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}

			lineCh <- fmt.Sprintf("[%s] pushed.", svc.Name)
		})
	}

	wg.Wait()
	return firstErr
}
