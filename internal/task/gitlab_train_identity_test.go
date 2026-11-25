package task

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func trainIdentityManager(t *testing.T) (*manager, *mockGitClient, string) {
	t.Helper()
	client, effects := fakeGitLabTrain(t, "feature/a", "develop")
	m, g := newMRMergeTestManager(t, map[string]string{"a": "feature/a"}, trainRemote, client)
	m.cfg.Forge.GitLabHost = "git.corp.invalid"
	m.cfg.Forge.MergeQueue = true
	g.resolveRefRes = "reviewed-sha"
	g.remoteRefSHAFn = func(_, _ string) (string, error) { return "reviewed-sha", nil }
	g.isAncestorFn = func(_, from, _ string) (bool, error) { return from == "delivered", nil }
	return m, g, effects
}

func TestGateOpenIID(t *testing.T) {
	m, g, effects := trainIdentityManager(t)
	t.Setenv("TRAIN_IID", "8")
	result, err := m.MergeTaskMRs(t.Context(), "TASK-1")
	writes, readErr := os.ReadFile(effects)
	if err != nil || result.Errs["a"] == nil || len(result.Merged)+len(result.Queued) != 0 ||
		readErr != nil || len(writes) != 0 || len(leasePushes(g)) != 0 {
		t.Fatalf("list !7, detail !8: result=%+v err=%v effects=%q readErr=%v", result, err, writes, readErr)
	}
}

func TestGateNextM(t *testing.T) {
	for _, tc := range []struct {
		name, enroll, state  string
		remote, head, target string
		source               string
		replacement, merged  bool
		failed               bool
		queueDisabled        bool
	}{
		{name: "replacement_after_ack", enroll: "schedule", state: "closed", replacement: true, failed: true},
		{name: "replacement_after_lost_response", enroll: "lost-response", state: "closed", replacement: true, failed: true},
		{name: "delivery_after_lost_response", enroll: "lost-response", state: "merged", merged: true},
		{name: "invisible_car_after_lost_response", enroll: "lost-response", state: "opened"},
		{name: "scheduled_before_car_exists", enroll: "schedule", state: "opened"},
		{name: "queue_disabled_after_attempt", enroll: "schedule", state: "opened", queueDisabled: true},
		{name: "different_project", enroll: "schedule", state: "merged", remote: "git@git.corp.invalid:group/nested/other.git", failed: true},
		{name: "different_host", enroll: "schedule", state: "merged", remote: "git@other.invalid:group/nested/repo.git", failed: true},
		{name: "changed_head", enroll: "schedule", state: "merged", head: "other-sha", failed: true},
		{name: "retargeted", enroll: "schedule", state: "merged", target: "other-target", failed: true},
		{name: "changed_source", enroll: "schedule", state: "merged", source: "feature/other", failed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, g, effects := trainIdentityManager(t)
			t.Setenv("TRAIN_MODE", tc.enroll)
			first, err := m.MergeTaskMRs(t.Context(), "TASK-1")
			if err != nil || len(first.Merged) != 0 || (first.Errs["a"] != nil) != (tc.enroll == "lost-response") ||
				slices.Equal(first.Queued, []string{"a"}) != (tc.enroll == "schedule") {
				t.Fatalf("first M: result=%+v err=%v", first, err)
			}
			t.Setenv("TRAIN_STATE", tc.state)
			t.Setenv("TRAIN_MODE", "schedule")
			if tc.remote != "" {
				g.remoteURLRes = tc.remote
				t.Setenv("TRAIN_REPO", forge.ExtractRepoPath(tc.remote))
				m.cfg.Forge.GitLabHost = forge.RemoteHost(tc.remote)
			}
			if tc.head != "" {
				t.Setenv("TRAIN_HEAD", tc.head)
			}
			if tc.target != "" {
				t.Setenv("TRAIN_TARGET", tc.target)
			}
			if tc.source != "" {
				t.Setenv("TRAIN_SOURCE", tc.source)
			}
			if tc.replacement {
				t.Setenv("TRAIN_REPLACEMENT", "yes")
			}
			m.cfg.Forge.MergeQueue = !tc.queueDisabled
			restart := newTestManagerWithDeps(t, m.cfg, g, m.flow, map[forge.ForgeProvider]forge.ForgeClient{
				forge.ForgeProviderGitLab: forge.NewGlabClient(m.cfg.RootDir),
			})

			next, err := restart.MergeTaskMRs(t.Context(), "TASK-1")
			writes, readErr := os.ReadFile(effects)
			if err != nil || readErr != nil || string(writes) != "POST\n" || len(leasePushes(g)) != 0 ||
				slices.Equal(next.Merged, []string{"a"}) != tc.merged || len(next.Queued) != 0 ||
				(next.Errs["a"] != nil) != tc.failed {
				t.Fatalf("second M: result=%+v err=%v effects=%q readErr=%v", next, err, writes, readErr)
			}
		})
	}
}

