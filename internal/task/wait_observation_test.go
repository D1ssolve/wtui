package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func TestObserveTaskWait_UsesExactMergedReviewWhenTargetIsNotCached(t *testing.T) {
	for _, tc := range []struct {
		name, merged, wantKey string
	}{
		{"matching live target", "live-target", "F"},
		{"unconfirmed target", "different-target", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hook := filepath.Join(t.TempDir(), "unexpected-hook")
			rule := taskMachineRule(t, hook)
			mgr := newRuleWorkflowTestManager(t, rule, false).(*manager)
			git := mgr.git.(*mockGitClient)
			git.getWorktreeBranchFn = func(path string) (string, error) { return "feature/" + filepath.Base(path), nil }
			git.remoteRefSHAFn = func(_, _ string) (string, error) { return "live-target", nil }
			git.resolveRefFn = func(_, ref string) (string, error) { return ref + "-head", nil }
			git.isAncestorFn = func(_, _, _ string) (bool, error) { return false, errors.New("target object not cached") }
			git.fetchFn = func(_ string) error { t.Fatal("read-only observer called fetch"); return nil }
			client := &workflowHistoryForgeClient{
				history: map[string][]forge.MRInfo{
					"feature/a": {{Number: 11, State: "merged", SourceBranch: "feature/a", TargetBranch: "develop"}},
					"feature/b": {{Number: 12, State: "merged", SourceBranch: "feature/b", TargetBranch: "develop"}},
				},
				byNumber: map[int]forge.MRReadiness{
					11: {Number: 11, State: "merged", SourceBranch: "feature/a", TargetBranch: "develop", HeadSHA: "feature/a-head", MergedSHA: "live-target"},
					12: {Number: 12, State: "merged", SourceBranch: "feature/b", TargetBranch: "develop", HeadSHA: "feature/b-head", MergedSHA: tc.merged},
				},
			}
			mgr.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitLab: client}
			state := `{"type":"feature","fingerprint":"` + rule.Machine.Fingerprint() + `","state":"review"}`
			path := filepath.Join(mgr.taskDir("TASK-1"), taskFlowFileName)
			if err := os.WriteFile(path, []byte(state), 0o644); err != nil {
				t.Fatal(err)
			}
			services, err := mgr.ListServices(t.Context(), "TASK-1")
			if err != nil {
				t.Fatal(err)
			}
			got, err := mgr.ObserveTaskWait(t.Context(), domain.Task{ID: "TASK-1", Phase: "feature", Services: services}, "review")
			if err != nil || got != (WaitObservation{Ready: tc.wantKey != "", Key: tc.wantKey}) {
				t.Fatalf("observation = %+v, err = %v; want key %q", got, err, tc.wantKey)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != state {
				t.Fatalf("observation modified task state: %q, %v", data, err)
			}
		})
	}
}

func TestObserveTaskWait_GuardedFOnGreenCIActuallyAdvances(t *testing.T) {
	rule := taskMachineRule(t, filepath.Join(t.TempDir(), "unexpected-hook"))
	rule.Machine = &gitflow.Machine{Initial: "qa", States: []gitflow.State{
		{ID: "qa", Kind: gitflow.StateWait, Wait: gitflow.WaitCIGreen, WaitOn: gitflow.TaskBranchRef, Next: "done",
			Events: []gitflow.Event{{Name: "accept", Key: "F", When: gitflow.WaitCIGreen, To: "done"}}},
		{ID: "done", Kind: gitflow.StateFinal, Outcome: gitflow.OutcomeReleased},
	}}
	mgr := newRuleWorkflowTestManager(t, rule, false).(*manager)
	git := mgr.git.(*mockGitClient)
	git.getWorktreeBranchFn = func(path string) (string, error) { return "feature/" + filepath.Base(path), nil }
	git.remoteRefSHAFn = func(_, _ string) (string, error) { return "live-head", nil }
	mgr.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitLab: greenWaitForge{}}
	services, err := mgr.ListServices(t.Context(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	got, err := mgr.ObserveTaskWait(t.Context(), domain.Task{ID: "TASK-1", Phase: "feature", Services: services}, "qa")
	if err != nil || got != (WaitObservation{Ready: true, Key: "F"}) {
		t.Fatalf("green guarded wait: observation = %+v, err = %v; want ready/F", got, err)
	}
	if _, err := os.Stat(filepath.Join(mgr.taskDir("TASK-1"), taskFlowFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("observing CI persisted task state: %v", err)
	}
	if err := mgr.AdvanceTask(t.Context(), "TASK-1", rule.Machine.States[0].Events[0].Name, nil); err != nil {
		t.Fatal(err)
	}
	summary, err := mgr.TaskWorkflow(t.Context(), "TASK-1")
	if err != nil || summary.Current != "done" {
		t.Fatalf("manual guarded event did not finish: current=%q err=%v", summary.Current, err)
	}
}

type greenWaitForge struct{ forge.ForgeClient }

func (greenWaitForge) PipelineStatus(_ context.Context, _, _ string) ([]forge.PipelineStatus, error) {
	return []forge.PipelineStatus{{HeadSHA: "live-head", Status: "success"}}, nil
}
