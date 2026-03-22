package main

import (
	"fmt"
	"log/slog"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/diss0x/wtui/internal/app"
	"github.com/diss0x/wtui/internal/cli"
	"github.com/diss0x/wtui/internal/config"
	"github.com/diss0x/wtui/internal/logutil"
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
	cfg = cfg.Effective()

	// In TUI mode all log output must go to the file — nothing may be written to
	// stdout/stderr because bubbletea owns those file descriptors for rendering.
	logger, logErr := logutil.InitLogger("wtui", logutil.ParseLogLevel(cfg.LogLevel))
	if logErr != nil {
		// Non-fatal: InitLogger already returns a fallback stderr logger.
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
