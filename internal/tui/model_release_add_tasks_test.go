package tui

import (
	"slices"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
)

func addTasksModel(t *testing.T, mgr *mockManager, release domain.Release) Model {
	t.Helper()
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m.tasks = []domain.Task{{ID: "APP-1", Phase: "feature"}, {ID: "APP-2", Phase: "feature"}, {ID: "APP-3", Phase: "feature", ParentID: "APP-2"}}
	m.setFocus(FocusReleases)
	m.releasesPanel.SetReleases([]domain.Release{release})
	return m
}

func runModalKey(t *testing.T, m Model, key tea.Msg) (Model, tea.Msg) {
	t.Helper()
	updated, cmd := m.Update(key)
	m = updated.(Model)
	if cmd == nil {
		return m, nil
	}
	return m, cmd()
}

func TestAddReleaseTasks_SelectPlanConfirm_ExecutesReviewedPlan(t *testing.T) {
	prepared := time.Now()
	plan := task.ReleaseTaskAddPlan{ReleaseID: "rel-1", TaskIDs: []string{"APP-2"}, Services: []task.ReleaseTaskAddService{{
		ServiceName: "api", ReleaseBranch: "release/1.0.0", ReleaseTipSHA: "aaaa1111",
		Branches: []task.ReleaseTaskAddBranch{{TaskID: "APP-2", Branch: "feature/APP-2", HeadSHA: "bbbb2222"}},
	}}}
	mgr := &mockManager{planTaskAddResult: plan}
	m := addTasksModel(t, mgr, domain.Release{ID: "rel-1", Status: domain.ReleaseStatusPrepared, PreparedAt: &prepared, TaskIDs: []string{"APP-1"}})

	updated, _ := m.Update(sendKey("A"))
	m = updated.(Model)
	if _, ok := m.modal.(*modal.AddReleaseTasksDialog); !ok {
		t.Fatalf("A opened %T, want AddReleaseTasksDialog", m.modal)
	}
	m, _ = runModalKey(t, m, sendKey(" "))
	m, submit := runModalKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if _, ok := submit.(modal.SubmitAddReleaseTasksMsg); !ok {
		t.Fatalf("enter emitted %T, want SubmitAddReleaseTasksMsg", submit)
	}
	updated, cmd := m.Update(submit)
	m = updated.(Model)
	ready := findMsgInBatch(t, cmd)
	if !slices.Equal(mgr.planTaskAddTaskIDs, []string{"APP-2"}) {
		t.Fatalf("planned tasks = %v, want only APP-2 (APP-1 is in the release, APP-3 is a child)", mgr.planTaskAddTaskIDs)
	}
	updated, _ = m.Update(ready)
	m = updated.(Model)
	if dlg, ok := m.modal.(*modal.AddReleaseTasksDialog); !ok || dlg.Plan() == nil {
		t.Fatalf("plan result left modal %T without a plan", m.modal)
	}

	m, confirm := runModalKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if _, ok := confirm.(modal.ConfirmAddReleaseTasksMsg); !ok {
		t.Fatalf("enter on a ready plan emitted %T, want ConfirmAddReleaseTasksMsg", confirm)
	}
	updated, cmd = m.Update(confirm)
	m = updated.(Model)
	if m.modal != nil || !m.opRunning {
		t.Fatalf("after confirm modal=%T opRunning=%v, want closed modal and running op", m.modal, m.opRunning)
	}
	if done, ok := findMsgInBatch(t, cmd).(ReleaseActionDoneMsg); !ok || done.Err != nil {
		t.Fatalf("add tasks finished with %+v", done)
	}
	if len(mgr.addReleaseTasksPlans) != 1 || !slices.Equal(mgr.addReleaseTasksPlans[0].TaskIDs, []string{"APP-2"}) || mgr.addReleaseTasksPlans[0].Services[0].ReleaseTipSHA != "aaaa1111" {
		t.Fatalf("AddReleaseTasks plans = %+v, want the reviewed plan once", mgr.addReleaseTasksPlans)
	}
}

func TestAddReleaseTasks_BlockedPlanCannotBeConfirmed(t *testing.T) {
	prepared := time.Now()
	mgr := &mockManager{planTaskAddResult: task.ReleaseTaskAddPlan{ReleaseID: "rel-1", TaskIDs: []string{"APP-2"}, Blockers: []string{"api/APP-2: conflict"}, Services: []task.ReleaseTaskAddService{{ServiceName: "api"}}}}
	m := addTasksModel(t, mgr, domain.Release{ID: "rel-1", Status: domain.ReleaseStatusPrepared, PreparedAt: &prepared})
	updated, _ := m.Update(sendKey("A"))
	m = updated.(Model)
	m, _ = runModalKey(t, m, sendKey(" "))
	m, submit := runModalKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	updated, cmd := m.Update(submit)
	m = updated.(Model)
	updated, _ = m.Update(findMsgInBatch(t, cmd))
	m = updated.(Model)

	if _, confirm := runModalKey(t, m, tea.KeyMsg{Type: tea.KeyEnter}); confirm != nil {
		t.Fatalf("enter on a blocked plan emitted %T", confirm)
	}
	if len(mgr.addReleaseTasksPlans) != 0 {
		t.Fatal("blocked plan reached AddReleaseTasks")
	}
}

func TestAddReleaseTasks_UnpreparedReleaseOpensNothing(t *testing.T) {
	m := addTasksModel(t, &mockManager{}, domain.Release{ID: "rel-1", Status: domain.ReleaseStatusDraft})
	updated, _ := m.Update(sendKey("A"))
	if updated.(Model).modal != nil {
		t.Fatalf("A on an unprepared release opened %T", updated.(Model).modal)
	}
}
