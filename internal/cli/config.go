package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/diss0x/wtui/internal/config"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "View or modify configuration",
		Long:  `View the effective configuration or update a single key in the config file.`,
	}
	cmd.AddCommand(newConfigListCmd(), newConfigSetCmd())
	return cmd
}

// newConfigListCmd returns the "wtui config list" subcommand.
// It marshals the effective config to YAML and prints it to stdout.
// The command relies on PersistentPreRunE from the root command to populate cfg.
func newConfigListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Print the effective configuration as YAML",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := yaml.Marshal(cfg)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), string(out))
			return err
		},
	}
}

// newConfigSetCmd returns the "wtui config set <key> <value>" subcommand.
// It writes a single key=value to the resolved config file atomically.
func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration key in the config file",
		Long: `Set a single configuration key in the config file.

Supported keys:
  root_dir            Root directory containing your git repositories
  tasks_root          Directory where task worktree groups are created
  branch_prefix       Prefix applied to new git branches
  editor              Command used to open .code-workspace files
  discovery_depth     Maximum depth when scanning for git repos (min 2)
  output_panel_lines  Number of visible lines in the TUI output panel (3-20)
  log_level           Logging verbosity: DEBUG, INFO, WARN, ERROR`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			value := args[1]

			path := cfgFile
			if path == "" {
				path = resolveDefaultConfigPath()
			}

			if err := config.SetKey(path, key, value); err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Config updated: %s = %s\n", key, value)
			return nil
		},
	}
}

// resolveDefaultConfigPath returns the preferred writable config file path.
// Mirrors the XDG candidate priority used in Load() but always returns a path
// (creating a new one at the default location when no file exists).
func resolveDefaultConfigPath() string {
	if xdgHome := os.Getenv("XDG_CONFIG_HOME"); xdgHome != "" {
		return xdgHome + "/wtui/config.yaml"
	}
	home, _ := os.UserHomeDir()
	return home + "/.config/wtui/config.yaml"
}
