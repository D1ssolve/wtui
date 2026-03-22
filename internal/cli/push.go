package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/diss0x/wtui/internal/logutil"
)

func newPushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "push TASK_ID",
		Short: "Push all service worktrees of a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := args[0]
			ctx, cancel := context.WithTimeout(logutil.WithTaskID(cmd.Context(), taskID), 5*time.Minute)
			defer cancel()

			ch := make(chan string, 32)
			go func() {
				for line := range ch {
					fmt.Fprintln(cmd.OutOrStdout(), line)
				}
			}()

			err := mgr.PushTask(ctx, taskID, ch)
			if err != nil {
				return fmt.Errorf("push task %s: %w", taskID, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Task %s pushed.\n", taskID)
			return nil
		},
	}
}
