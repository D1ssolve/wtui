//go:build !windows

package shell

import (
	"context"
	"os/exec"
)

func command(ctx context.Context, script string) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", "-c", script)
}
