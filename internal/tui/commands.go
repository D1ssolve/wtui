package tui

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/logutil"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/panels"
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

type CloneSourceServicesLoadedMsg struct {
	SourceTaskID string
	Services     []domain.Service
	Err          error
}

type OutputLineMsg struct {
	Line string
	Next tea.Cmd
}

type CommandDoneMsg struct {
	Err error
	Op  string
}

type LazygitDoneMsg struct {
	TaskID       string
	ServiceName  string
	WorktreePath string
	Err          error
}

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
			return CommandDoneMsg{Err: err, Op: "Load tasks"}
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
			return CommandDoneMsg{Err: err, Op: "Load services for task " + taskID}
		}
		return ServicesLoadedMsg{TaskID: taskID, Services: services}
	}
}

func loadCloneSourceServicesCmd(mgr task.Manager, taskID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 30*time.Second)
		defer cancel()
		services, err := mgr.ListServices(ctx, taskID)
		return CloneSourceServicesLoadedMsg{SourceTaskID: taskID, Services: services, Err: err}
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
			return CommandDoneMsg{Err: err, Op: "Init task " + params.TaskID}
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
			return CommandDoneMsg{Err: err, Op: "Add services to " + params.TaskID}
		},
		readNextLine(statusCh),
	)
}

func removeTaskCmd(mgr task.Manager, taskID string, force, deleteBranches bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 5*time.Minute)
		defer cancel()
		return CommandDoneMsg{Err: mgr.Remove(ctx, taskID, force, deleteBranches), Op: "Remove task " + taskID}
	}
}

func syncTaskCmd(mgr task.Manager, taskID string, strategy task.SyncStrategy) tea.Cmd {
	statusCh := make(chan string, 32)
	return tea.Batch(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 5*time.Minute)
			defer cancel()
			return CommandDoneMsg{Err: mgr.SyncTask(ctx, taskID, strategy, statusCh), Op: "Sync task " + taskID}
		},
		readNextLine(statusCh),
	)
}

func riderTaskCmd(taskID, dir string) tea.Cmd {
	name, args := riderTaskArgs(taskID)
	return execProcessCmd(name, args, dir, "Open Rider for "+taskID)
}

func riderTaskArgs(taskID string) (string, []string) {
	return "rider", []string{taskID + ".sln"}
}

func codeWorkspaceTaskCmd(editor, taskID, dir string) tea.Cmd {
	name, args := codeWorkspaceTaskArgs(editor, taskID)
	return execProcessCmd(name, args, dir, "Open "+editor+" for "+taskID)
}

func codeWorkspaceTaskArgs(editor, taskID string) (string, []string) {
	return editor, []string{taskID + ".code-workspace"}
}

func lazygitServiceCmd(taskID, serviceName, worktreePath string) tea.Cmd {
	c := lazygitServiceExecCmd(worktreePath)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return lazygitServiceDoneMsg(taskID, serviceName, worktreePath, err)
	})
}

func lazygitServiceExecCmd(worktreePath string) *exec.Cmd {
	name, args := lazygitServiceArgs(worktreePath)
	c := exec.Command(name, args...)
	c.Dir = worktreePath
	return c
}

func lazygitServiceArgs(worktreePath string) (string, []string) {
	return "lazygit", []string{"-p", worktreePath}
}

func lazygitServiceDoneMsg(taskID, serviceName, worktreePath string, err error) tea.Msg {
	return LazygitDoneMsg{
		TaskID:       taskID,
		ServiceName:  serviceName,
		WorktreePath: worktreePath,
		Err:          err,
	}
}

func pushTaskCmd(mgr task.Manager, taskID string) tea.Cmd {
	statusCh := make(chan string, 32)
	return tea.Batch(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 5*time.Minute)
			defer cancel()
			return CommandDoneMsg{Err: mgr.PushTask(ctx, taskID, statusCh), Op: "Push task " + taskID}
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
			return CommandDoneMsg{Err: err, Op: "Push service " + serviceName}
		},
		readNextLine(statusCh),
	)
}

