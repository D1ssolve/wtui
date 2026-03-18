package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/diss0x/wtui/internal/task"
)

func newAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add TASK_ID SERVICE [SERVICE...]",
		Short: "Add services to an existing task",
		Long: `Add one or more SERVICE worktrees to an existing task group.

TASK_ID  the existing task identifier (e.g., IN-6748)
SERVICE  one or more service/repo names to add as worktrees`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := args[0]
			services := args[1:]

			params := task.AddParams{
				TaskID:   taskID,
				Services: services,
			}

			if err := mgr.Add(cmd.Context(), params); err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Added %d service(s) to task %s\n", len(services), taskID)
			return nil
		},
	}

	return cmd
}
