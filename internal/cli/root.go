package cli

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/diss0x/wtui/internal/config"
	"github.com/diss0x/wtui/internal/discovery"
	"github.com/diss0x/wtui/internal/dotnet"
	"github.com/diss0x/wtui/internal/git"
	"github.com/diss0x/wtui/internal/sln"
	"github.com/diss0x/wtui/internal/task"
)

// Package-level state shared across subcommands.
// Using package-level vars is the standard cobra pattern for CLI applications
// (avoids threading context through cobra.Command hooks).
var (
	cfgFile    string
	rootDir    string
	tasksRoot  string
	initConfig bool

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

	root.AddCommand(
		newInitCmd(),
		newAddCmd(),
		newListCmd(),
		newRemoveCmd(),
		newSlnCmd(),
		newOpenCmd(),
		newVersionCmd(version),
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
	cfg.Effective()

	if rootDir != "" {
		cfg.RootDir = rootDir
		if tasksRoot == "" {
			cfg.TasksRoot = filepath.Join(cfg.RootDir, ".tasks")
		}
	}
	if tasksRoot != "" {
		cfg.TasksRoot = tasksRoot
	}

	var logErr error
	logger, logErr = initLogger(cfg)
	if logErr != nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelWarn,
		}))
		fmt.Fprintf(os.Stderr, "Warning: could not open log file: %v\n", logErr)
	}

	gitClient := git.NewCommandClient(logger)
	disc := discovery.New(cfg, gitClient, logger)
	dotnetClient := dotnet.NewCommandClient(logger)
	slnMgr := sln.NewManager(dotnetClient, logger)
	mgr = task.New(cfg, gitClient, disc, slnMgr, logger)

	return nil
}

// initLogger opens (or creates) the wtui log file under the XDG state directory
// and returns an slog.Logger writing JSON to that file.
//
// Log file location:
//  1. $XDG_STATE_HOME/wtui/wtui.log
//  2. $HOME/.local/state/wtui/wtui.log
func initLogger(c *config.Config) (*slog.Logger, error) {
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

func xdgStateDir(app string) string {
	if base := os.Getenv("XDG_STATE_HOME"); base != "" {
		return filepath.Join(base, app)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", app)
}

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
