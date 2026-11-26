package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/fsutil"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/shell"
)

const taskFlowFileName = ".wtui-task-flow.json"

type taskFlowState struct {
	Type                string                     `json:"type"`
	Fingerprint         string                     `json:"fingerprint"`
	State               string                     `json:"state"`
	History             []domain.ReleaseTransition `json:"history,omitempty"`
	HooksDone           []string                   `json:"hooks_done,omitempty"`
	Error               string                     `json:"error,omitempty"`
	HookStopUnconfirmed bool                       `json:"hook_stop_unconfirmed,omitempty"`
}

type taskFlow struct {
	taskID   string
	machine  *gitflow.Machine
	services []domain.Service
	state    taskFlowState
}

var ErrTaskFlowStep = errors.New("task workflow")

// loadTaskFlow returns nil when the task's type has no machine.
func (m *manager) loadTaskFlow(ctx context.Context, taskID string) (*taskFlow, error) {
	if m.flow == nil {
		return nil, nil
	}
	services, err := m.ListServices(ctx, taskID)
	if err != nil || len(services) == 0 {
		return nil, err
	}
	return m.readTaskFlow(taskID, services)
}

func (m *manager) readTaskFlow(taskID string, services []domain.Service) (*taskFlow, error) {
	rules := serviceRulesIdentity{}
	var typ gitflow.BranchType
	mixed := false
	for i, svc := range services {
		rules.add(m.flow, svc.Name, svc.Branch)
		bt := gitflow.DetectBranchType(svc.Branch, m.flow)
		if i == 0 {
			typ = bt
		}
		mixed = mixed || bt != typ
	}
	rule := m.flow.BranchTypes[typ]
	if mixed {
		for _, svc := range services {
			if m.flow.BranchTypes[gitflow.DetectBranchType(svc.Branch, m.flow)].Machine != nil {
				return nil, fmt.Errorf("%w: task %s mixes branch types; a task type with a machine needs every service on that type", ErrTaskFlowStep, taskID)
			}
		}
		return nil, nil
	}
	if rule.Machine == nil {
		return nil, nil
	}
	fingerprint := rules.bind(rule.Machine.Fingerprint())
	tf := &taskFlow{taskID: taskID, machine: rule.Machine, services: services}
	data, err := os.ReadFile(filepath.Join(m.taskDir(taskID), taskFlowFileName))
	switch {
	case errors.Is(err, os.ErrNotExist):
		tf.state = taskFlowState{Type: string(typ), Fingerprint: fingerprint, State: rule.Machine.Initial}
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(data, &tf.state); err != nil {
			return nil, fmt.Errorf("%w: %s is corrupt: %v", ErrTaskFlowStep, taskFlowFileName, err)
		}
		if tf.state.Type != string(typ) || tf.state.Fingerprint != fingerprint {
			return nil, fmt.Errorf("%w: task %s started under a different %s machine; restore that configuration or delete %s in the task directory to restart its workflow", ErrTaskFlowStep, taskID, typ, taskFlowFileName)
		}
	}
	return tf, nil
}

