package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/diss0x/wtui/internal/logutil"
)

func newRemoveCmd() *cobra.Command {
	var force bool
	var deleteBranches bool

	cmd := &cobra.Command{
		Use:   "remove TASK_ID",
		Short: "Remove a task and all its worktrees",
		Long: `Remove a task group and all associated git worktrees.

Without --force the command exits with an error if any worktree has
uncommitted changes. Use --force to remove dirty worktrees regardless.
Use --delete-branches to also delete the git branch for each worktree.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := args[0]

			if err := mgr.Remove(logutil.WithTaskID(cmd.Context(), taskID), taskID, force, deleteBranches); err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Task %s removed\n", taskID)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "force removal of dirty worktrees")
	cmd.Flags().BoolVar(&deleteBranches, "delete-branches", false, "delete the git branch for each worktree")

	return cmd
}
