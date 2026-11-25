package shell

import (
	"context"
	"os/exec"
)

func Command(ctx context.Context, script string) *exec.Cmd {
	return command(ctx, script)
}

func cmdLine(script string) string {
	return `cmd /S /C "` + script + `"`
}