func syncServiceCmd(mgr task.Manager, taskID, serviceName string, strategy task.SyncStrategy) tea.Cmd {
	statusCh := make(chan string, 32)
	return tea.Batch(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 5*time.Minute)
			defer cancel()
			return CommandDoneMsg{Err: mgr.SyncService(ctx, taskID, serviceName, strategy, statusCh), Op: "Sync service " + serviceName}
		},
		readNextLine(statusCh),
	)
}

func stashServiceCmd(mgr task.Manager, taskID, serviceName string, pop bool, includeUntracked bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 30*time.Second)
		defer cancel()
		op := "Stashing service " + serviceName
		if pop {
			op = "Unstashing service " + serviceName
		}
		return CommandDoneMsg{Err: mgr.StashService(ctx, taskID, serviceName, pop, includeUntracked), Op: op}
	}
}

func removeServiceCmd(mgr task.Manager, taskID, serviceName string, removeBranch bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 30*time.Second)
		defer cancel()
		return CommandDoneMsg{Err: mgr.RemoveService(ctx, taskID, serviceName, removeBranch), Op: "Remove service " + serviceName}
	}
}

func validateTaskCmd(mgr task.Manager, taskID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 30*time.Second)
		defer cancel()

		validation, err := mgr.ValidateTask(ctx, taskID)
		if err != nil {
			return CommandDoneMsg{Err: err, Op: "Validate task " + taskID}
		}

		return ValidationResultMsg{Validation: validation}
	}
}

func planCloseTaskCmd(mgr task.Manager, taskID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 30*time.Second)
		defer cancel()

		plan, err := mgr.PlanCloseTask(ctx, taskID)
		return ClosePlanReadyMsg{Plan: plan, Err: err}
	}
}

func closeTaskCmd(mgr task.Manager, params task.CloseTaskParams) tea.Cmd {
	statusCh := make(chan string, 32)
	doneCh := make(chan CloseTaskFinishedMsg, 1)
	params.StatusCh = statusCh

	go func() {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), params.TaskID), 10*time.Minute)
		defer cancel()
		result, err := mgr.CloseTask(ctx, params)
		doneCh <- CloseTaskFinishedMsg{Result: result, Err: err}
		close(doneCh)
	}()

	return readStatusOrDone(statusCh, doneCh)
}

func scanPrunableTasksCmd(mgr task.Manager) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		candidates, err := mgr.ScanPrunableTasks(ctx)
		return PrunePlanReadyMsg{Candidates: candidates, Err: err}
	}
}

func pruneTasksCmd(mgr task.Manager, taskIDs []string) tea.Cmd {
	statusCh := make(chan string, 32)
	doneCh := make(chan PruneFinishedMsg, 1)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		removed := make([]string, 0, len(taskIDs))
		errList := make([]error, 0)

		for _, taskID := range taskIDs {
			if taskID == "" {
				continue
			}

			statusCh <- "Pruning task " + taskID + "..."
			if err := mgr.Remove(logutil.WithTaskID(ctx, taskID), taskID, true, false); err != nil {
				errList = append(errList, err)
				statusCh <- "Prune task " + taskID + " failed: " + err.Error()
				continue
			}

			removed = append(removed, taskID)
			statusCh <- "Prune task " + taskID + " done."
		}

		close(statusCh)
		doneCh <- PruneFinishedMsg{Removed: removed, Errors: errList}
		close(doneCh)
	}()

	return readStatusOrDone(statusCh, doneCh)
}

func listTagsCmd(mgr task.Manager, taskID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 30*time.Second)
		defer cancel()

		tags, err := mgr.ListTags(ctx, taskID)
		return TagListMsg{TaskID: taskID, Tags: tags, Err: err}
	}
}

