package cli

import (
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list [TASK_ID]",
		Short: "List tasks or services within a task",
		Long: `List all task groups or, when TASK_ID is given, the services in that task.

Without TASK_ID: prints each task ID on its own line, sorted alphabetically.
With TASK_ID:    prints each service name on its own line, sorted alphabetically.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return runListTasks(cmd)
			}
			return runListServices(cmd, args[0])
		},
	}

	return cmd
}

func runListTasks(cmd *cobra.Command) error {
	tasks, err := mgr.List(cmd.Context())
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return err
	}

	if len(tasks) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No tasks.")
		return nil
	}

	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	sort.Strings(ids)

	for _, id := range ids {
		fmt.Fprintln(cmd.OutOrStdout(), id)
	}
	return nil
}

func runListServices(cmd *cobra.Command, taskID string) error {
	services, err := mgr.ListServices(cmd.Context(), taskID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return err
	}

	if len(services) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No services.")
		return nil
	}

	names := make([]string, len(services))
	for i, s := range services {
		names[i] = s.Name
	}
	sort.Strings(names)

	for _, name := range names {
		fmt.Fprintln(cmd.OutOrStdout(), name)
	}
	return nil
}
