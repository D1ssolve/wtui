package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/diss0x/wtui/internal/cli"
	"github.com/diss0x/wtui/internal/config"
	"github.com/diss0x/wtui/internal/discovery"
	"github.com/diss0x/wtui/internal/dotnet"
	"github.com/diss0x/wtui/internal/git"
	"github.com/diss0x/wtui/internal/sln"
	"github.com/diss0x/wtui/internal/task"
	"github.com/diss0x/wtui/internal/tui"
)

// Version is set at build time via -ldflags "-X main.Version=<tag>".
var Version = "dev"

func main() {
	// If any positional (non-flag) argument is present, treat it as a CLI
	// subcommand and delegate entirely to cobra.
	for _, arg := range os.Args[1:] {
		if len(arg) > 0 && arg[0] != '-' {
			cli.Execute(Version)
			return
		}
	}

	// No subcommand — launch the interactive TUI.
	if err := runTUI(); err != nil {
		slog.Error("TUI failed", "err", err)
		os.Exit(1)
	}
}

// runTUI loads configuration, wires all dependencies, and starts the bubbletea
// program in alt-screen mode.
func runTUI() error {
	cfg, err := config.Load("")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg.Effective()

	logger, err := initTUILogger(cfg)
	if err != nil {
		// Non-fatal: fall back to a discard logger so TUI still launches.
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelWarn,
		}))
		fmt.Fprintf(os.Stderr, "Warning: could not open log file: %v\n", err)
	}

	// Wire the dependency graph (mirrors cli/root.go setupDependencies).
	gitClient := git.NewCommandClient(logger)
	disc := discovery.New(cfg, gitClient, logger)
	dotnetClient := dotnet.NewCommandClient(logger)
	slnMgr := sln.NewManager(dotnetClient, logger)
	mgr := task.New(cfg, gitClient, disc, slnMgr, logger)

	model, err := tui.New(cfg, mgr, logger)
	if err != nil {
		return fmt.Errorf("create TUI model: %w", err)
	}

	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

// initTUILogger opens (or creates) the wtui log file under the XDG state
// directory and returns an slog.Logger writing JSON to that file.
//
// In TUI mode all log output must go to the file — nothing may be written to
// stdout/stderr because bubbletea owns those file descriptors for rendering.
func initTUILogger(c *config.Config) (*slog.Logger, error) {
	logDir := xdgStateDir("wtui")
	if err := os.MkdirAll(logDir, 0o750); err != nil {
		return nil, fmt.Errorf("create log directory %s: %w", logDir, err)
	}

	logPath := filepath.Join(logDir, "wtui.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, fmt.Errorf("open log file %s: %w", logPath, err)
	}

	level := parseLogLevel(c.LogLevel)
	handler := slog.NewJSONHandler(f, &slog.HandlerOptions{Level: level})
	return slog.New(handler), nil
}

// xdgStateDir returns the XDG state directory for the given application name.
// Falls back to $HOME/.local/state/<app> when XDG_STATE_HOME is unset.
func xdgStateDir(app string) string {
	if base := os.Getenv("XDG_STATE_HOME"); base != "" {
		return filepath.Join(base, app)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", app)
}

// parseLogLevel converts a string log level name to the corresponding slog.Level.
// Unrecognised strings default to INFO.
func parseLogLevel(level string) slog.Level {
	switch level {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
