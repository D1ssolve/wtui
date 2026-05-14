package dotnet

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"time"
)

const isAvailableTimeout = 10 * time.Second

const subprocessTimeout = 30 * time.Second

type Client interface {
	IsAvailable(ctx context.Context) bool

	NewSln(ctx context.Context, workDir, name string) error

	SlnAdd(ctx context.Context, workDir, slnPath, projPath string) error
}

type CommandClient struct {
	logger *slog.Logger
}

func NewCommandClient(logger *slog.Logger) *CommandClient {
	return &CommandClient{logger: logger}
}

func (c *CommandClient) IsAvailable(ctx context.Context) bool {
	if _, err := exec.LookPath("dotnet"); err != nil {
		c.logger.DebugContext(ctx, "dotnet not found in PATH", slog.String("reason", err.Error()))
		return false
	}

	ctx, cancel := context.WithTimeout(ctx, isAvailableTimeout)
	defer cancel()

	argv := []string{"dotnet", "--version"}
	c.logger.InfoContext(ctx, "exec dotnet", slog.Any("argv", argv))

	cmd := exec.CommandContext(ctx, "dotnet", "--version")
	cmd.Stdout = nil
	cmd.Stderr = nil

	return cmd.Run() == nil
}

func (c *CommandClient) NewSln(ctx context.Context, workDir, name string) error {
	args := []string{"new", "sln", "-n", name}
	if err := c.run(ctx, workDir, args...); err != nil {
		return fmt.Errorf("dotnet new sln: %w", err)
	}
	return nil
}

func (c *CommandClient) SlnAdd(ctx context.Context, workDir, slnPath, projPath string) error {
	args := []string{"sln", slnPath, "add", projPath}
	if err := c.run(ctx, workDir, args...); err != nil {
		return fmt.Errorf("dotnet sln add: %w", err)
	}
	return nil
}

func (c *CommandClient) run(ctx context.Context, workDir string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, subprocessTimeout)
	defer cancel()

	argv := append([]string{"dotnet"}, args...)
	c.logger.InfoContext(ctx, "exec dotnet", slog.Any("argv", argv))

	cmd := exec.CommandContext(ctx, "dotnet", args...)
	cmd.Dir = workDir

	cmd.Stdout = nil

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		exitCode := 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		return &ExecError{
			Argv:     argv,
			ExitCode: exitCode,
			Stderr:   stderr.String(),
		}
	}

	return nil
}
