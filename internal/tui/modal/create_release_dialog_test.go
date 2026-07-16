package modal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/tui/panels"
)

func TestCreateReleaseDialog_Phase1_DisablesNonFeatureAndChildTasks(t *testing.T) {
	d := NewCreateReleaseDialog([]domain.Task{
		{ID: "FEAT-1", Phase: "feature", Services: []domain.Service{{Name: "api"}}},
		{ID: "REL-1", Phase: "release", Services: []domain.Service{{Name: "api"}}},
		{ID: "CHILD-1", Phase: "feature", ParentID: "FEAT-1", Services: []domain.Service{{Name: "api"}}},
	}, 100, 30)

	if !d.taskRows[0].selectable {
		t.Fatal("expected root feature task selectable")
	}
	if d.taskRows[1].selectable || !strings.Contains(d.taskRows[1].reason, "non-feature") {
		t.Fatalf("expected non-feature task disabled with reason, got selectable=%v reason=%q", d.taskRows[1].selectable, d.taskRows[1].reason)
	}
	if d.taskRows[2].selectable || d.taskRows[2].reason != "child task" {
		t.Fatalf("expected child task disabled, got selectable=%v reason=%q", d.taskRows[2].selectable, d.taskRows[2].reason)
	}

	view := stripAnsi(d.View())
	if !strings.Contains(view, "disabled: non-feature") {
		t.Fatalf("expected disabled reason in view, got: %q", view)
	}
}

func TestCreateReleaseDialog_Phase1_EnterRequiresSelection(t *testing.T) {
	d := NewCreateReleaseDialog([]domain.Task{{ID: "FEAT-1", Phase: "feature"}}, 100, 30)

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd != nil {
		t.Fatal("enter without selection must not submit")
	}
	if d.phase != phaseTaskSelect {
		t.Fatalf("expected stay in phaseTaskSelect, got %v", d.phase)
	}
	if d.err == "" {
		t.Fatal("expected validation error when nothing selected")
	}
}

func TestCreateReleaseDialog_PhaseFlow_EscFromPhase2ReturnsToPhase1WithSelection(t *testing.T) {
	d := NewCreateReleaseDialog([]domain.Task{
		{ID: "FEAT-1", Phase: "feature", Services: []domain.Service{{Name: "api"}}},
	}, 100, 30)

	_, _ = d.Update(sendKey(" "))
	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("phase 1 enter with selection must request versions")
	}
	msg := execCmd(cmd)
	req, ok := msg.(RequestReleaseVersionsMsg)
	if !ok {
		t.Fatalf("expected RequestReleaseVersionsMsg, got %T", msg)
	}
	if len(req.TaskIDs) != 1 || req.TaskIDs[0] != "FEAT-1" {
		t.Fatalf("unexpected requested task IDs: %+v", req.TaskIDs)
	}

	if d.phase != phaseVersionInput {
		t.Fatalf("expected phaseVersionInput, got %v", d.phase)
	}

	_, cmd = d.Update(sendSpecialKey(tea.KeyEsc))
	if cmd != nil {
		t.Fatal("esc from phase2 must not close modal")
	}
	if d.phase != phaseTaskSelect {
		t.Fatalf("expected return to phaseTaskSelect, got %v", d.phase)
	}
	if !d.taskRows[0].selected {
		t.Fatal("expected selected tasks preserved when returning to phase1")
	}
}

func TestCreateReleaseDialog_EscFromPhase1_Closes(t *testing.T) {
	d := NewCreateReleaseDialog([]domain.Task{{ID: "FEAT-1", Phase: "feature"}}, 100, 30)

	_, cmd := d.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("esc from phase1 must close")
	}

	msg := execCmd(cmd)
	if _, ok := msg.(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", msg)
	}
}

func TestCreateReleaseDialog_Phase2_ShowsUnionAndValidatesSemver(t *testing.T) {
	d := NewCreateReleaseDialog([]domain.Task{
		{ID: "FEAT-1", Phase: "feature", Services: []domain.Service{{Name: "api"}, {Name: "worker"}}},
		{ID: "FEAT-2", Phase: "feature", Services: []domain.Service{{Name: "api"}, {Name: "web"}}},
	}, 100, 30)

	_, _ = d.Update(sendKey(" "))
	_, _ = d.Update(sendSpecialKey(tea.KeyDown))
	_, _ = d.Update(sendKey(" "))
	_, _ = d.Update(sendSpecialKey(tea.KeyEnter))

	if len(d.inputRows) != 3 {
		t.Fatalf("expected union of 3 services, got %d", len(d.inputRows))
	}

	d.loadingVersions = false
	for i := range d.inputRows {
		d.inputRows[i].value = "invalid"
	}

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd != nil {
		t.Fatal("invalid semver must block submit")
	}
	for _, row := range d.inputRows {
		if row.err == "" {
			t.Fatalf("expected inline error for %s", row.serviceName)
		}
	}
}

func TestCreateReleaseDialog_ReleaseVersionsLoaded_PrefillsInputs(t *testing.T) {
	d := NewCreateReleaseDialog([]domain.Task{
		{ID: "FEAT-1", Phase: "feature", Services: []domain.Service{{Name: "api"}, {Name: "worker"}}},
	}, 100, 30)

	_, _ = d.Update(sendKey(" "))
	_, _ = d.Update(sendSpecialKey(tea.KeyEnter))

	_, _ = d.Update(panels.ReleaseVersionsLoadedMsg{Versions: map[string]string{
		"api":    "1.2.3",
		"worker": "2.0.0",
	}})

	if d.loadingVersions {
		t.Fatal("expected loading false after versions loaded")
	}
	if got := d.inputRows[0].value; got == "…" || got == "" {
		t.Fatalf("expected first input prefilled, got %q", got)
	}
}

func TestCreateReleaseDialog_Submit_EmitsSubmitCreateReleaseMsg(t *testing.T) {
	d := NewCreateReleaseDialog([]domain.Task{
		{ID: "FEAT-1", Phase: "feature", Services: []domain.Service{{Name: "api"}}},
		{ID: "FEAT-2", Phase: "feature", Services: []domain.Service{{Name: "worker"}}},
	}, 100, 30)

	_, _ = d.Update(sendKey(" "))
	_, _ = d.Update(sendSpecialKey(tea.KeyDown))
	_, _ = d.Update(sendKey(" "))
	_, _ = d.Update(sendSpecialKey(tea.KeyEnter))

	_, _ = d.Update(panels.ReleaseVersionsLoadedMsg{Versions: map[string]string{
		"api":    "1.2.3",
		"worker": "2.1.0",
	}})

	_, cmd := d.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("valid submit must emit cmd")
	}

	msg := execCmd(cmd)
	sub, ok := msg.(SubmitCreateReleaseMsg)
	if !ok {
		t.Fatalf("expected SubmitCreateReleaseMsg, got %T", msg)
	}

	if len(sub.TaskIDs) != 2 || sub.TaskIDs[0] != "FEAT-1" || sub.TaskIDs[1] != "FEAT-2" {
		t.Fatalf("unexpected task ids: %+v", sub.TaskIDs)
	}
	if sub.Versions["api"] != "1.2.3" || sub.Versions["worker"] != "2.1.0" {
		t.Fatalf("unexpected versions payload: %+v", sub.Versions)
	}
}
