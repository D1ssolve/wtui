package task

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestReleaseCancellationDoesNotTakeErrorRoute(t *testing.T) {
	m, _ := newFinishTestManager(t)
	useReleaseMachine(t, m, `    machine:
      initial: qualify
      states:
        qualify: {wait: manual, on: {approve: {key: F, to: verify}}}
        verify:
          run: [{hook: {name: verify, command: "exit 1"}}]
          next: done
          on_error: refused
        done: {final: released}
        refused: {final: rejected}
`)
	release := machineRelease(t, m, "verify", finalizeService(m))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := m.runReleaseMachine(ctx, release, &releaseRun{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run returned status %q, err %v; want cancellation", got.Status, err)
	}
	stored, err := m.GetRelease(t.Context(), release.ID)
	if err != nil || stored.Status != domain.ReleaseStatusFailed || stored.State != "verify" || stored.Error == nil || !stored.Error.Recoverable {
		t.Fatalf("persisted result = %+v, err %v; want recoverable verify state", stored, err)
	}
}

func TestTaskCancellationPreservesRetryStateAndCause(t *testing.T) {
	rule := taskMachineRule(t, filepath.Join(t.TempDir(), "log"))
	stand, _ := rule.Machine.State("stand")
	stand.OnError = "done"
	m := newRuleWorkflowTestManager(t, rule, false).(*manager)
	tf, err := m.loadTaskFlow(t.Context(), "TASK-1")
	if err != nil || tf == nil {
		t.Fatalf("load task flow: %v", err)
	}
	if err := m.enterTaskState(tf, "stand", "close"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := m.runTaskMachine(ctx, tf, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("run returned %v; want typed cancellation", err)
	}
	summary, err := m.TaskWorkflow(t.Context(), "TASK-1")
	if err != nil || summary.Current != "stand" || len(summary.Events) != 1 || summary.Events[0].Key != "F" {
		t.Fatalf("persisted workflow = %+v, err %v; want stand and retry", summary, err)
	}
}
