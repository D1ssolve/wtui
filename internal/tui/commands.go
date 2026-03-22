package tui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/diss0x/wtui/internal/domain"
	"github.com/diss0x/wtui/internal/logutil"
	"github.com/diss0x/wtui/internal/task"
)

type TasksLoadedMsg struct{ Tasks []domain.Task }

type ReposLoadedMsg struct {
	Repos []domain.Repo
	Err   error
}

type ServicesLoadedMsg struct {
	TaskID   string
	Services []domain.Service
}

type OutputLineMsg struct {
	Line string
	Next tea.Cmd
}

type CommandDoneMsg struct{ Err error }

// channelDrainedMsg is an internal sentinel returned by readNextCommand and
// readNextLine when their source channel is closed. It is intentionally
// unexported so that only the single authoritative CommandDoneMsg (returned by
// the main operation goroutine) triggers the "Done." output line and post-op
// refresh. Without this sentinel the model would receive two CommandDoneMsgs —
// one from the main goroutine and one from the draining helper — causing the
// "Done." line to appear twice.
type channelDrainedMsg struct{}

type DirtyServicesLoadedMsg struct {
	ServiceCount  int
	DirtyServices []string
}

type OpenCandidatesLoadedMsg struct {
	TaskID     string
	Candidates task.OpenCandidates
}

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

func loadServicesCmd(mgr task.Manager, taskID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 30*time.Second)
		defer cancel()
		services, err := mgr.ListServices(ctx, taskID)
		if err != nil {
			// The task was deleted just before this refresh; treat as empty list.
			if errors.Is(err, task.ErrTaskNotFound) {
				return ServicesLoadedMsg{TaskID: taskID, Services: nil}
			}
			return CommandDoneMsg{Err: err}
		}
		return ServicesLoadedMsg{TaskID: taskID, Services: services}
	}
}

func loadReposCmd(mgr task.Manager) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		repos, err := mgr.DiscoverRepos(ctx)
		if err != nil {
			return ReposLoadedMsg{Err: err}
		}
		return ReposLoadedMsg{Repos: repos}
	}
}

func loadDirtyServicesCmd(mgr task.Manager, taskID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 30*time.Second)
		defer cancel()
		services, err := mgr.ListServices(ctx, taskID)
		if err != nil {
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

func initTaskCmd(mgr task.Manager, params task.InitParams) tea.Cmd {
	statusCh := make(chan string, 32)
	params.StatusCh = statusCh
	return tea.Batch(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), params.TaskID), 5*time.Minute)
			defer cancel()
			err := mgr.Init(ctx, params)
			close(statusCh)
			return CommandDoneMsg{Err: err}
		},
		readNextLine(statusCh),
	)
}

func addServiceCmd(mgr task.Manager, params task.AddParams) tea.Cmd {
	statusCh := make(chan string, 32)
	params.StatusCh = statusCh
	return tea.Batch(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), params.TaskID), 5*time.Minute)
			defer cancel()
			err := mgr.Add(ctx, params)
			close(statusCh)
			return CommandDoneMsg{Err: err}
		},
		readNextLine(statusCh),
	)
}

func removeTaskCmd(mgr task.Manager, taskID string, force, deleteBranches bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 5*time.Minute)
		defer cancel()
		return CommandDoneMsg{Err: mgr.Remove(ctx, taskID, force, deleteBranches)}
	}
}

func generateSlnCmd(mgr task.Manager, taskID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 5*time.Minute)
		defer cancel()
		err := mgr.GenerateSln(ctx, taskID)
		return CommandDoneMsg{Err: err}
	}
}

func loadOpenCandidatesCmd(mgr task.Manager, taskID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 10*time.Second)
		defer cancel()
		candidates, err := mgr.ListOpenCandidates(ctx, taskID)
		if err != nil {
			return CommandDoneMsg{Err: err}
		}
		return OpenCandidatesLoadedMsg{TaskID: taskID, Candidates: candidates}
	}
}

func openFileCmd(mgr task.Manager, path, app string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := mgr.OpenFile(ctx, path, app)
		return CommandDoneMsg{Err: err}
	}
}

func cloneTaskCmd(mgr task.Manager, src, dst string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), dst), 5*time.Minute)
		defer cancel()
		return CommandDoneMsg{Err: mgr.CloneTask(ctx, src, dst)}
	}
}

func syncTaskCmd(mgr task.Manager, taskID string) tea.Cmd {
	statusCh := make(chan string, 32)
	return tea.Batch(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 5*time.Minute)
			defer cancel()
			return CommandDoneMsg{Err: mgr.SyncTask(ctx, taskID, statusCh)}
		},
		readNextLine(statusCh),
	)
}

func pushTaskCmd(mgr task.Manager, taskID string) tea.Cmd {
	statusCh := make(chan string, 32)
	return tea.Batch(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 5*time.Minute)
			defer cancel()
			return CommandDoneMsg{Err: mgr.PushTask(ctx, taskID, statusCh)}
		},
		readNextLine(statusCh),
	)
}

func pushServiceCmd(mgr task.Manager, taskID, serviceName string) tea.Cmd {
	statusCh := make(chan string, 32)
	return tea.Batch(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 5*time.Minute)
			defer cancel()
			err := mgr.PushService(ctx, taskID, serviceName, statusCh)
			close(statusCh)
			return CommandDoneMsg{Err: err}
		},
		readNextLine(statusCh),
	)
}

func stashServiceCmd(mgr task.Manager, taskID, serviceName string, pop bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 30*time.Second)
		defer cancel()
		return CommandDoneMsg{Err: mgr.StashService(ctx, taskID, serviceName, pop)}
	}
}

func removeServiceCmd(mgr task.Manager, taskID, serviceName string, removeBranch bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 30*time.Second)
		defer cancel()
		return CommandDoneMsg{Err: mgr.RemoveService(ctx, taskID, serviceName, removeBranch)}
	}
}

func readNextLine(ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return channelDrainedMsg{}
		}
		return OutputLineMsg{Line: line, Next: readNextLine(ch)}
	}
}

// execShellCmd hands the terminal over to an interactive shell in the task's
// working directory. This is the only place in the TUI that calls exec.Command
// directly without routing through task.Manager — this is intentional.
//
// Routing through Manager would require returning a subprocess handle from the
// Manager interface just to pass to tea.ExecProcess, which would violate
// Manager's contract (blocking calls only). The risk is low: this function is
// a single-use TUI escape hatch, not business logic.
//
// See ADR-004 for the full rationale.
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
