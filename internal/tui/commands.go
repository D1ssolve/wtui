package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/logutil"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	"github.com/D1ssolve/wtui/internal/tui/panels"
)

type TasksLoadedMsg struct{ Tasks []domain.Task }

type ReposLoadedMsg struct {
	Repos []domain.Repo
	Err   error
}

type ServicesLoadedMsg struct {
	TaskID     string
	Generation uint64
	Services   []domain.Service
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
	Generation uint64
	Err        error
	Op         string
}

type PartialInitDoneMsg struct {
	Generation uint64
	Result     task.PartialFailureResult
	Err        error
	Op         string
}

type PartialAddDoneMsg struct {
	Generation uint64
	Result     task.PartialFailureResult
	Err        error
	Op         string
}

type LazygitDoneMsg struct {
	Generation   uint64
	TaskID       string
	ServiceName  string
	WorktreePath string
	Err          error
}

type channelDrainedMsg struct{}

type LoadFailedMsg struct {
	Err error
	Op  string
}

type DirtyServicesLoadedMsg struct {
	ServiceCount  int
	DirtyServices []string
}

func loadTasksCmd(mgr task.Manager, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		tasks, err := mgr.List(ctx)
		if err != nil {
			return LoadFailedMsg{Err: err, Op: "Load tasks"}
		}
		return TasksLoadedMsg{Tasks: tasks}
	}
}

func loadServicesCmd(mgr task.Manager, taskID string, generation, opGeneration uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 30*time.Second)
		defer cancel()
		services, err := mgr.ListServices(ctx, taskID)
		if err != nil {

			if errors.Is(err, task.ErrTaskNotFound) {
				return ServicesLoadedMsg{TaskID: taskID, Generation: generation, Services: nil}
			}
			return LoadFailedMsg{Err: err, Op: "Load services for task " + taskID}
		}
		return ServicesLoadedMsg{TaskID: taskID, Generation: generation, Services: services}
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

func initTaskCmd(mgr task.Manager, params task.InitParams, generation uint64) tea.Cmd {
	statusCh := make(chan string, 32)
	params.StatusCh = statusCh
	return tea.Batch(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), params.TaskID), 5*time.Minute)
			defer cancel()
			partial, err := mgr.Init(ctx, params)
			close(statusCh)
			if err != nil && len(partial.SucceededServices) > 0 && len(partial.FailedServices) > 0 {
				return PartialInitDoneMsg{Generation: generation, Result: partial, Err: err, Op: "Init task " + params.TaskID}
			}
			return CommandDoneMsg{Generation: generation, Err: err, Op: "Init task " + params.TaskID}
		},
		readNextLine(statusCh),
	)
}

func addServiceCmd(mgr task.Manager, params task.AddParams, generation uint64) tea.Cmd {
	statusCh := make(chan string, 32)
	params.StatusCh = statusCh
	return tea.Batch(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), params.TaskID), 5*time.Minute)
			defer cancel()
			partial, err := mgr.Add(ctx, params)
			close(statusCh)
			if err != nil && len(partial.SucceededServices) > 0 && len(partial.FailedServices) > 0 {
				return PartialAddDoneMsg{Generation: generation, Result: partial, Err: err, Op: "Add services to " + params.TaskID}
			}
			return CommandDoneMsg{Generation: generation, Err: err, Op: "Add services to " + params.TaskID}
		},
		readNextLine(statusCh),
	)
}

func removeTaskCmd(mgr task.Manager, taskID string, opts task.RemoveOptions, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 5*time.Minute)
		defer cancel()
		return CommandDoneMsg{Generation: generation, Err: mgr.Remove(ctx, taskID, opts), Op: "Remove task " + taskID}
	}
}

func convertHotfixCmd(mgr task.Manager, params task.ConvertHotfixParams, generation uint64) tea.Cmd {
	statusCh := make(chan string, 32)
	params.StatusCh = statusCh
	return tea.Batch(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), params.SourceTaskID), 10*time.Minute)
			defer cancel()
			err := mgr.ConvertHotfixToFeature(ctx, params)
			close(statusCh)
			return ConvertHotfixDoneMsg{Generation: generation, SourceTaskID: params.SourceTaskID, TargetTaskID: params.TargetTaskID, Err: err}
		},
		readNextLine(statusCh),
	)
}

