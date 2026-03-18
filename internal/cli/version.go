package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newVersionCmd returns the cobra command for `wtui version`.
//
// Prints the version string that was injected at build time via:
//
//	go build -ldflags "-X main.Version=$(git describe --tags)"
func newVersionCmd(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:               "version",
		Short:             "Print the version",
		Long:              `Print the wtui version string and exit.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), version)
			return nil
		},
	}

	return cmd
}
