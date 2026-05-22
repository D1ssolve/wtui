package main

import (
	"fmt"
	"log/slog"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/app"
	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/logutil"
	"github.com/D1ssolve/wtui/internal/tui"
)

var Version = "dev"

func main() {
	if versionRequested(os.Args[1:]) {
		fmt.Print(versionOutput(Version))
		return
	}

	if err := runTUI(); err != nil {
		slog.Error("TUI failed", "err", err)
		os.Exit(1)
	}
}

func versionRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--version" || arg == "-v" {
			return true
		}
	}

	return false
}

func versionOutput(version string) string {
	return fmt.Sprintf("wtui %s\n", version)
}

func runTUI() error {
	cfg, err := config.Load("")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg = cfg.Effective()

	logger, logErr := logutil.InitLogger("wtui", logutil.ParseLogLevel(cfg.LogLevel))
	if logErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not open log file: %v\n", logErr)
	}

	deps := app.BuildDependencies(cfg, logger)

	model, err := tui.NewWithOptions(cfg, deps.Manager, logger, tui.Options{
		LazygitAvailable: deps.Features.LazygitAvailable,
	})
	if err != nil {
		return fmt.Errorf("create TUI model: %w", err)
	}

	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}
