package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newRemoveCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "remove TASK_ID",
		Short: "Remove a task and all its worktrees",
		Long: `Remove a task group and all associated git worktrees.

Without --force the command exits with an error if any worktree has
uncommitted changes. Use --force to remove dirty worktrees regardless.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := args[0]

			if err := mgr.Remove(cmd.Context(), taskID, force); err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Task %s removed\n", taskID)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "force removal of dirty worktrees")

	return cmd
}