func syncTaskCmd(mgr task.Manager, taskID string, strategy task.SyncStrategy, generation uint64) tea.Cmd {
	statusCh := make(chan string, 32)
	return tea.Batch(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 5*time.Minute)
			defer cancel()
			err := mgr.SyncTask(ctx, taskID, strategy, statusCh)
			close(statusCh)
			return CommandDoneMsg{Generation: generation, Err: err, Op: "Sync task " + taskID}
		},
		readNextLine(statusCh),
	)
}

func riderTaskCmd(taskID, dir string, generation uint64) tea.Cmd {
	return execProcessCmd("rider", []string{taskID + ".sln"}, dir, "Open Rider for "+taskID, generation)
}

func codeWorkspaceTaskCmd(editor, taskID, dir string, generation uint64) tea.Cmd {
	return execProcessCmd(editor, []string{taskID + ".code-workspace"}, dir, "Open "+editor+" for "+taskID, generation)
}

func openReleaseFolderCmd(executable, releaseID, dir string, generation uint64) tea.Cmd {
	return func() tea.Msg {
		path := dir
		var err error
		if dir == "" {
			err = errors.New("release directory is empty")
		} else {
			var absolute string
			absolute, err = filepath.Abs(dir)
			if err == nil {
				path = absolute
				var info os.FileInfo
				info, err = os.Stat(path)
				if err == nil && !info.IsDir() {
					err = errors.New("release path is not a directory")
				}
			}
		}
		op := fmt.Sprintf("Open %s for release %s folder %q", executable, releaseID, path)
		if err != nil {
			return execProcessDoneMsg(op, fmt.Errorf("%s: %w", op, err), generation)
		}
		return execProcessCmd(executable, []string{path}, path, op, generation)()
	}
}

func lazygitServiceCmd(taskID, serviceName, worktreePath string, generation uint64) tea.Cmd {
	c := lazygitServiceExecCmd(worktreePath)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return LazygitDoneMsg{
			Generation:   generation,
			TaskID:       taskID,
			ServiceName:  serviceName,
			WorktreePath: worktreePath,
			Err:          err,
		}
	})
}

func lazygitServiceExecCmd(worktreePath string) *exec.Cmd {
	c := exec.Command("lazygit", "-p", worktreePath)
	c.Dir = worktreePath
	return c
}

func pushTaskCmd(mgr task.Manager, taskID string, generation uint64) tea.Cmd {
	statusCh := make(chan string, 32)
	return tea.Batch(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 5*time.Minute)
			defer cancel()
			err := mgr.PushTask(ctx, taskID, statusCh)
			close(statusCh)
			return CommandDoneMsg{Generation: generation, Err: err, Op: "Push task " + taskID}
		},
		readNextLine(statusCh),
	)
}

func pushServiceCmd(mgr task.Manager, taskID, serviceName string, generation uint64) tea.Cmd {
	statusCh := make(chan string, 32)
	return tea.Batch(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 5*time.Minute)
			defer cancel()
			err := mgr.PushService(ctx, taskID, serviceName, statusCh)
			close(statusCh)
			return CommandDoneMsg{Generation: generation, Err: err, Op: "Push service " + serviceName}
		},
		readNextLine(statusCh),
	)
}

func syncServiceCmd(mgr task.Manager, taskID, serviceName string, strategy task.SyncStrategy, generation uint64) tea.Cmd {
	statusCh := make(chan string, 32)
	return tea.Batch(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 5*time.Minute)
			defer cancel()
			err := mgr.SyncService(ctx, taskID, serviceName, strategy, statusCh)
			close(statusCh)
			return CommandDoneMsg{Generation: generation, Err: err, Op: "Sync service " + serviceName}
		},
		readNextLine(statusCh),
	)
}

