package task

import "context"

func sendLine(ctx context.Context, ch chan<- string, msg string) bool {
	select {
	case ch <- msg:
		return true
	case <-ctx.Done():
		return false
	}
}
