package shell

import (
	"context"
	"os/exec"
	"runtime"
)

func argv(goos, script string) []string {
	if goos == "windows" {
		return []string{"cmd", "/C", script}
	}
	return []string{"sh", "-c", script}
}

func Command(ctx context.Context, script string) *exec.Cmd {
	args := argv(runtime.GOOS, script)
	return exec.CommandContext(ctx, args[0], args[1:]...)
}
