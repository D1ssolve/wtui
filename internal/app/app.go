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

func BuildManager(cfg *config.Config, logger *slog.Logger) task.Manager {
	gitClient := git.NewCommandClient(logger)
	disc := discovery.NewCached(discovery.New(cfg, gitClient, logger))
	dotnetClient := dotnet.NewCommandClient(logger)
	slnMgr := sln.NewManager(dotnetClient, logger)
	return task.New(cfg, gitClient, disc, slnMgr, logger)
}
