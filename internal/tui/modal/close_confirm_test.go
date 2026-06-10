package modal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/task"
)

func TestCloseTaskConfirmModal_ImplementsModal(t *testing.T) {
	var _ Modal = NewCloseTaskConfirmModal(task.ClosePlan{}, 80, 24)
}

func TestCloseTaskConfirmModal_ViewShowsPlanDetails(t *testing.T) {
	plan := task.ClosePlan{
		TaskID:     "IN-777",
		BranchType: gitflow.BranchTypeFeature,
		Warnings:   []string{"warn-1"},
		Services: []task.ServiceClosePlan{{
			ServiceName:    "api",
			SourceBranch:   "feature/IN-777",
			TargetBranches: []string{"develop", "main"},
			CloseStrategy:  gitflow.CloseStrategyDirectMerge,
			TagPlan:        &task.TagPlan{TagName: "v1.2.3", Version: "1.2.3"},
			ForgePlan:      &task.ReviewRequestPlan{TargetBranch: "develop"},
			PipelinePlan:   &task.PipelinePlan{Branch: "main"},
		}},
	}

	m := NewCloseTaskConfirmModal(plan, 120, 40)
	view := stripAnsi(m.View())
	for _, want := range []string{
		"Close task IN-777",
		"Branch type: feature",
		"api",
		"feature/IN-777 → develop, main",
		"close strategy: direct_merge",
		"tag proposal: v1.2.3 (1.2.3)",
		"forge action: create review request to develop",
		"pipeline trigger: main",
		"Warnings:",
		"warn-1",
		"Tag version:",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q: %s", want, view)
		}
	}
}

func TestCloseTaskConfirmModal_EnterSubmitsTaskIDAndTagVersion(t *testing.T) {
	m := NewCloseTaskConfirmModal(task.ClosePlan{
		TaskID: "IN-778",
		Services: []task.ServiceClosePlan{{
			ServiceName: "api",
			TagPlan:     &task.TagPlan{Version: "1.0.0"},
		}},
	}, 100, 40)

	modal, _ := m.Update(sendKey("2"))
	m = modal.(*CloseTaskConfirmModal)

	_, cmd := m.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter must return submit cmd")
	}
	msg := execCmd(cmd)
	sub, ok := msg.(SubmitCloseTaskMsg)
	if !ok {
		t.Fatalf("expected SubmitCloseTaskMsg, got %T", msg)
	}
	if sub.TaskID != "IN-778" {
		t.Fatalf("task id mismatch: %q", sub.TaskID)
	}
	if sub.TagVersion != "1.0.02" {
		t.Fatalf("tag version mismatch: %q", sub.TagVersion)
	}
}

func TestCloseTaskConfirmModal_EscCloses(t *testing.T) {
	m := NewCloseTaskConfirmModal(task.ClosePlan{TaskID: "IN-779"}, 80, 24)
	_, cmd := m.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("esc must return close cmd")
	}
	if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", execCmd(cmd))
	}
}
