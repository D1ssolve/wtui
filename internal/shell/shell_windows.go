//go:build windows

package shell

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"

	"golang.org/x/sys/windows"
)

func command(ctx context.Context, script string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: cmdLine(script)}
	return cmd
}

func isolateHook(cmd *exec.Cmd) func() error {
	return func() error {
		dir, err := windows.GetSystemDirectory()
		if err == nil {
			err = exec.Command(filepath.Join(dir, "taskkill.exe"), "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
		}
		if err != nil {
			return errors.Join(err, cmd.Process.Kill())
		}
		return nil
	}
}