func restartedTrainReview(t *testing.T) (Manager, *mockGitClient, string) {
	t.Helper()
	m, g, effects := trainIdentityManager(t)
	rule := taskMachineRule(t, filepath.Join(t.TempDir(), "log"))
	m.flow = &gitflow.ResolvedGitFlow{
		IntegrationBranch: "develop",
		BranchTypes:       map[gitflow.BranchType]gitflow.BranchTypeRule{gitflow.BranchTypeFeature: rule},
	}
	state := `{"type":"feature","fingerprint":"` + rule.Machine.Fingerprint() + `","state":"review"}`
	if err := os.WriteFile(filepath.Join(m.taskDir("TASK-1"), taskFlowFileName), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	g.getWorktreeBranchFn = func(string) (string, error) { return "feature/a", nil }
	g.remoteRefSHAFn = func(_, ref string) (string, error) {
		if ref == "refs/heads/develop" {
			return "target-tip", nil
		}
		return "reviewed-sha", nil
	}
	if result, err := m.MergeTaskMRs(t.Context(), "TASK-1"); err != nil || !slices.Equal(result.Queued, []string{"a"}) {
		t.Fatalf("enrollment: result=%+v err=%v", result, err)
	}
	g.isAncestorFn = func(_, from, to string) (bool, error) {
		return (from == "reviewed-sha" || from == "delivered") && to == "target-tip", nil
	}
	t.Setenv("TRAIN_STATE", "closed")
	t.Setenv("TRAIN_REPLACEMENT", "yes")
	restart := newTestManagerWithDeps(t, m.cfg, g, m.flow, map[forge.ForgeProvider]forge.ForgeClient{
		forge.ForgeProviderGitLab: forge.NewGlabClient(m.cfg.RootDir),
	})
	return restart, g, effects
}

func TestGateTaskMachineWaitsForPinnedTrainDelivery(t *testing.T) {
	restart, g, effects := restartedTrainReview(t)
	g.isAncestorFn = func(_, _, _ string) (bool, error) { return true, nil }
	if err := restart.AdvanceTask(t.Context(), "TASK-1", "", nil); err != nil {
		t.Fatal(err)
	}
	pending, err := restart.TaskWorkflow(t.Context(), "TASK-1")
	if err != nil || pending.Current != "review" {
		t.Fatalf("replacement ancestry advanced workflow: summary=%+v err=%v", pending, err)
	}
	t.Setenv("TRAIN_STATE", "merged")
	result, err := restart.MergeTaskMRs(t.Context(), "TASK-1")
	finished, workflowErr := restart.TaskWorkflow(t.Context(), "TASK-1")
	writes, readErr := os.ReadFile(effects)
	if err != nil || workflowErr != nil || finished.Current != "done" || !slices.Equal(result.Merged, []string{"a"}) ||
		readErr != nil || string(writes) != "POST\n" || len(leasePushes(g)) != 0 {
		t.Fatalf("pinned delivery: result=%+v err=%v workflow=%+v workflowErr=%v effects=%q readErr=%v",
			result, err, finished, workflowErr, writes, readErr)
	}
}

func TestObserveTaskWait_PinnedTrainAfterRestart(t *testing.T) {
	for _, tc := range []struct {
		state     string
		wantPhase domain.WorkflowPhase
		want      WaitObservation
	}{
		{"closed", "review", WaitObservation{}}, // !8 delivered instead of !7
		{"merged", "done", WaitObservation{Ready: true, Key: "F"}},
	} {
		t.Run(tc.state, func(t *testing.T) {
			restart, g, _ := restartedTrainReview(t)
			t.Setenv("TRAIN_STATE", tc.state)
			if tc.state == "merged" {
				t.Setenv("TRAIN_REPLACEMENT", "")
			}
			services, err := restart.ListServices(t.Context(), "TASK-1")
			if err != nil {
				t.Fatal(err)
			}
			selected := domain.Task{ID: "TASK-1", Phase: "feature", Services: services}
			g.fetchFn = func(string) error { t.Fatal("observer fetched"); return nil }
			got, err := restart.(WaitObserver).ObserveTaskWait(t.Context(), selected, "review")
			if err != nil || got != tc.want {
				t.Errorf("observe = %+v, %v; want %+v", got, err, tc.want)
			}
			g.fetchFn = nil
			if err := restart.AdvanceTask(t.Context(), selected.ID, "", nil); err != nil {
				t.Fatal(err)
			}
			workflow, err := restart.TaskWorkflow(t.Context(), selected.ID)
			if err != nil || workflow.Current != tc.wantPhase {
				t.Fatalf("after F: state = %q, %v; want %q", workflow.Current, err, tc.wantPhase)
			}
		})
	}
}
