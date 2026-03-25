package cli

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/diss0x/wtui/internal/app"
	"github.com/diss0x/wtui/internal/config"
	"github.com/diss0x/wtui/internal/logutil"
	"github.com/diss0x/wtui/internal/task"
)

// Package-level state shared across subcommands.
//
// Design note: using package-level vars is the standard cobra pattern for CLI
// applications; it avoids threading context through cobra.Command hooks.
//
// Ordering constraint: setupDependencies (PersistentPreRunE) must run before
// any subcommand's RunE. All subcommands that need mgr/cfg/logger must read the
// package-level var at RunE time, NOT capture it at command-construction time.
//
// Known limitation: package-level state makes concurrent testing of CLI commands
// impossible. A future refactor to a CLIApp struct would resolve this.
var (
	cfgFile    string
	rootDir    string
	tasksRoot  string
	initConfig bool

	// CLI flag overrides for config fields (zero value = not set, use config/env value).
	editor         string
	branchPrefix   string
	baseBranch     string
	discoveryDepth int
	outputLines    int

	cfg    *config.Config
	logger *slog.Logger
	mgr    task.Manager
)

func Execute(version string) {
	rootCmd := buildRootCmd(version)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(exitCode(err))
	}
}

func buildRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:   "wtui",
		Short: "Manage git worktree groups across microservice monorepos",
		Long: `wtui is a CLI+TUI tool for managing git worktree groups (tasks) across
microservice monorepos.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return setupDependencies()
		},
	}

	root.PersistentFlags().StringVar(&cfgFile, "config", "", "path to config file")
	root.PersistentFlags().StringVar(&rootDir, "root", "", "override config root_dir")
	root.PersistentFlags().StringVar(&tasksRoot, "tasks-root", "", "override config tasks_root")
	root.PersistentFlags().BoolVar(&initConfig, "init-config", false, "write default config.yaml and exit")
	root.PersistentFlags().StringVar(&editor, "editor", "", "override config editor")
	root.PersistentFlags().StringVar(&branchPrefix, "branch-prefix", "", "override config branch_prefix")
	root.PersistentFlags().StringVar(&baseBranch, "base-branch", "", "override config base_branch")
	root.PersistentFlags().IntVar(&discoveryDepth, "discovery-depth", 0, "override config discovery_depth (min 2)")
	root.PersistentFlags().IntVar(&outputLines, "output-lines", 0, "override config output_panel_lines (range [3, 20])")

	root.AddCommand(
		newInitCmd(),
		newAddCmd(),
		newListCmd(),
		newCloneCmd(),
		newRemoveCmd(),
		newSlnCmd(),
		newVersionCmd(version),
		newConfigCmd(),
		newPushCmd(),
	)

	return root
}

func setupDependencies() error {
	if initConfig {
		path := os.ExpandEnv("$HOME/.config/wtui/config.yaml")
		defaultCfg := &config.Config{}
		if err := defaultCfg.WriteDefault(path); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing default config: %v\n", err)
			os.Exit(3)
		}
		fmt.Printf("Config written to %s\n", path)
		os.Exit(0)
	}

	var loadErr error
	cfg, loadErr = config.Load(cfgFile)
	if loadErr != nil {
		return fmt.Errorf("load config: %w", loadErr)
	}
	cfg = cfg.Effective()

	if rootDir != "" {
		cfg.RootDir = rootDir
		if tasksRoot == "" {
			cfg.TasksRoot = filepath.Join(cfg.RootDir, ".tasks")
		}
	}
	if tasksRoot != "" {
		cfg.TasksRoot = tasksRoot
	}

	// Apply CLI flag overrides (non-zero values win over YAML/env).
	if editor != "" {
		cfg.Editor = editor
	}
	if branchPrefix != "" {
		cfg.BranchPrefix = branchPrefix
	}
	if baseBranch != "" {
		cfg.BaseBranch = baseBranch
	}
	if discoveryDepth > 0 {
		if discoveryDepth < 2 {
			discoveryDepth = 2
		}
		cfg.DiscoveryDepth = discoveryDepth
	}
	if outputLines > 0 {
		if outputLines < 3 {
			outputLines = 3
		}
		if outputLines > 20 {
			outputLines = 20
		}
		cfg.OutputPanelLines = outputLines
	}

	var logErr error
	logger, logErr = logutil.InitLogger("wtui", logutil.ParseLogLevel(cfg.LogLevel))
	if logErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not open log file: %v\n", logErr)
	}

	mgr = app.BuildManager(cfg, logger)

	return nil
}
