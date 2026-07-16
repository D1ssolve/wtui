package task

import (
	"context"
	"fmt"
	"sync"

	"golang.org/x/sync/semaphore"
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
	setFirstErr := func(err error) {
		if err == nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if firstErr == nil {
			firstErr = err
		}
	}
	sem := semaphore.NewWeighted(int64(m.concurrency()))

	for _, svc := range services {
		if err := sem.Acquire(ctx, 1); err != nil {
			setFirstErr(err)
			break
		}

		wg.Go(func() {
			defer sem.Release(1)
			if !sendLine(ctx, lineCh, fmt.Sprintf("[%s] pushing...", svc.Name)) {
				setFirstErr(ctx.Err())
				return
			}
			if err := m.ensurePushBranchAllowed(ctx, svc.WorktreePath); err != nil {
				sendLine(ctx, lineCh, fmt.Sprintf("[%s] push error: %v", svc.Name, err))
				setFirstErr(err)
				return
			}
			if err := m.git.Push(ctx, svc.WorktreePath, lineCh); err != nil {
				sendLine(ctx, lineCh, fmt.Sprintf("[%s] push error: %v", svc.Name, err))
				setFirstErr(err)
				return
			}

			sendLine(ctx, lineCh, fmt.Sprintf("[%s] pushed.", svc.Name))
		})
	}

	wg.Wait()
	return firstErr
}
