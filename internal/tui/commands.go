package tui

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/diss0x/wtui/internal/domain"
	"github.com/diss0x/wtui/internal/task"
)

// ── Message types ─────────────────────────────────────────────────────────────

// TasksLoadedMsg is dispatched when the background loadTasksCmd has completed.
type TasksLoadedMsg struct{ Tasks []domain.Task }

// ReposLoadedMsg is dispatched when the background loadReposCmd has completed.
type ReposLoadedMsg struct{ Repos []domain.Repo }

// ServicesLoadedMsg is dispatched when the background loadServicesCmd has completed.
type ServicesLoadedMsg struct {
	TaskID   string
	Services []domain.Service
}

// OutputLineMsg carries a single line of real-time subprocess output to the
// output panel. The Update handler appends the line and schedules the next read.
type OutputLineMsg struct{ Line string }

// CommandDoneMsg is dispatched when a background operation has finished.
// Err is nil on success, non-nil on failure.
type CommandDoneMsg struct{ Err error }

// DirtyServicesLoadedMsg carries dirty-service information needed to populate
// the RemoveDialog before it is displayed.
type DirtyServicesLoadedMsg struct {
	ServiceCount  int
	DirtyServices []string
}

// OpenCandidatesLoadedMsg is dispatched when loadOpenCandidatesCmd completes.
type OpenCandidatesLoadedMsg struct {
	TaskID     string
	Candidates task.OpenCandidates
}

// ── Command factories ─────────────────────────────────────────────────────────

// loadTasksCmd returns a tea.Cmd that lists all tasks from disk and returns
// a TasksLoadedMsg (or CommandDoneMsg on error).
func loadTasksCmd(mgr task.Manager) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		tasks, err := mgr.List(ctx)
		if err != nil {
			return CommandDoneMsg{Err: err}
		}
		return TasksLoadedMsg{Tasks: tasks}
	}
}

// loadServicesCmd returns a tea.Cmd that loads the services for taskID and
// returns a ServicesLoadedMsg (or CommandDoneMsg on error).
func loadServicesCmd(mgr task.Manager, taskID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		services, err := mgr.ListServices(ctx, taskID)
		if err != nil {
			return CommandDoneMsg{Err: err}
		}
		return ServicesLoadedMsg{TaskID: taskID, Services: services}
	}
}

// loadReposCmd returns a tea.Cmd that discovers all repos under ROOT_DIR and
// returns a ReposLoadedMsg. Discovery errors are silently swallowed — the
// panel will show "No repos found." when the result is empty.
func loadReposCmd(mgr task.Manager) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		repos, err := mgr.DiscoverRepos(ctx)
		if err != nil {
			// Non-fatal: return empty slice so the panel shows "No repos found."
			return ReposLoadedMsg{Repos: nil}
		}
		return ReposLoadedMsg{Repos: repos}
	}
}

// loadDirtyServicesCmd returns a tea.Cmd that loads the services for taskID and
// inspects each for uncommitted changes. The result is used to pre-populate the
// RemoveDialog with accurate dirty-service warnings before it is displayed.
func loadDirtyServicesCmd(mgr task.Manager, taskID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		services, err := mgr.ListServices(ctx, taskID)
		if err != nil {
			// Return empty info — RemoveDialog will still be shown without warnings.
			return DirtyServicesLoadedMsg{}
		}
		var dirtyNames []string
		for _, s := range services {
			if s.IsDirty {
				dirtyNames = append(dirtyNames, s.Name)
			}
		}
		return DirtyServicesLoadedMsg{
			ServiceCount:  len(services),
			DirtyServices: dirtyNames,
		}
	}
}

// initTaskCmd returns a tea.Cmd that calls mgr.Init with the given params and
// returns a CommandDoneMsg when complete.
func initTaskCmd(mgr task.Manager, params task.InitParams) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		err := mgr.Init(ctx, params)
		return CommandDoneMsg{Err: err}
	}
}

// addServiceCmd returns a tea.Cmd that calls mgr.Add with the given params and
// returns a CommandDoneMsg when complete.
func addServiceCmd(mgr task.Manager, params task.AddParams) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		err := mgr.Add(ctx, params)
		return CommandDoneMsg{Err: err}
	}
}

// removeTaskCmd returns a tea.Cmd that calls mgr.Remove and returns a
// CommandDoneMsg when complete.
func removeTaskCmd(mgr task.Manager, taskID string, force bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		err := mgr.Remove(ctx, taskID, force)
		return CommandDoneMsg{Err: err}
	}
}

// generateSlnCmd returns a tea.Cmd that calls mgr.GenerateSln and returns a
// CommandDoneMsg when complete.
func generateSlnCmd(mgr task.Manager, taskID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		err := mgr.GenerateSln(ctx, taskID)
		return CommandDoneMsg{Err: err}
	}
}

// openWorkspaceCmd returns a tea.Cmd that calls mgr.OpenWorkspace (non-blocking)
// and returns a CommandDoneMsg when complete.
func openWorkspaceCmd(mgr task.Manager, taskID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := mgr.OpenWorkspace(ctx, taskID)
		return CommandDoneMsg{Err: err}
	}
}

// loadOpenCandidatesCmd calls mgr.ListOpenCandidates and returns the result.
func loadOpenCandidatesCmd(mgr task.Manager, taskID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		candidates, err := mgr.ListOpenCandidates(ctx, taskID)
		if err != nil {
			return CommandDoneMsg{Err: err}
		}
		return OpenCandidatesLoadedMsg{TaskID: taskID, Candidates: candidates}
	}
}

// openFileCmd calls mgr.OpenFile and returns a CommandDoneMsg.
func openFileCmd(mgr task.Manager, path, app string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := mgr.OpenFile(ctx, path, app)
		return CommandDoneMsg{Err: err}
	}
}

// readNextLine reads one line from ch and returns it as an OutputLineMsg.
// When the channel is closed it returns a CommandDoneMsg{} to signal completion.
// This enables the streaming-output pattern: each OutputLineMsg handler
// schedules the next readNextLine, creating a chain until the channel closes.
func readNextLine(ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return CommandDoneMsg{}
		}
		return OutputLineMsg{Line: line}
	}
}

// execShellCmd suspends the TUI and runs cmd in dir via sh -c.
// After the process exits, bubbletea automatically restores the TUI.
func execShellCmd(cmd, dir string) tea.Cmd {
	c := exec.Command("sh", "-c", cmd)
	c.Dir = dir
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return CommandDoneMsg{Err: fmt.Errorf("shell: %w", err)}
		}
		return CommandDoneMsg{}
	})
}
