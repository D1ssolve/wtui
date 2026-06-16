package main

import (
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/app"
	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/logutil"
	"github.com/D1ssolve/wtui/internal/tui"
)

// Version is set via -ldflags at release build time.
// When installed via go install, resolveVersion falls back to module info.
var Version = ""

// resolveVersion returns the ldflag-injected version if set,
// otherwise falls back to the module version from runtime/debug.ReadBuildInfo
// (available when installed via go install).
func resolveVersion() string {
	if Version != "" {
		return Version
	}

	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}

	if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}

	return "dev"
}

func main() {
	if versionRequested(os.Args[1:]) {
		fmt.Print(versionOutput(resolveVersion()))
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
	cfg, err = cfg.Effective()
	if err != nil {
		return fmt.Errorf("normalize config: %w", err)
	}

	logger, logErr := logutil.InitLogger("wtui", logutil.ParseLogLevel(cfg.LogLevel))
	if logErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not open log file: %v\n", logErr)
	}

	deps := app.BuildDependencies(cfg, logger)

	model, err := tui.NewWithOptions(cfg, deps.Manager, logger, tui.Options{
		LazygitAvailable: deps.Features.LazygitAvailable,
		GlabAvailable:    deps.GlabAvailable,
		GhAvailable:      deps.GhAvailable,
		ForgeClients:     deps.ForgeClients,
		ResolvedFlow:     deps.ResolvedFlow,
	})
	if err != nil {
		return fmt.Errorf("create TUI model: %w", err)
	}

	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}
