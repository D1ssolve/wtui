package cli

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/diss0x/wtui/internal/task"
)

func newOpenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "open TASK_ID",
		Short: "Open a task file in the configured editor",
		Long: `Interactively select a file and application to open for the given task.

If exactly one file or application is found, it is selected automatically.
Otherwise, a numbered prompt is presented on stdout and the selection is
read from stdin. The editor process is launched non-blocking (the command
returns immediately after the process is started).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := args[0]
			out := cmd.OutOrStdout()
			in := bufio.NewReader(cmd.InOrStdin())

			candidates, err := mgr.ListOpenCandidates(cmd.Context(), taskID)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Error: %v\n", err)
				return fmt.Errorf("list open candidates: %w", err)
			}

			// ── File selection ────────────────────────────────────────────────
			if len(candidates.Files) == 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "No openable files found for task %s\n", taskID)
				return ErrNoFiles
			}

			var selectedFile task.OpenableFile

			if len(candidates.Files) == 1 {
				selectedFile = candidates.Files[0]
				fmt.Fprintf(out, "Using file: %s\n", selectedFile.Name)
			} else {
				fmt.Fprintln(out, "Select file to open:")
				for i, f := range candidates.Files {
					fmt.Fprintf(out, "  %d. %s\n", i+1, f.Name)
				}
				fmt.Fprint(out, "Enter number: ")

				line, err := in.ReadString('\n')
				if err != nil {
					return fmt.Errorf("read file selection: %w", err)
				}
				n, err := strconv.Atoi(strings.TrimSpace(line))
				if err != nil || n < 1 || n > len(candidates.Files) {
					return fmt.Errorf("invalid file selection %q: must be a number between 1 and %d", strings.TrimSpace(line), len(candidates.Files))
				}
				selectedFile = candidates.Files[n-1]
			}

			// ── App selection ─────────────────────────────────────────────────
			var selectedApp task.AppEntry

			if len(candidates.Apps) == 1 {
				selectedApp = candidates.Apps[0]
				fmt.Fprintf(out, "Using app: %s\n", selectedApp.Name)
			} else {
				fmt.Fprintln(out, "Select app to use:")
				for i, a := range candidates.Apps {
					fmt.Fprintf(out, "  %d. %s\n", i+1, a.Name)
				}
				fmt.Fprint(out, "Enter number: ")

				line, err := in.ReadString('\n')
				if err != nil {
					return fmt.Errorf("read app selection: %w", err)
				}
				n, err := strconv.Atoi(strings.TrimSpace(line))
				if err != nil || n < 1 || n > len(candidates.Apps) {
					return fmt.Errorf("invalid app selection %q: must be a number between 1 and %d", strings.TrimSpace(line), len(candidates.Apps))
				}
				selectedApp = candidates.Apps[n-1]
			}

			// ── Launch ────────────────────────────────────────────────────────
			if err := mgr.OpenFile(cmd.Context(), selectedFile.Path, selectedApp.Binary); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Error: %v\n", err)
				return fmt.Errorf("open file: %w", err)
			}

			fmt.Fprintf(out, "Opened %s with %s\n", selectedFile.Name, selectedApp.Name)
			return nil
		},
	}

	return cmd
}
