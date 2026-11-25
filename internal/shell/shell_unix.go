//go:build !windows

package shell

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func command(ctx context.Context, script string) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", "-c", script)
}

func isolateHook(cmd *exec.Cmd) func() error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
