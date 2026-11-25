package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	tea "github.com/charmbracelet/bubbletea"
)

func TestWaitRefresh_ObservesSelectedReleaseWithoutAdvancing(t *testing.T) {
	mgr := &waitRefreshManager{t: t, releaseObservation: task.WaitObservation{Ready: true, Key: "M"}}
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
	if m.modal != nil || mgr.reads != 0 || strings.Contains(m.outputPanel.RecentText(5), "press F") || timer == nil {
		t.Fatalf("a command dispatched after modal invalidation must not read, reads=%d", mgr.reads)
	}
	m.opRunning = true
	model, _ = m.Update(modal.CloseModalMsg{})
	model, check = model.Update(waitRefreshTickMsg(2))
	if check != nil || mgr.reads != 0 {
		t.Fatal("busy tick scheduled an observation")
	}
}

func TestWaitRefresh_ReportsReadinessWithoutSuggestingAKey(t *testing.T) {
	mgr := &waitRefreshManager{t: t, releaseObservation: task.WaitObservation{Ready: true}}
	m := newTestModel(t, mgr)
	m.setFocus(FocusReleases)
	model, _ := m.Update(ReleasesLoadedMsg{Releases: []domain.Release{
		{ID: "release-qa", Status: domain.ReleaseStatusAwaitingMasterMerge, ManifestVersion: 2},
	}})
	model, check := model.Update(waitRefreshTickMsg(1))
	if check == nil {
		t.Fatal("missing release observation")
	}
	model, _ = model.Update(check())
	m = model.(Model)
	line := m.outputPanel.RecentText(1)
	if !strings.Contains(line, "wait condition met") || strings.Contains(line, "press ") {
		t.Fatalf("expected positive status with no key hint: %q", line)
	}
}

type waitRefreshManager struct {
	task.Manager       // All methods outside the permitted read set fail if invoked.
	t                  *testing.T
	reads              int
	releaseObservation task.WaitObservation
}

func (m *waitRefreshManager) ObserveReleaseWait(ctx context.Context, id, state string) (task.WaitObservation, error) {
	m.t.Helper()
	if _, bounded := ctx.Deadline(); !bounded || id != "release-qa" || state != gitflow.StateAwaitingMasterMerge {
		m.t.Fatalf("unexpected observation request for %s/%s (bounded=%v)", id, state, bounded)
	}
	m.reads++
	return m.releaseObservation, nil
}

func (m *waitRefreshManager) ObserveTaskWait(ctx context.Context, selected domain.Task, state string) (task.WaitObservation, error) {
	m.t.Helper()
	if _, bounded := ctx.Deadline(); !bounded || selected.ID != "QA-1" || state != "qa" {
		m.t.Fatalf("unexpected task observation for %s/%s (bounded=%v)", selected.ID, state, bounded)
	}
	m.reads++
	return task.WaitObservation{Ready: true, Key: "F"}, nil
}

func TestWaitRefresh_CancelsBlockedReadAndKeepsSlotUntilReturn(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  tea.Msg
	}{
		{"selection", keyMsgFor("j")},
		{"modal", keyMsgFor("?")},
		{"operation", modal.SubmitInitMsg{}},
		{"quit", keyMsgFor("q")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr := &blockedWaitRefreshManager{
				started: make(chan string, 1), canceled: make(chan struct{}), release: make(chan struct{}),
			}
			t.Cleanup(func() { close(mgr.release) })
			m := newTestModel(t, mgr)
			m.setFocus(FocusReleases)
			model, _ := m.Update(ReleasesLoadedMsg{Releases: []domain.Release{
				{ID: "release-a", Status: domain.ReleaseStatusAwaitingMasterMerge, ManifestVersion: 2},
				{ID: "release-b", Status: domain.ReleaseStatusAwaitingMasterMerge, ManifestVersion: 2},
			}})
			model, check := model.Update(waitRefreshTickMsg(1))
			if check == nil {
				t.Fatal("missing command for selected release")
			}
			deadline, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			result := make(chan tea.Msg, 1)
			go func() { result <- check() }()
			select {
			case id := <-mgr.started:
				if id != "release-a" {
					t.Fatalf("observed %q, want release-a", id)
				}
			case <-deadline.Done():
				t.Fatal("observer did not start")
			}
			model, cmd := model.Update(tc.msg)
			if tc.name == "selection" && cmd != nil {
				t.Fatal("selection rearmed before the canceled read returned")
			}
			if tc.name == "quit" {
				if cmd == nil {
					t.Fatal("quit command missing")
				}
				if _, ok := cmd().(tea.QuitMsg); !ok {
					t.Fatal("q did not quit")
				}
			}
			select {
			case <-mgr.canceled:
				t.Log("in-flight observer received cancellation")
			case <-deadline.Done():
				t.Fatal("obsolete observer was not canceled")
			}
			model, overlap := model.Update(waitRefreshTickMsg(2))
			if overlap != nil {
				t.Fatal("scheduled a second observer before the canceled read returned")
			}
			select {
			case mgr.release <- struct{}{}:
			case <-deadline.Done():
				t.Fatal("observer did not reach retirement handshake")
			}
			select {
			case msg := <-result:
				model, cmd = model.Update(msg)
			case <-deadline.Done():
				t.Fatal("canceled observer did not finish")
			}
			m = model.(Model)
			if strings.Contains(m.outputPanel.RecentText(5), "press M") {
				t.Fatal("obsolete result reached OUTPUT")
			}
			if tc.name == "selection" && (cmd == nil || m.releasesPanel.SelectedRelease().ID != "release-b") {
				t.Fatal("did not rearm for the new selection after retirement")
			}
		})
	}
}

func (m *waitRefreshManager) ReleaseEvents(r domain.Release) []task.ReleaseEvent {
	return gitFlowReleaseEvents(r)
}

type blockedWaitRefreshManager struct {
	waitRefreshManager
	started  chan string
	canceled chan struct{}
	release  chan struct{}
}

func (m *blockedWaitRefreshManager) ObserveReleaseWait(ctx context.Context, id, _ string) (task.WaitObservation, error) {
	m.started <- id
	<-ctx.Done()
	close(m.canceled)
	<-m.release
	// Even a late, positive result from a canceled adapter must not be displayed.
	return task.WaitObservation{Ready: true, Key: "M"}, nil
}

func (m *waitRefreshManager) AdvanceRelease(context.Context, string, string, chan<- string) (domain.Release, error) {
	m.t.Fatal("refresh called AdvanceRelease")
	return domain.Release{}, nil
}

func (m *waitRefreshManager) AdvanceTask(context.Context, string, string, chan<- string) error {
	m.t.Fatal("refresh called AdvanceTask")
	return nil
}
