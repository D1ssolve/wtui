package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/diss0x/wtui/internal/logutil"
	"github.com/diss0x/wtui/internal/task"
)

func newInitCmd() *cobra.Command {
	var (
		branchPrefix string
		baseBranch   string
	)

	cmd := &cobra.Command{
		Use:   "init TASK_ID SERVICE [SERVICE...]",
		Short: "Create a new task group with worktrees",
		Long: `Create a new task directory and set up git worktrees for each SERVICE.

TASK_ID  unique task identifier (e.g., IN-6748)
SERVICE  one or more service/repo names to create worktrees for`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := args[0]
			services := args[1:]

			prefix := branchPrefix
			if prefix == "" && cfg != nil {
				prefix = cfg.BranchPrefix
			}

			params := task.InitParams{
				TaskID:       taskID,
				Services:     services,
				BranchPrefix: prefix,
				BaseBranch:   baseBranch,
			}

			if err := mgr.Init(logutil.WithTaskID(cmd.Context(), taskID), params); err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Task %s initialized with %d service(s)\n", taskID, len(services))
			return nil
		},
	}

	cmd.Flags().StringVar(&branchPrefix, "branch-prefix", "", "branch prefix (overrides config branch_prefix)")
	cmd.Flags().StringVar(&baseBranch, "base", "", "base branch to create feature branch from")

	return cmd
}
