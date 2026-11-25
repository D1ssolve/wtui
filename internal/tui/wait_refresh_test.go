package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
)

func TestWaitRefresh_ObservesSelectedReleaseWithoutAdvancing(t *testing.T) {
	mgr := &waitRefreshManager{t: t}
	m := newTestModel(t, mgr)
	m.setFocus(FocusReleases)
	model, timer := m.Update(ReleasesLoadedMsg{Releases: []domain.Release{
		{ID: "release-qa", Status: domain.ReleaseStatusAwaitingMasterMerge, ManifestVersion: 2},
	}})
	if timer == nil {
		t.Fatal("selecting a waiting release did not schedule a refresh")
	}
	model, check := model.Update(waitRefreshTickMsg(1))
	if check == nil || mgr.reads != 0 {
		t.Fatal("a refresh tick must return an asynchronous status read")
	}
	model, duplicate := model.Update(waitRefreshTickMsg(1))
	if duplicate != nil {
		t.Fatal("duplicate tick scheduled concurrent work")
	}
	model, timer = model.Update(check())
	m = model.(Model)
	output := m.outputPanel.RecentText(5)
	if mgr.reads != 1 || !strings.Contains(output, "release-qa") || !strings.Contains(output, "press M") {
		t.Fatalf("expected one read and manual reconciliation guidance, reads=%d output=%q", mgr.reads, output)
	}
	if timer == nil {
		t.Fatal("did not schedule another check while the workflow still waits")
	}
	model, check = model.Update(waitRefreshTickMsg(2))
	if check == nil {
		t.Fatal("second simulated tick did not schedule a read")
	}
	model, _ = model.Update(check())
	m = model.(Model)
	if got := m.outputPanel.RecentText(5); got != output {
		t.Fatalf("unchanged observation repeated output: %q", got)
	}
}

func TestWaitRefresh_DiscardsTaskResultAcrossModalAndSkipsBusyTick(t *testing.T) {
	mgr := &waitRefreshManager{t: t}
	m := newTestModel(t, mgr)
	m.flow = &gitflow.ResolvedGitFlow{BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
		"feature": {Machine: &gitflow.Machine{States: []gitflow.State{{ID: "qa", Kind: gitflow.StateWait, Wait: gitflow.WaitCIGreen}}}},
	}}
	selected := domain.Task{ID: "QA-1", Phase: "feature", Services: []domain.Service{{Name: "api"}}}
	m.tasksPanel.SetTasks([]domain.Task{selected})
	m.servicesPanel.SetServices(selected.ID, selected.Services)
	model, _ := m.Update(TaskWorkflowLoadedMsg{TaskID: selected.ID, Workflow: domain.WorkflowSummary{Gated: true, Current: "qa"}})
	model, check := model.Update(waitRefreshTickMsg(1))
	if check == nil {
		t.Fatal("selected task CI wait did not schedule a read")
	}
	model, _ = model.Update(keyMsgFor("?"))
	model, _ = model.Update(modal.CloseModalMsg{})
	model, timer := model.Update(check())
	m = model.(Model)
	if m.modal != nil || mgr.reads != 1 || strings.Contains(m.outputPanel.RecentText(5), "press F") || timer == nil {
		t.Fatal("a task wait result crossing a modal must be discarded, with polling resumed")
	}
	m.opRunning = true
	model, _ = m.Update(modal.CloseModalMsg{})
	model, check = model.Update(waitRefreshTickMsg(2))
	if check != nil || mgr.reads != 1 {
		t.Fatal("busy tick scheduled an observation")
	}
}

type waitRefreshManager struct {
	task.Manager // All methods outside the permitted read set fail if invoked.
	t            *testing.T
	reads        int
}

func (m *waitRefreshManager) ObserveReleaseWait(ctx context.Context, id, state string) (string, error) {
	m.t.Helper()
	if _, bounded := ctx.Deadline(); !bounded || id != "release-qa" || state != gitflow.StateAwaitingMasterMerge {
		m.t.Fatalf("unexpected observation request for %s/%s (bounded=%v)", id, state, bounded)
	}
	m.reads++
	return "M", nil
}

func (m *waitRefreshManager) ObserveTaskWait(ctx context.Context, selected domain.Task, state string) (string, error) {
	m.t.Helper()
	if _, bounded := ctx.Deadline(); !bounded || selected.ID != "QA-1" || state != "qa" {
		m.t.Fatalf("unexpected task observation for %s/%s (bounded=%v)", selected.ID, state, bounded)
	}
	m.reads++
	return "F", nil
}

func (m *waitRefreshManager) AdvanceRelease(context.Context, string, string, chan<- string) (domain.Release, error) {
	m.t.Fatal("refresh called AdvanceRelease")
	return domain.Release{}, nil
}

func (m *waitRefreshManager) AdvanceTask(context.Context, string, string, chan<- string) error {
	m.t.Fatal("refresh called AdvanceTask")
	return nil
}
