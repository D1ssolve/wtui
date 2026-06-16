package app

import (
	"context"
	"log/slog"
	"os/exec"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/discovery"
	"github.com/D1ssolve/wtui/internal/dotnet"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/sln"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/validation"
)

type FeatureFlags struct {
	LazygitAvailable bool
	GlabAvailable    bool
	GhAvailable      bool
}

type Dependencies struct {
	Manager       task.Manager
	Features      FeatureFlags
	ForgeClients  map[forge.ForgeProvider]forge.ForgeClient
	ResolvedFlow  *gitflow.ResolvedGitFlow
	GlabAvailable bool
	GhAvailable   bool
}

type lookPathFunc func(string) (string, error)

func BuildDependencies(cfg *config.Config, logger *slog.Logger) Dependencies {
	resolvedFlow, err := gitflow.EffectiveConfig(cfg.GitFlow)
	if err != nil {
		logger.Warn("invalid git flow config, using defaults", "err", err)
		resolvedFlow, _ = gitflow.EffectiveConfig(nil)
	}

	forgeCtx := context.Background()
	glabAvailable := forge.IsGlabAvailable(forgeCtx)
	ghAvailable := forge.IsGhAvailable(forgeCtx)
	forgeClients := buildForgeClients(cfg, glabAvailable, ghAvailable)
	features := detectFeatures(exec.LookPath)
	features.GlabAvailable = glabAvailable
	features.GhAvailable = ghAvailable

	return Dependencies{
		Manager:      buildManager(cfg, logger, resolvedFlow, forgeClients),
		Features:     features,
		ForgeClients: forgeClients,
		ResolvedFlow: resolvedFlow,
		GlabAvailable: glabAvailable,
		GhAvailable:   ghAvailable,
	}
}

func buildManager(
	cfg *config.Config,
	logger *slog.Logger,
	resolvedFlow *gitflow.ResolvedGitFlow,
	forgeClients map[forge.ForgeProvider]forge.ForgeClient,
) task.Manager {
	gitClient := git.NewCommandClient(logger)
	disc := discovery.NewCached(discovery.New(cfg, gitClient, logger))
	dotnetClient := dotnet.NewCommandClient(logger)
	slnMgr := sln.NewManager(dotnetClient, logger)
	validator := validation.NewTaskValidator(gitClient)

	return task.New(cfg, gitClient, disc, slnMgr, validator, resolvedFlow, forgeClients, logger)
}

func buildForgeClients(cfg *config.Config, glabAvailable bool, ghAvailable bool) map[forge.ForgeProvider]forge.ForgeClient {
	forgeClients := make(map[forge.ForgeProvider]forge.ForgeClient)
	if glabAvailable {
		forgeClients[forge.ForgeProviderGitLab] = forge.NewGlabClient(cfg.RootDir)
	}
	if ghAvailable {
		forgeClients[forge.ForgeProviderGitHub] = forge.NewGhClient(cfg.RootDir)
	}
	return forgeClients
}

func detectFeatures(lookPath lookPathFunc) FeatureFlags {
	_, err := lookPath("lazygit")
	return FeatureFlags{LazygitAvailable: err == nil}
}