func stashServiceCmd(mgr task.Manager, taskID, serviceName string, pop bool, includeUntracked bool, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 30*time.Second)
		defer cancel()
		op := "Stashing service " + serviceName
		if pop {
			op = "Unstashing service " + serviceName
		}
		return CommandDoneMsg{Generation: generation, Err: mgr.StashService(ctx, taskID, serviceName, pop, includeUntracked), Op: op}
	}
}

func removeServiceCmd(mgr task.Manager, taskID, serviceName string, removeBranch bool, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 30*time.Second)
		defer cancel()
		return CommandDoneMsg{Generation: generation, Err: mgr.RemoveService(ctx, taskID, serviceName, removeBranch), Op: "Remove service " + serviceName}
	}
}

func validateTaskCmd(mgr task.Manager, taskID string, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 30*time.Second)
		defer cancel()

		validation, err := mgr.ValidateTask(ctx, taskID)
		if err != nil {
			return CommandDoneMsg{Generation: generation, Err: err, Op: "Validate task " + taskID}
		}

		return ValidationResultMsg{Generation: generation, Validation: validation}
	}
}

func planCloseTaskCmd(mgr task.Manager, taskID string, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 30*time.Second)
		defer cancel()

		plan, err := mgr.PlanCloseTask(ctx, taskID)
		return ClosePlanReadyMsg{Generation: generation, Plan: plan, Err: err}
	}
}

func closeTaskCmd(mgr task.Manager, params task.CloseTaskParams, generation uint64) tea.Cmd {
	statusCh := make(chan string, 32)
	doneCh := make(chan CloseTaskFinishedMsg, 1)
	params.StatusCh = statusCh

	go func() {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), params.TaskID), 10*time.Minute)
		defer cancel()
		result, err := mgr.CloseTask(ctx, params)
		doneCh <- CloseTaskFinishedMsg{Generation: generation, Result: result, DryRun: params.DryRun, Err: err}
		close(doneCh)
	}()

	return readStatusOrDone(statusCh, doneCh)
}

// scanCleanupCandidatesCmd runs a read-only scan over every task and every
// released release, planning cleanup for each so the dialog can show readiness
// and block reasons. Planning never mutates; per-item plan failures block that
// item instead of failing the scan.
func scanCleanupCandidatesCmd(mgr task.Manager, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		tasks, err := mgr.List(ctx)
		if err != nil {
			return CleanupScanReadyMsg{Generation: generation, Err: fmt.Errorf("list tasks: %w", err)}
		}
		releases, err := mgr.ListReleases(ctx)
		if err != nil {
			return CleanupScanReadyMsg{Generation: generation, Err: fmt.Errorf("list releases: %w", err)}
		}

		candidates := make([]modal.CleanupCandidate, 0, len(tasks)+len(releases))
		for _, taskInfo := range tasks {
			candidate := modal.CleanupCandidate{Kind: modal.CleanupKindTask, ID: taskInfo.ID}
			plan, planErr := mgr.PlanTaskCleanup(ctx, task.TaskCleanupRequest{TaskID: taskInfo.ID})
			switch {
			case planErr != nil:
				candidate.Reason = "plan failed: " + planErr.Error()
			default:
				preview := plan.Preview()
				candidate.Services = len(preview.Services)
				for _, service := range preview.Services {
					if service.WorktreePath != "" {
						candidate.Resources++
					}
				}
				switch {
				case len(preview.Blockers) > 0:
					candidate.Reason = strings.Join(preview.Blockers, "; ")
				case len(preview.Services) == 0:
					candidate.Reason = "no cleanup steps planned"
				default:
					candidate.Ready = true
				}
			}
			candidates = append(candidates, candidate)
			if ctx.Err() != nil {
				return CleanupScanReadyMsg{Generation: generation, Err: ctx.Err()}
			}
		}
		for _, release := range releases {
			if release.Status != domain.ReleaseStatusReleased {
				continue
			}
			candidate := modal.CleanupCandidate{Kind: modal.CleanupKindRelease, ID: release.ID}
			plan, planErr := mgr.PlanReleaseCleanup(ctx, release.ID, task.ReleaseCleanupSelection{RemoveRelease: true})
			switch {
			case planErr != nil:
				candidate.Reason = "plan failed: " + planErr.Error()
			default:
				preview := plan.Preview()
				candidate.Services = len(preview.Services)
				candidate.Resources = len(preview.Tasks)
				if len(preview.Blockers) > 0 {
					candidate.Reason = strings.Join(preview.Blockers, "; ")
				} else {
					candidate.Ready = true
				}
			}
			candidates = append(candidates, candidate)
			if ctx.Err() != nil {
				return CleanupScanReadyMsg{Generation: generation, Err: ctx.Err()}
			}
		}
		return CleanupScanReadyMsg{Generation: generation, Candidates: candidates}
	}
}

