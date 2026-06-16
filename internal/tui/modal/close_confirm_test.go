package modal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/task"
)

func TestCloseTaskConfirmModal_ImplementsModal(t *testing.T) {
	var _ Modal = NewCloseTaskConfirmModal(domain.Task{}, task.ClosePlan{}, 80, 24)
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

	m := NewCloseTaskConfirmModal(domain.Task{ID: "IN-777", Phase: "feature"}, plan, 120, 40)
	view := stripAnsi(m.View())
	for _, want := range []string{
		"Close Feature Task: IN-777",
		"Branch type: feature",
		"Service | Source Branch | Targets | Close Strategy | Tag | Pipeline | Forge/MR",
		"api | feature/IN-777 | develop, main | direct_merge | v1.2.3 (1.2.3) | main | develop",
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
	m := NewCloseTaskConfirmModal(domain.Task{}, task.ClosePlan{
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
	m := NewCloseTaskConfirmModal(domain.Task{}, task.ClosePlan{TaskID: "IN-779"}, 80, 24)
	_, cmd := m.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("esc must return close cmd")
	}
	if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", execCmd(cmd))
	}
}

func TestCloseTaskConfirmModal_DynamicColumnsHideOptionalWhenNoPlans(t *testing.T) {
	m := NewCloseTaskConfirmModal(domain.Task{ID: "IN-780"}, task.ClosePlan{
		TaskID: "IN-780",
		Services: []task.ServiceClosePlan{{
			ServiceName:    "api",
			SourceBranch:   "feature/IN-780",
			TargetBranches: []string{"develop"},
			CloseStrategy:  gitflow.CloseStrategyDirectMerge,
		}},
	}, 100, 40)

	view := stripAnsi(m.View())
	if !strings.Contains(view, "Service | Source Branch | Targets | Close Strategy") {
		t.Fatalf("expected base columns, got: %s", view)
	}
	for _, unexpected := range []string{" | Tag", " | Pipeline", " | Forge/MR"} {
		if strings.Contains(view, unexpected) {
			t.Fatalf("unexpected optional column %q in view: %s", unexpected, view)
		}
	}
}

func TestCloseTaskConfirmModal_TitleByPhaseAndVersion(t *testing.T) {
	tests := []struct {
		name string
		task domain.Task
		want string
	}{
		{name: "feature", task: domain.Task{ID: "ZA-553", Phase: "feature"}, want: "Close Feature Task: ZA-553"},
		{name: "release", task: domain.Task{ID: "ZA-553-release", Phase: "release", Version: "1.2.0"}, want: "Close Release Task: ZA-553-release (v1.2.0)"},
		{name: "hotfix", task: domain.Task{ID: "ZA-553-hotfix", Phase: "hotfix", Version: "1.2.1"}, want: "Close Hotfix Task: ZA-553-hotfix (v1.2.1)"},
		{name: "unknown", task: domain.Task{ID: "ZA-553"}, want: "Close Task: ZA-553"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := NewCloseTaskConfirmModal(tc.task, task.ClosePlan{TaskID: tc.task.ID}, 80, 24)
			if got := m.Title(); got != tc.want {
				t.Fatalf("Title() = %q, want %q", got, tc.want)
			}
		})
	}
}
