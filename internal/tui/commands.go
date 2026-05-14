package tui

import (
	"context"
	"errors"
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

type channelDrainedMsg struct{}

type DirtyServicesLoadedMsg struct {
	ServiceCount  int
	DirtyServices []string
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

			if errors.Is(err, task.ErrTaskNotFound) {
				return ServicesLoadedMsg{TaskID: taskID, Services: nil}
			}
			return CommandDoneMsg{Err: err}
		}
		return ServicesLoadedMsg{TaskID: taskID, Services: services}
	}
}

func loadReposCmd(mgr task.Manager, force bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		repos, err := mgr.Repos(ctx, force)
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

func syncTaskCmd(mgr task.Manager, taskID string, strategy task.SyncStrategy) tea.Cmd {
	statusCh := make(chan string, 32)
	return tea.Batch(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 5*time.Minute)
			defer cancel()
			return CommandDoneMsg{Err: mgr.SyncTask(ctx, taskID, strategy, statusCh)}
		},
		readNextLine(statusCh),
	)
}

func riderTaskCmd(taskID, dir string) tea.Cmd {
	name, args := riderTaskArgs(taskID)
	return execProcessCmd(name, args, dir)
}

func riderTaskArgs(taskID string) (string, []string) {
	return "rider", []string{taskID + ".sln"}
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

func execShellCmd(cmd, dir string) tea.Cmd {
	c := exec.Command("sh", "-c", cmd)
	c.Dir = dir
	return execTeaProcess(c)
}

func execProcessCmd(name string, args []string, dir string) tea.Cmd {
	c := exec.Command(name, args...)
	c.Dir = dir
	return execTeaProcess(c)
}

func execTeaProcess(c *exec.Cmd) tea.Cmd {
	return tea.ExecProcess(c, execProcessDoneMsg)
}

func execProcessDoneMsg(err error) tea.Msg {
	return CommandDoneMsg{Err: err}
}
