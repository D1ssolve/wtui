package app

import (
	"log/slog"

	"github.com/diss0x/wtui/internal/config"
	"github.com/diss0x/wtui/internal/discovery"
	"github.com/diss0x/wtui/internal/dotnet"
	"github.com/diss0x/wtui/internal/git"
	"github.com/diss0x/wtui/internal/sln"
	"github.com/diss0x/wtui/internal/task"
)

// BuildManager wires the full dependency graph and returns a ready-to-use
// task.Manager. cfg must already have Effective() applied before calling.
//
// This is the single source of truth for DI wiring — both TUI (main.go) and
// CLI (root.go) call this instead of duplicating the 5-line wiring block.
func BuildManager(cfg *config.Config, logger *slog.Logger) task.Manager {
	gitClient := git.NewCommandClient(logger)
	disc := discovery.New(cfg, gitClient, logger)
	dotnetClient := dotnet.NewCommandClient(logger)
	slnMgr := sln.NewManager(dotnetClient, logger)
	return task.New(cfg, gitClient, disc, slnMgr, logger)
}