func listTagsCmd(mgr task.Manager, taskID string, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 30*time.Second)
		defer cancel()

		tags, err := mgr.ListTags(ctx, taskID)
		return TagListMsg{Generation: generation, TaskID: taskID, Tags: tags, Err: err}
	}
}

func loadReleasesCmd(mgr task.Manager) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		releases, err := mgr.ListReleases(ctx)
		return ReleasesLoadedMsg{Releases: releases, Err: err}
	}
}

func planReleaseCleanupCmd(mgr task.Manager, releaseID string, selection task.ReleaseCleanupSelection, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		plan, err := mgr.PlanReleaseCleanup(ctx, releaseID, selection)
		return ReleaseCleanupPlanReadyMsg{Generation: generation, Plan: plan, Err: err}
	}
}

func executeReleaseCleanupCmd(mgr task.Manager, plan task.ReleaseCleanupPlan, releaseID string, generation uint64) tea.Cmd {
	statusCh := make(chan string, 32)
	doneCh := make(chan ReleaseCleanupDoneMsg, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		result, err := mgr.ExecuteReleaseCleanup(ctx, plan, statusCh)
		if result.ReleaseID == "" {
			result.ReleaseID = releaseID
		}
		close(statusCh)
		doneCh <- ReleaseCleanupDoneMsg{Generation: generation, Result: result, Err: err}
		close(doneCh)
	}()
	return readStatusOrDone(statusCh, doneCh)
}

func planTaskCleanupCmd(mgr task.Manager, taskID string, generation, operationGeneration uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 2*time.Minute)
		defer cancel()
		plan, err := mgr.PlanTaskCleanup(ctx, task.TaskCleanupRequest{TaskID: taskID})
		return TaskCleanupPlanReadyMsg{TaskID: taskID, Generation: generation, OperationGeneration: operationGeneration, Plan: plan, Err: err}
	}
}

func executeTaskCleanupCmd(mgr task.Manager, plan task.TaskCleanupPlan, taskID string, generation, operationGeneration uint64) tea.Cmd {
	statusCh := make(chan string, 32)
	doneCh := make(chan TaskCleanupDoneMsg, 1)
	go func() {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 10*time.Minute)
		defer cancel()
		result, err := mgr.ExecuteTaskCleanup(ctx, plan, statusCh)
		if result.TaskID == "" {
			result.TaskID = taskID
		}
		close(statusCh)
		doneCh <- TaskCleanupDoneMsg{
			TaskID:              taskID,
			Generation:          generation,
			OperationGeneration: operationGeneration,
			Result:              result,
			Err:                 err,
		}
		close(doneCh)
	}()
	return readStatusOrDone(statusCh, doneCh)
}

func createReleaseCmd(mgr task.Manager, params task.CreateReleaseParams, generation uint64) tea.Cmd {
	statusCh := make(chan string, 32)
	doneCh := make(chan CreateReleaseDoneMsg, 1)
	params.StatusCh = statusCh

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		release, err := mgr.CreateRelease(ctx, params)
		close(statusCh)
		doneCh <- CreateReleaseDoneMsg{Generation: generation, Release: release, Err: err}
		close(doneCh)
	}()

	return readStatusOrDone(statusCh, doneCh)
}

