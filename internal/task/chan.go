package task

import "context"

func sendLine(ctx context.Context, ch chan<- string, msg string) bool {
	if ch == nil {
		return true
	}
	select {
	case ch <- msg:
		return true
	case <-ctx.Done():
		return false
	}
}

// nilSafeLineCh returns ch, or a drained sink when ch is nil, so manager
// methods safely accept a nil status channel without blocking producers.
func nilSafeLineCh(ch chan<- string) (chan<- string, func()) {
	if ch != nil {
		return ch, func() {}
	}
	sink := make(chan string)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-sink:
			case <-done:
				return
			}
		}
	}()
	return sink, func() { close(done) }
}
