package tui

import (
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/D1ssolve/wtui/internal/tui/modal"
	tea "github.com/charmbracelet/bubbletea"
)

func TestHotfixModel_SubmitsConfirmedPlanAndShowsWaiting(t *testing.T) {
	mgr := &cmdManager{closeResult: task.CloseTaskResult{TaskID: "H", Waiting: true}}
	m := newTestModel(t, mgr)
	p := task.ClosePlan{TaskID: "H", HotfixReview: true, Fingerprint: "approved", Services: []task.ServiceClosePlan{{ServiceName: "api", TagPlan: &task.TagPlan{Version: "1.2.4", SourceRef: "merge"}}}}
	updated, _ := m.Update(ClosePlanReadyMsg{Plan: p})
	m = updated.(Model)
	d, ok := m.modal.(*modal.HotfixCloseModal)
	if !ok {
		t.Fatalf("wrong modal: %T", m.modal)
	}
	_, submit := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, cmd := m.Update(submit())
	m = updated.(Model)
	done, ok := drainCloseCommand(cmd)
	if !ok {
		t.Fatal("missing close completion")
	}
	if mgr.closeParams.Fingerprint != "approved" || mgr.closeParams.TagVersions["api"] != "1.2.4" {
		t.Fatalf("lost confirmation: %+v", mgr.closeParams)
	}
	updated, _ = m.Update(done)
	m = updated.(Model)
	if !strings.Contains(m.modal.View(), "WAITING FOR MERGE") || strings.Contains(m.modal.View(), "Overall: FAILED") {
		t.Fatalf("wrong waiting summary: %s", m.modal.View())
	}
}

func TestHotfixModel_ServiceMergeKeepsAllTargetsAndForwardsSelection(t *testing.T) {
	mgr := &cmdManager{}
	m := sendWindowSize(newTestModel(t, mgr), 120, 40)
	m.setFocus(FocusServices)
	m.servicesPanel.SetServices("H", []domain.Service{{Name: "api"}})
	m.mergeInspection = &mergeInspectionRequest{generation: 1, focus: FocusServices, taskID: "H", serviceName: "api"}
	rows := []task.ServiceMergeInspection{
		{ServiceName: "api", Status: "merged", MR: forge.MRReadiness{Number: 1, TargetBranch: "master", HeadSHA: "source"}},
		{ServiceName: "api", Status: "ready", MR: forge.MRReadiness{Number: 2, TargetBranch: "develop", HeadSHA: "source"}},
	}
	updated, _ := m.Update(TaskMergeInspectionMsg{TaskID: "H", Generation: 1, Inspection: task.TaskMergeInspection{TaskID: "H", Services: rows}})
	m = updated.(Model)
	d, ok := m.modal.(*modal.MergeConfirmDialog)
	if !ok {
		t.Fatalf("modal=%T", m.modal)
	}
	if !strings.Contains(d.View(), "develop") {
		t.Fatal("second target discarded")
	}
	d.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, confirm := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, cmd := m.Update(confirm())
	if cmd == nil {
		t.Fatal("no merge command")
	}
	for _, c := range cmd().(tea.BatchMsg) {
		if c != nil {
			c()
		}
	}
	if len(mgr.mergeSelection) != 1 || mgr.mergeSelection[0].Number != 2 || mgr.mergeSelection[0].HeadSHA != "source" {
		t.Fatalf("lost selection: %+v", mgr.mergeSelection)
	}
}
