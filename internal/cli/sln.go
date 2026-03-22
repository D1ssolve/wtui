package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/diss0x/wtui/internal/logutil"
)

func newSlnCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sln TASK_ID",
		Short: "Regenerate the .NET solution file for a task",
		Long: `(Re)generate the <TASK_ID>.sln file for a task group.

Discovers all .csproj files under each service worktree and adds them to the
solution. Requires dotnet CLI to be available in PATH; silently skips if not found.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := args[0]

			if err := mgr.GenerateSln(logutil.WithTaskID(cmd.Context(), taskID), taskID); err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Solution file generated for task %s\n", taskID)
			return nil
		},
	}

	return cmd
}