func (m *manager) saveTaskFlow(tf *taskFlow) error {
	data, err := json.MarshalIndent(tf.state, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(filepath.Join(m.taskDir(tf.taskID), taskFlowFileName), data, 0o644)
}

func (tf *taskFlow) current() *gitflow.State {
	st, _ := tf.machine.State(tf.state.State)
	return st
}

func (m *manager) enterTaskState(tf *taskFlow, to, event string) error {
	tf.state.History = append(tf.state.History, domain.ReleaseTransition{From: tf.state.State, To: to, Event: event, At: defaultReleaseNow().UTC()})
	tf.state.HooksDone = slices.DeleteFunc(tf.state.HooksDone, func(h string) bool { return strings.HasPrefix(h, to+"/") })
	tf.state.State = to
	return m.saveTaskFlow(tf)
}

func taskEventFor(st *gitflow.State, action string) *gitflow.Event {
	if st == nil || st.Kind != gitflow.StateWait {
		return nil
	}
	for i := range st.Events {
		if st.Events[i].Action == action {
			return &st.Events[i]
		}
	}
	return nil
}

// gateTaskAction refuses close or merge outside the machine state that offers it.
func (m *manager) gateTaskAction(ctx context.Context, taskID, action string) (*taskFlow, error) {
	tf, err := m.loadTaskFlow(ctx, taskID)
	if err != nil || tf == nil {
		return nil, err
	}
	if taskEventFor(tf.current(), action) == nil {
		return nil, fmt.Errorf("%w: task %s is in state %s, where %s is not available; %s", ErrTaskFlowStep, taskID, tf.state.State, action, taskStateGuidance(tf.current()))
	}
	return tf, nil
}

// afterTaskAction moves the machine once a gated close or merge succeeded.
func (m *manager) afterTaskAction(ctx context.Context, tf *taskFlow, action string, statusCh chan<- string) error {
	if tf == nil {
		return nil
	}
	ev := taskEventFor(tf.current(), action)
	if ev == nil {
		return nil
	}
	if ev.To != "" {
		if err := m.enterTaskState(tf, ev.To, ev.Name); err != nil {
			return err
		}
	}
	return m.runTaskMachine(ctx, tf, statusCh)
}

// AdvanceTask fires a named event of the task's current wait state, or re-checks the
// wait condition when event is empty, then runs the machine until it waits again.
func (m *manager) AdvanceTask(ctx context.Context, taskID, event string, statusCh chan<- string) error {
	unlock, err := m.lockTask(ctx, taskID)
	if err != nil {
		return err
	}
	defer unlock()
	tf, err := m.loadTaskFlow(ctx, taskID)
	if err != nil {
		return err
	}
	if tf == nil {
		return fmt.Errorf("%w: task %s has no workflow machine", ErrTaskFlowStep, taskID)
	}
	st := tf.current()
	if st == nil || st.Kind != gitflow.StateWait {
		if st != nil && st.Kind == gitflow.StateRun {
			if tf.state.HookStopUnconfirmed {
				return fmt.Errorf("%w: hook shutdown was not confirmed; inspect remaining processes before recovery", ErrTaskFlowStep)
			}
			return m.runTaskMachine(ctx, tf, statusCh) // retry of a failed run state
		}
		return fmt.Errorf("%w: task %s is in state %s; nothing waits for input", ErrTaskFlowStep, taskID, tf.state.State)
	}
	if event == "" {
		return m.runTaskMachine(ctx, tf, statusCh)
	}
	for _, ev := range st.Events {
		if ev.Name != event {
			continue
		}
		if ev.Action != "" {
			return fmt.Errorf("%w: %s runs through %s", ErrTaskFlowStep, ev.Name, map[string]string{gitflow.EventActionClose: "C", gitflow.EventActionMergeReviews: "M"}[ev.Action])
		}
		if ev.When == gitflow.WaitCIGreen {
			green, detail, err := m.taskCIGreen(ctx, tf, gitflow.TaskBranchRef)
			if err != nil {
				return err
			}
			if !green {
				return fmt.Errorf("%w: %s requires green CI: %s", ErrTaskFlowStep, ev.Name, detail)
			}
		}
		tf.state.Error = ""
		if err := m.enterTaskState(tf, ev.To, ev.Name); err != nil {
			return err
		}
		return m.runTaskMachine(ctx, tf, statusCh)
	}
	return fmt.Errorf("%w: state %s of task %s has no event %q", ErrTaskFlowStep, st.ID, taskID, event)
}

func (m *manager) runTaskMachine(ctx context.Context, tf *taskFlow, statusCh chan<- string) error {
	for {
		st := tf.current()
		if st == nil {
			return fmt.Errorf("%w: unknown state %q", ErrTaskFlowStep, tf.state.State)
		}
		switch st.Kind {
		case gitflow.StateFinal:
			return m.saveTaskFlow(tf)
		case gitflow.StateWait:
			if st.Wait == gitflow.WaitManual {
				return m.saveTaskFlow(tf)
			}
			holds, err := m.taskWaitHolds(ctx, tf, st)
			if err != nil {
				if workflowInterrupted(err) {
					return err
				}
				sendStatus(statusCh, fmt.Sprintf("[%s][%s] %v", tf.taskID, st.ID, err))
			}
			if err != nil || !holds {
				return m.saveTaskFlow(tf)
			}
			if err := m.enterTaskState(tf, st.Next, st.Wait); err != nil {
				return err
			}
		case gitflow.StateRun:
			if err := m.runTaskState(ctx, tf, st, statusCh); err != nil {
				tf.state.Error = err.Error()
				if workflowInterrupted(err) {
					return m.saveTaskInterruption(tf, err)
				}
				if st.OnError != "" {
					sendStatus(statusCh, fmt.Sprintf("[%s][%s] %v; continuing with %s", tf.taskID, st.ID, err, st.OnError))
					if err := m.enterTaskState(tf, st.OnError, "error"); err != nil {
						return err
					}
					continue
				}
				if saveErr := m.saveTaskFlow(tf); saveErr != nil {
					return errors.Join(err, saveErr)
				}
				return fmt.Errorf("%w: state %s failed (press F to retry): %w", ErrTaskFlowStep, st.ID, err)
			}
			tf.state.Error = ""
			if err := m.enterTaskState(tf, st.Next, ""); err != nil {
				return err
			}
		}
	}
}

func (m *manager) runTaskState(ctx context.Context, tf *taskFlow, st *gitflow.State, statusCh chan<- string) error {
	for i, step := range st.Run {
		for _, svc := range tf.services {
			var err error
			switch step.Kind {
			case gitflow.StepHook:
				done := fmt.Sprintf("%s/%d/%s/%s", st.ID, i, step.Name, svc.Name)
				if slices.Contains(tf.state.HooksDone, done) {
					continue
				}
				if err = m.runTaskHook(ctx, tf, st.ID, step, svc, statusCh); err == nil {
					tf.state.HooksDone = append(tf.state.HooksDone, done)
					err = m.saveTaskFlow(tf)
				}
			case gitflow.StepPipeline:
				var client forge.ForgeClient
				var repo string
				if client, repo, err = m.serviceForgeTarget(ctx, svc); err == nil {
					branch := taskBranchFor(svc, step.On)
					sendStatus(statusCh, fmt.Sprintf("[%s][pipeline] triggering on %s", svc.Name, branch))
					err = client.TriggerPipeline(ctx, forge.TriggerPipelineParams{WorktreePath: svc.WorktreePath, Branch: branch, Repo: repo})
				}
			default:
				err = fmt.Errorf("task action %s is not supported", step.Kind)
			}
			if err != nil {
				return fmt.Errorf("%s for %s: %w", step.Kind, svc.Name, err)
			}
		}
	}
	return nil
}

func (m *manager) runTaskHook(ctx context.Context, tf *taskFlow, stateID string, step gitflow.Step, svc domain.Service, statusCh chan<- string) error {
	timeout, err := time.ParseDuration(step.Timeout)
	if err != nil {
		return err
	}
	sendStatus(statusCh, fmt.Sprintf("[%s][hook] %s: %s", svc.Name, step.Name, step.Command))
	hookCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := shell.Command(hookCtx, step.Command)
	cmd.Dir = svc.WorktreePath
	cmd.Env = append(os.Environ(),
		"WTUI_TASK_ID="+tf.taskID,
		"WTUI_STATE="+stateID,
		"WTUI_SERVICE="+svc.Name,
		"WTUI_BRANCH="+svc.Branch,
		"WTUI_REPO="+svc.RepoPath,
		"WTUI_WORKTREE="+svc.WorktreePath,
	)
	out, runErr := shell.HookOutput(hookCtx, cmd)
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	for _, line := range lines[max(0, len(lines)-hookOutputLines):] {
		if line != "" {
			sendStatus(statusCh, fmt.Sprintf("[%s][hook %s] %s", svc.Name, step.Name, line))
		}
	}
	if errors.Is(runErr, context.DeadlineExceeded) && ctx.Err() == nil {
		return fmt.Errorf("hook %s timed out after %s", step.Name, timeout)
	}
	if runErr != nil {
		return fmt.Errorf("hook %s: %w", step.Name, runErr)
	}
	return nil
}

func taskBranchFor(svc domain.Service, ref string) string {
	if ref == gitflow.TaskBranchRef {
		return svc.Branch
	}
	return ref
}

func (m *manager) serviceForgeTarget(ctx context.Context, svc domain.Service) (forge.ForgeClient, string, error) {
	client, err := m.forgeClientForService(ctx, svc)
	if err != nil {
		return nil, "", err
	}
	remoteURL, err := m.git.RemoteURL(ctx, svc.WorktreePath, m.cfg.ServiceRemote().String())
	if err != nil {
		return nil, "", err
	}
	repo := forge.ExtractRepoPath(remoteURL)
	if repo == "" {
		return nil, "", fmt.Errorf("remote URL %q of %s is not parseable", remoteURL, svc.Name)
	}
	return client, repo, nil
}

func (m *manager) taskWaitHolds(ctx context.Context, tf *taskFlow, st *gitflow.State) (bool, error) {
	switch st.Wait {
	case gitflow.WaitReviewMerged:
		// Manual re-check may fetch objects; tracking refs are not proof.
		for _, svc := range tf.services {
			if err := m.git.Fetch(ctx, svc.RepoPath); err != nil {
				return false, err
			}
		}
		return m.taskReviewWaitHolds(ctx, tf)
	case gitflow.WaitCIGreen:
		green, _, err := m.taskCIGreen(ctx, tf, st.WaitOn)
		return green, err
	}
	return false, nil
}

func (m *manager) taskCIGreen(ctx context.Context, tf *taskFlow, ref string) (bool, string, error) {
	for _, svc := range tf.services {
		client, repo, err := m.serviceForgeTarget(ctx, svc)
		if err != nil {
			return false, "", err
		}
		if green, detail, err := m.ciGreenOnHead(ctx, client, repo, svc.WorktreePath, svc.Name, taskBranchFor(svc, ref)); err != nil || !green {
			return false, detail, err
		}
	}
	return true, "", nil
}

func taskStateGuidance(st *gitflow.State) string {
	switch {
	case st == nil:
		return "check the workflow configuration"
	case st.Kind == gitflow.StateFinal:
		return "the task workflow is finished"
	case st.Kind == gitflow.StateRun:
		return "press F to run " + st.Label + " again"
	case st.Wait == gitflow.WaitReviewMerged:
		return "merge the MRs (M), then press F to re-check"
	case st.Wait == gitflow.WaitCIGreen:
		return "waiting for green CI on " + st.WaitOn + "; press F to re-check"
	}
	actions := make([]string, 0, len(st.Events))
	for _, ev := range st.Events {
		actions = append(actions, "press "+ev.Key+" to "+ev.Label)
	}
	return strings.Join(actions, " · ")
}

func (m *manager) taskMachineWorkflow(tf *taskFlow) domain.WorkflowSummary {
	st := tf.current()
	summary := domain.WorkflowSummary{
		Steps:      releasePath(tf.machine, tf.state.State),
		Current:    domain.WorkflowPhase(tf.state.State),
		NextAction: taskStateGuidance(st),
		Blocker:    tf.state.Error,
	}
	if st != nil && st.Kind == gitflow.StateWait {
		hasF := false
		for _, ev := range st.Events {
			summary.Events = append(summary.Events, domain.WorkflowEvent{
				Name: ev.Name, Key: ev.Key, Label: ev.Label, Action: ev.Action,
				Cancellable: ev.Action == "" && tf.machine.CancellablePath(ev.To),
			})
			hasF = hasF || ev.Key == "F"
		}
		if st.Wait != gitflow.WaitManual && !hasF {
			summary.Events = append(summary.Events, domain.WorkflowEvent{Key: "F", Label: "re-check " + strings.ReplaceAll(st.Wait, "_", " "), Cancellable: tf.machine.CancellablePath(st.Next)})
		}
	}
	if st != nil && st.Kind == gitflow.StateRun && !tf.state.HookStopUnconfirmed {
		summary.Events = []domain.WorkflowEvent{{Key: "F", Label: "retry " + st.Label, Cancellable: tf.machine.CancellablePath(st.ID)}}
	}
	if tf.state.HookStopUnconfirmed {
		summary.NextAction = "Inspect remaining hook processes before recovery"
	}
	final := st != nil && st.Kind == gitflow.StateFinal
	result := workflowSummaryWithServices(summary.Steps, summary.Current, summary.NextAction, summary.Blocker,
		final && st.Outcome == gitflow.OutcomeReleased, tf.state.Error != "" || final && st.Outcome == gitflow.OutcomeRejected, nil)
	if final {
		result.Outcome = st.Outcome
	}
	result.Events, result.Gated = summary.Events, true
	return result
}