func inspectTaskMergeCmd(mgr task.Manager, taskID string, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 2*time.Minute)
		defer cancel()
		inspection, err := mgr.InspectTaskMerge(ctx, taskID)
		return TaskMergeInspectionMsg{TaskID: taskID, Generation: generation, Inspection: inspection, Err: err}
	}
}

func inspectReleaseMergeCmd(mgr task.Manager, releaseID string, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		inspection, err := mgr.InspectReleaseMerge(ctx, releaseID)
		return ReleaseMergeInspectionMsg{ReleaseID: releaseID, Generation: generation, Inspection: inspection, Err: err}
	}
}

func mergeTaskMRsCmd(mgr task.Manager, taskID string, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 10*time.Minute)
		defer cancel()
		result, err := mgr.MergeTaskMRs(ctx, taskID)
		return TaskMergeDoneMsg{Generation: generation, Result: result, Err: err}
	}
}

func mergeServiceMRCmd(mgr task.Manager, taskID, serviceName string, generation uint64, selection ...task.MRSelection) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 10*time.Minute)
		defer cancel()
		result, err := mgr.MergeServiceMR(ctx, taskID, serviceName, selection...)
		return TaskMergeDoneMsg{Generation: generation, Result: result, Err: err}
	}
}

func mergeReleaseMRsCmd(mgr task.Manager, releaseID string, generation uint64) tea.Cmd {
	statusCh := make(chan string, 32)
	doneCh := make(chan ReleaseMergeDoneMsg, 1)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		release, result, err := mgr.MergeReleaseMRs(ctx, releaseID, statusCh)
		close(statusCh)
		doneCh <- ReleaseMergeDoneMsg{Generation: generation, Release: release, Result: result, Err: err}
		close(doneCh)
	}()

	return readStatusOrDone(statusCh, doneCh)
}

func promoteReleaseCmd(mgr task.Manager, releaseID string, generation uint64) tea.Cmd {
	return releaseActionCmd(mgr, "promote", releaseID, generation)
}

func finalizeReleaseCmd(mgr task.Manager, releaseID string, generation uint64) tea.Cmd {
	return releaseActionCmd(mgr, "finalize", releaseID, generation)
}

func retryReleaseCmd(mgr task.Manager, releaseID string, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		release, err := mgr.RetryRelease(ctx, releaseID)
		return ReleaseActionDoneMsg{Generation: generation, Action: "retry", Release: release, Err: err}
	}
}

func planReleaseTaskMergesCmd(mgr task.Manager, params task.CreateReleaseParams, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		plan, err := mgr.PlanReleaseTaskMerges(ctx, params)
		return ReleaseTaskMergePlanReadyMsg{Generation: generation, Plan: plan, Err: err}
	}
}

func planReleaseTaskMergeRetryCmd(mgr task.Manager, releaseID string, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		plan, err := mgr.PlanReleaseTaskMergeRetry(ctx, releaseID)
		return ReleaseTaskMergeRetryPlanReadyMsg{ReleaseID: releaseID, Generation: generation, Plan: plan, Err: err}
	}
}

func retryReleaseTaskMergesCmd(mgr task.Manager, releaseID string, plan *task.ReleaseTaskMergePlan, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		release, err := mgr.RetryReleaseTaskMerges(ctx, releaseID, plan)
		return ReleaseActionDoneMsg{Generation: generation, Action: "retry", Release: release, Err: err}
	}
}

func releaseActionCmd(mgr task.Manager, action, releaseID string, generation uint64) tea.Cmd {
	statusCh := make(chan string, 32)
	doneCh := make(chan ReleaseActionDoneMsg, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		var release domain.Release
		var err error
		if action == "promote" {
			release, err = mgr.PromoteRelease(ctx, releaseID, statusCh)
		} else {
			release, err = mgr.FinalizeRelease(ctx, task.FinishReleaseParams{ReleaseID: releaseID, StatusCh: statusCh})
		}
		close(statusCh)
		doneCh <- ReleaseActionDoneMsg{Generation: generation, Action: action, Release: release, Err: err}
		close(doneCh)
	}()
	return readStatusOrDone(statusCh, doneCh)
}

