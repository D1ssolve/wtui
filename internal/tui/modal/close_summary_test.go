package modal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/task"
)

func TestCloseTaskSummaryModal_ImplementsModal(t *testing.T) {
	var _ Modal = NewCloseTaskSummaryModal(domain.Task{}, task.CloseTaskResult{}, 80, 24)
}

func TestCloseTaskSummaryModal_ViewShowsStepsStatusIconsAndOverall(t *testing.T) {
	m := NewCloseTaskSummaryModal(domain.Task{ID: "IN-900", Phase: "feature"}, task.CloseTaskResult{
		TaskID: "IN-900",
		Steps: []task.CloseTaskStep{
			{Name: "api:fetch", Status: task.StepStatusOK, Message: "done"},
			{Name: "api:merge", Status: task.StepStatusSkipped, Message: "already merged"},
			{Name: "api:tag", Status: task.StepStatusFailed, Message: "boom"},
		},
		Success: false,
	}, 120, 40)

	view := stripAnsi(m.View())
	for _, want := range []string{
		"Step | Status | Message",
		"api:fetch",
		"✓ ok",
		"⊘ skipped",
		"✗ failed",
		"Overall: FAILED",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q: %s", want, view)
		}
	}
}

func TestCloseTaskSummaryModal_OverallSuccess(t *testing.T) {
	m := NewCloseTaskSummaryModal(domain.Task{}, task.CloseTaskResult{TaskID: "IN-901", Success: true}, 80, 24)
	view := stripAnsi(m.View())
	if !strings.Contains(view, "Overall: SUCCESS") {
		t.Fatalf("expected success summary, got: %s", view)
	}
}

func TestCloseTaskSummaryModal_EnterOrEscClose(t *testing.T) {
	m := NewCloseTaskSummaryModal(domain.Task{}, task.CloseTaskResult{}, 80, 24)

	_, cmd := m.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter must return close cmd")
	}
	if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg on enter, got %T", execCmd(cmd))
	}

	_, cmd = m.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("esc must return close cmd")
	}
	if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg on esc, got %T", execCmd(cmd))
	}
}

func TestCloseTaskSummaryModal_TitleByPhaseAndVersion(t *testing.T) {
	m := NewCloseTaskSummaryModal(
		domain.Task{ID: "ZA-553-release", Phase: "release", Version: "1.2.0"},
		task.CloseTaskResult{TaskID: "ZA-553-release", Success: true},
		80,
		24,
	)
	if got := m.Title(); got != "Close Release Task: ZA-553-release (v1.2.0)" {
		t.Fatalf("Title() = %q", got)
	}
}
