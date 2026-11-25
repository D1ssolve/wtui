package shell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// HookOutput isolates a workflow hook from the caller's process tree.
func HookOutput(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	stop := isolateHook(cmd)
	var interrupted bool
	var stopErr error
	cmd.Cancel = func() error {
		err := stop()
		interrupted = err == nil
		if err != nil && err != os.ErrProcessDone {
			stopErr = err
		}
		// Let a naturally successful exit win the race with cancellation.
		return os.ErrProcessDone
	}
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.CombinedOutput()
	if stopErr != nil {
		return out, &HookStopError{Cause: stopErr}
	}
	// Wait synchronizes with Cancel. Only attribute an unsuccessful run to
	// interruption if shutdown actually ran, or Start refused the context.
	if err != nil && interrupted {
		return out, fmt.Errorf("%w: %v", ctx.Err(), err)
	}
	return out, err
}

// HookStopError means the process tree could not be confirmed stopped.
type HookStopError struct{ Cause error }

func (e *HookStopError) Error() string { return "could not stop hook process tree: " + e.Cause.Error() }
func (e *HookStopError) Unwrap() error { return e.Cause }
