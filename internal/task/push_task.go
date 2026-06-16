package task

import (
	"context"
	"fmt"
	"sync"
)

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
			if !sendLine(ctx, lineCh, fmt.Sprintf("[%s] pushing...", svc.Name)) {
				mu.Lock()
				if firstErr == nil {
					firstErr = ctx.Err()
				}
				mu.Unlock()
				return
			}
			if err := m.ensurePushBranchAllowed(ctx, svc.WorktreePath); err != nil {
				sendLine(ctx, lineCh, fmt.Sprintf("[%s] push error: %v", svc.Name, err))

				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			if err := m.git.Push(ctx, svc.WorktreePath, lineCh); err != nil {
				sendLine(ctx, lineCh, fmt.Sprintf("[%s] push error: %v", svc.Name, err))

				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}

			sendLine(ctx, lineCh, fmt.Sprintf("[%s] pushed.", svc.Name))
		})
	}

	wg.Wait()
	return firstErr
}
