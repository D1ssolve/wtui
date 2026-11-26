package task

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func TestObserveTaskWait_UsesExactMergedReviewWhenTargetIsNotCached(t *testing.T) {
	for _, tc := range []struct {
		name, merged, wantKey string
		changedRef, newSHA    string
		refError              bool
	}{
		{name: "unchanged squash", merged: "live-target", wantKey: "F"},
		{name: "unconfirmed merge SHA", merged: "different-target"},
		{name: "published source advanced", merged: "live-target", wantKey: "F", changedRef: "refs/heads/feature/b", newSHA: "unmerged-successor"},
		{name: "published source deleted", merged: "live-target", wantKey: "F", changedRef: "refs/heads/feature/b"},
		{name: "published target deleted despite tracking ref", merged: "live-target", wantKey: "F", changedRef: "refs/heads/develop"},
		{name: "published target moved", merged: "live-target", wantKey: "F", changedRef: "refs/heads/develop", newSHA: "different-live-tip"},
		{name: "target unverifiable", merged: "live-target", wantKey: "F", changedRef: "refs/heads/develop", refError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hook := filepath.Join(t.TempDir(), "unexpected-hook")
			rule := taskMachineRule(t, hook)
			mgr := newRuleWorkflowTestManager(t, rule, false).(*manager)
			git := mgr.git.(*mockGitClient)
			git.getWorktreeBranchFn = func(path string) (string, error) { return "feature/" + filepath.Base(path), nil }
			changed := false
			git.remoteRefSHAFn = func(repo, ref string) (string, error) {
				if changed && filepath.Base(repo) == "b" && ref == tc.changedRef {
					if tc.refError {
						return "", errors.New("remote unavailable")
					}
					return tc.newSHA, nil
				}
				if ref == "refs/heads/develop" {
					return "live-target", nil
				}
				return strings.TrimPrefix(ref, "refs/heads/") + "-head", nil
			}
			git.resolveRefFn = func(_, ref string) (string, error) {
				if ref == "origin/develop" {
					return "live-target", nil // retained even after remote deletion or drift
				}
				return strings.TrimPrefix(ref, "refs/heads/") + "-head", nil
			}
			manual := false
			git.isAncestorFn = func(_, _, _ string) (bool, error) {
				if !manual {
					return false, errors.New("target object not cached")
				}
				return false, nil // squash is not ancestral even after fetching
			}
			git.fetchFn = func(_ string) error {
				if !manual {
					t.Fatal("read-only observer called fetch")
				}
				return nil
			}
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
			selected := domain.Task{ID: "TASK-1", Phase: "feature", Services: services}
			got, err := mgr.ObserveTaskWait(t.Context(), selected, "review")
			if err != nil || got != (WaitObservation{Ready: tc.wantKey != "", Key: tc.wantKey}) {
				t.Fatalf("observation = %+v, err = %v; want key %q", got, err, tc.wantKey)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != state {
				t.Fatalf("observation modified task state: %q, %v", data, err)
			}
			changed, manual = true, true
			if err := mgr.AdvanceTask(t.Context(), "TASK-1", "", nil); err != nil {
				t.Fatal(err)
			}
			data, err = os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var persisted taskFlowState
			if err := json.Unmarshal(data, &persisted); err != nil {
				t.Fatal(err)
			}
			if tc.wantKey == "" || tc.changedRef != "" {
				if persisted.State != "review" || len(persisted.History) != 0 {
					t.Fatalf("manual F accepted stale or unproven delivery: %s", data)
				}
			} else if persisted.State != "done" || len(persisted.History) != 1 || persisted.History[0].Event != "review_merged" {
				t.Fatalf("manual F did not finish verified squash: %s", data)
			}
			if tc.changedRef != "" {
				manual = false
				got, _ := mgr.ObserveTaskWait(t.Context(), selected, "review")
				if got.Ready || got.Key != "" {
					t.Fatalf("fresh observer accepted changed refs: %+v", got)
				}
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

func TestTaskReviewWaitRequiresEveryServiceTarget(t *testing.T) {
	flow, err := gitflow.ParseWorkflowYAML(`workflow:
  extends: git-flow
  task_types:
    feature:
      prefixes: [feature/]
      from: integration
      close: [{review: {into: [develop, next]}}]
      machine:
        states:
          coding: {wait: manual, on: {close: {key: C, action: close, to: review}}}
          review: {wait: review_merged, next: done}
          done: {final: released}
  services:
    a: {task_types: {feature: {close: [{review: {into: [main, stable]}}]}}}
`)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, missing string }{{"override", "a/stable"}, {"inherited", "b/next"}, {"all delivered", ""}} {
		t.Run(tc.name, func(t *testing.T) {
			m := newRuleWorkflowTestManager(t, flow.BranchTypes[gitflow.BranchTypeFeature], false).(*manager)
			m.flow = flow
			g := m.git.(*mockGitClient)
			g.getWorktreeBranchFn = func(path string) (string, error) { return "feature/" + filepath.Base(path), nil }
			g.resolveRefFn = func(repo, ref string) (string, error) { return filepath.Base(repo) + "-head", nil }
			g.remoteRefSHAFn = func(repo, ref string) (string, error) {
				name := filepath.Base(repo)
				branch := strings.TrimPrefix(ref, "refs/heads/")
				if branch == "feature/"+name {
					return name + "-head", nil
				}
				key := name + "/" + branch
				if key == tc.missing {
					return "", nil
				}
				switch key {
				case "a/main", "a/stable", "b/develop", "b/next":
					return name + "-tip", nil
				}
				return "", nil
			}
			g.isAncestorFn = func(repo, source, target string) (bool, error) {
				name := filepath.Base(repo)
				return source == name+"-head" && target == name+"-tip", nil
			}
			manual := false
			g.fetchFn = func(string) error {
				if !manual {
					t.Fatal("poll fetched")
				}
				return nil
			}
			ctx := t.Context()
			const id = "TASK-1"
			svcs, err := m.ListServices(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			tf, err := m.loadTaskFlow(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			tf.state.State = "review"
			if err = m.saveTaskFlow(tf); err != nil {
				t.Fatal(err)
			}
			statePath := filepath.Join(m.taskDir(id), taskFlowFileName)
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			got, err := m.ObserveTaskWait(ctx, domain.Task{ID: id, Phase: "feature", Services: svcs}, "review")
			want := WaitObservation{}
			state := "review"
			if tc.missing == "" {
				want = WaitObservation{Ready: true, Key: "F"}
				state = "done"
			}
			if err != nil || got != want {
				t.Errorf("observation: %+v, %v; want %+v", got, err, want)
			}
			if after, err := os.ReadFile(statePath); err != nil || string(after) != string(before) {
				t.Fatalf("poll changed persisted state: %s, %v", after, err)
			}
			manual = true
			if err := m.AdvanceTask(ctx, id, "", nil); err != nil {
				t.Fatal(err)
			}
			summary, err := m.TaskWorkflow(ctx, id)
			if err != nil || string(summary.Current) != state {
				t.Fatalf("manual re-check: %s, %v; want %s", summary.Current, err, state)
			}
		})
	}
}