func loadTaskWorkflowCmd(mgr task.Manager, taskID string, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(logutil.WithTaskID(context.Background(), taskID), 30*time.Second)
		defer cancel()
		workflow, err := mgr.TaskWorkflow(ctx, taskID)
		return TaskWorkflowLoadedMsg{TaskID: taskID, Generation: generation, Workflow: workflow, Err: err}
	}
}

func loadReleaseVersionsCmd(mgr task.Manager, taskIDs []string, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		versions, err := mgr.ProposeReleaseVersions(ctx, taskIDs)
		if err != nil {
			return CommandDoneMsg{Generation: generation, Err: err, Op: "Load release versions"}
		}

		return panels.ReleaseVersionsLoadedMsg{Versions: versions}
	}
}

type forgePipelineStatusParams struct {
	Branch   string
	Provider forge.ForgeProvider
}

type forgeCreateMRParams struct {
	Title string
	Force bool
}

func forgeOpCmd(mgr task.Manager, op string, taskID string, serviceName string, params any, generation uint64) tea.Cmd {
	return func() tea.Msg {
		ctxBase := context.Background()
		if taskID != "" {
			ctxBase = logutil.WithTaskID(ctxBase, taskID)
		}
		ctx, cancel := context.WithTimeout(ctxBase, 2*time.Minute)
		defer cancel()

		switch op {
		case "create_missing_mrs":
			p, ok := params.(forgeCreateMRParams)
			if !ok {
				return ForgeResultMsg{Generation: generation, TaskID: taskID, Op: op, Err: errors.New("invalid params for create_missing_mrs")}
			}
			result, err := mgr.ForgeCreateMissingMRs(ctx, taskID, p.Title, p.Force)
			return ForgeResultMsg{Generation: generation, TaskID: taskID, Op: op, Data: result, Err: err}

		case "pipeline_status":
			p, ok := params.(forgePipelineStatusParams)
			if !ok {
				return ForgeResultMsg{Generation: generation, ServiceName: serviceName, Op: op, Err: errors.New("invalid params for pipeline_status")}
			}
			result, err := mgr.ForgePipelineStatus(ctx, taskID, serviceName, p.Branch)
			return ForgeResultMsg{Generation: generation, ServiceName: serviceName, Op: op, Provider: p.Provider, Data: result, Err: err}

		case "list_issues":
			p, ok := params.(forge.ListIssuesParams)
			if !ok {
				return ForgeResultMsg{Generation: generation, ServiceName: serviceName, Op: op, Err: errors.New("invalid params for list_issues")}
			}
			result, err := mgr.ForgeListIssues(ctx, taskID, serviceName, p)
			return ForgeResultMsg{Generation: generation, ServiceName: serviceName, Op: op, Data: result, Err: err}

		default:
			return ForgeResultMsg{Generation: generation, ServiceName: serviceName, Op: op, Err: errors.New("unsupported forge operation: " + op)}
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

func shellExecCommand(command, dir string) *exec.Cmd {
	c := exec.Command("sh", "-c", command)
	c.Dir = dir
	return c
}

func execShellCmd(command, dir string, generation uint64) tea.Cmd {
	return execTeaProcess(shellExecCommand(command, dir), "Run shell command", generation)
}

func execProcessCmd(name string, args []string, dir string, op string, generation uint64) tea.Cmd {
	c := exec.Command(name, args...)
	c.Dir = dir
	return execTeaProcess(c, op, generation)
}

func execTeaProcess(c *exec.Cmd, op string, generation uint64) tea.Cmd {
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return execProcessDoneMsg(op, err, generation)
	})
}

func execProcessDoneMsg(op string, err error, generation uint64) tea.Msg {
	return CommandDoneMsg{Generation: generation, Err: err, Op: op}
}
