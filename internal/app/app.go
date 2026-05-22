package app

import (
	"log/slog"
	"os/exec"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/discovery"
	"github.com/D1ssolve/wtui/internal/dotnet"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/sln"
	"github.com/D1ssolve/wtui/internal/task"
)

type FeatureFlags struct {
	LazygitAvailable bool
}

type Dependencies struct {
	Manager  task.Manager
	Features FeatureFlags
}

type lookPathFunc func(string) (string, error)

func BuildDependencies(cfg *config.Config, logger *slog.Logger) Dependencies {
	return Dependencies{
		Manager:  buildManager(cfg, logger),
		Features: detectFeatures(exec.LookPath),
	}
}

func buildManager(cfg *config.Config, logger *slog.Logger) task.Manager {
	gitClient := git.NewCommandClient(logger)
	disc := discovery.NewCached(discovery.New(cfg, gitClient, logger))
	dotnetClient := dotnet.NewCommandClient(logger)
	slnMgr := sln.NewManager(dotnetClient, logger)
	return task.New(cfg, gitClient, disc, slnMgr, logger)
}

func detectFeatures(lookPath lookPathFunc) FeatureFlags {
	_, err := lookPath("lazygit")
	return FeatureFlags{LazygitAvailable: err == nil}
}
