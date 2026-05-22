package main

import (
	"fmt"
	"log/slog"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/diss0x/wtui/internal/app"
	"github.com/diss0x/wtui/internal/config"
	"github.com/diss0x/wtui/internal/logutil"
	"github.com/diss0x/wtui/internal/tui"
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

	mgr := app.BuildManager(cfg, logger)

	model, err := tui.New(cfg, mgr, logger)
	if err != nil {
		return fmt.Errorf("create TUI model: %w", err)
	}

	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
