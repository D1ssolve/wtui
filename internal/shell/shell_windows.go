//go:build windows

package shell

import (
	"context"
	"os/exec"
	"syscall"
)

func command(ctx context.Context, script string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: cmdLine(script)}
	return cmd
}
