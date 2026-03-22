package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/diss0x/wtui/internal/logutil"
)

func newCloneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clone SRC_TASK_ID DST_TASK_ID",
		Short: "Clone a task and its service worktrees",
		Long: `Clone an existing task group into a new task ID.

The new task reuses the source task's services and creates new worktrees on a
new branch derived from the configured branch prefix and destination task ID.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			src := args[0]
			dst := args[1]

			if err := mgr.CloneTask(logutil.WithTaskID(cmd.Context(), dst), src, dst); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Error: %v\n", err)
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Task %s cloned from %s.\n", dst, src)
			return nil
		},
	}

	return cmd
}