func loadPromoteVersionsCmd(mgr task.Manager, taskID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 30*time.Second)
		defer cancel()

		services, err := mgr.ListServices(ctx, taskID)
		if err != nil {
			return CommandDoneMsg{Err: err, Op: "Load promote versions for task " + taskID}
		}

		versions := make(map[string]string, len(services))
		for _, svc := range services {
			name := strings.TrimSpace(svc.Name)
			if name == "" {
				continue
			}
			versions[name] = "0.1.0"
		}

		return panels.PromoteVersionsLoadedMsg{TaskID: taskID, Versions: versions}
	}
}

func promoteToReleaseCmd(mgr task.Manager, taskID string, versions map[string]string) tea.Cmd {
	statusCh := make(chan string, 32)
	params := task.PromoteToReleaseParams{
		TaskID:   taskID,
		Versions: versions,
		StatusCh: statusCh,
	}

	return tea.Batch(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 10*time.Minute)
			defer cancel()

			promotedTask, err := mgr.PromoteToRelease(ctx, params)
			close(statusCh)
			return PromoteToReleaseDoneMsg{Task: promotedTask, Err: err}
		},
		readNextLine(statusCh),
	)
}

type forgePipelineStatusParams struct {
	Branch string
}

func forgeOpCmd(mgr task.Manager, op string, taskID string, serviceName string, params any) tea.Cmd {
	return func() tea.Msg {
		ctxBase := context.Background()
		if taskID != "" {
			ctxBase = logutil.WithTaskID(ctxBase, taskID)
		}
		ctx, cancel := context.WithTimeout(ctxBase, 2*time.Minute)
		defer cancel()

		switch op {
		case "create_mr":
			p, ok := params.(forge.CreateMRParams)
			if !ok {
				return ForgeResultMsg{ServiceName: serviceName, Op: op, Err: errors.New("invalid params for create_mr")}
			}
			result, err := mgr.ForgeCreateMR(ctx, taskID, serviceName, p)
			return ForgeResultMsg{ServiceName: serviceName, Op: op, Data: result, Err: err}

		case "pipeline_status":
			p, ok := params.(forgePipelineStatusParams)
			if !ok {
				return ForgeResultMsg{ServiceName: serviceName, Op: op, Err: errors.New("invalid params for pipeline_status")}
			}
			result, err := mgr.ForgePipelineStatus(ctx, taskID, serviceName, p.Branch)
			return ForgeResultMsg{ServiceName: serviceName, Op: op, Data: result, Err: err}

		case "list_issues":
			p, ok := params.(forge.ListIssuesParams)
			if !ok {
				return ForgeResultMsg{ServiceName: serviceName, Op: op, Err: errors.New("invalid params for list_issues")}
			}
			result, err := mgr.ForgeListIssues(ctx, taskID, serviceName, p)
			return ForgeResultMsg{ServiceName: serviceName, Op: op, Data: result, Err: err}

		default:
			return ForgeResultMsg{ServiceName: serviceName, Op: op, Err: errors.New("unsupported forge operation: " + op)}
		}
	}
}

func readStatusOrDone[T any](statusCh <-chan string, doneCh <-chan T) tea.Cmd {
	var next func() tea.Cmd
	next = func() tea.Cmd {
		return func() tea.Msg {
			ch := statusCh
			if ch != nil {
				select {
				case line, ok := <-ch:
					if ok {
						return OutputLineMsg{Line: line, Next: next()}
					}
					ch = nil
				default:
				}
			}

			select {
			case line, ok := <-ch:
				if ok {
					return OutputLineMsg{Line: line, Next: next()}
				}
			case msg := <-doneCh:
				return any(msg).(tea.Msg)
			}

			msg := <-doneCh
			return any(msg).(tea.Msg)
		}
	}

	return next()
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

func execProcessCmd(name string, args []string, dir string, op string) tea.Cmd {
	c := exec.Command(name, args...)
	c.Dir = dir
	return execTeaProcess(c, op)
}

func execTeaProcess(c *exec.Cmd, op string) tea.Cmd {
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return execProcessDoneMsg(op, err)
	})
}

func execProcessDoneMsg(op string, err error) tea.Msg {
	return CommandDoneMsg{Err: err, Op: op}
}
