package modal

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/Masterminds/semver/v3"
)

func TestPruneConfirmModal_SpaceTogglesOnlyPrunableAndEnterSubmits(t *testing.T) {
	candidates := []domain.PruneCandidate{
		{
			TaskID:   "IN-1001",
			Prunable: true,
			Services: []domain.ServicePrune{{ServiceName: "api", IsMerged: true}},
		},
		{
			TaskID:   "IN-1002",
			Prunable: false,
			Services: []domain.ServicePrune{{ServiceName: "worker", IsMerged: false}},
		},
	}

	m := NewPruneConfirmModal(candidates, 100, 40)

	if !m.rows[0].selected {
		t.Fatal("prunable row should be selected by default")
	}

	modal, _ := m.Update(sendKey(" "))
	m = modal.(*PruneConfirmModal)
	if m.rows[0].selected {
		t.Fatal("space on prunable row should toggle to unselected")
	}

	modal, _ = m.Update(sendKey("j"))
	m = modal.(*PruneConfirmModal)
	if m.selectedIndex != 1 {
		t.Fatalf("expected selected index 1, got %d", m.selectedIndex)
	}

	modal, _ = m.Update(sendKey(" "))
	m = modal.(*PruneConfirmModal)
	if m.rows[1].selected {
		t.Fatal("space on blocked row must not toggle selection")
	}

	_, cmd := m.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter must return submit command")
	}
	msg := execCmd(cmd)
	sub, ok := msg.(SubmitPruneMsg)
	if !ok {
		t.Fatalf("expected SubmitPruneMsg, got %T", msg)
	}
	if len(sub.SelectedTaskIDs) != 0 {
		t.Fatalf("expected zero selected tasks after toggle off, got %v", sub.SelectedTaskIDs)
	}
}

func TestNewModals_ImplementModalInterface(t *testing.T) {
	var _ Modal = NewPruneConfirmModal(nil, 80, 24)
	var _ Modal = NewTagBrowserModal(nil, 80, 24)
	var _ Modal = NewForgeMenuModal("svc", forge.ForgeProviderGitLab, 80, 24)
}

func TestPruneConfirmModal_ViewShowsBlockedReasonAndEscCloses(t *testing.T) {
	m := NewPruneConfirmModal([]domain.PruneCandidate{{
		TaskID:   "IN-1003",
		Prunable: false,
		Services: []domain.ServicePrune{{ServiceName: "svc", IsMerged: false}},
	}}, 100, 40)

	view := stripAnsi(m.View())
	for _, want := range []string{"IN-1003", "blocked", "not merged", "[-]"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q: %s", want, view)
		}
	}

	_, cmd := m.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("esc must return close command")
	}
	if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", execCmd(cmd))
	}
}

func TestTagBrowserModal_SortsSemverDescWithNonSemverAtBottomAndAnnotatedIndicator(t *testing.T) {
	v1, err := semver.NewVersion("v1.2.0")
	if err != nil {
		t.Fatalf("failed parse v1.2.0: %v", err)
	}
	v2, err := semver.NewVersion("v2.0.0")
	if err != nil {
		t.Fatalf("failed parse v2.0.0: %v", err)
	}

	tags := []domain.TagInfo{
		{Name: "alpha", Ref: "aaaa", Message: "alpha msg", IsAnnotated: false},
		{Name: "v1.2.0", Ref: "1111", Message: "v1 msg", IsAnnotated: true, IsSemver: true, Version: v1},
		{Name: "v2.0.0", Ref: "2222", Message: "v2 msg", IsAnnotated: false, IsSemver: true, Version: v2},
		{Name: "zeta", Ref: "zzzz", Message: "zeta msg", IsAnnotated: true},
	}

	m := NewTagBrowserModal(tags, 120, 40)

	gotOrder := []string{m.tags[0].Name, m.tags[1].Name, m.tags[2].Name, m.tags[3].Name}
	wantOrder := []string{"v2.0.0", "v1.2.0", "alpha", "zeta"}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Fatalf("unexpected tag order: got %v want %v", gotOrder, wantOrder)
	}

	view := stripAnsi(m.View())
	if !strings.Contains(view, "* v1.2.0") {
		t.Fatalf("view must include annotated indicator for annotated tags, got: %s", view)
	}
}

func TestTagBrowserModal_EnterOrEscDismisses(t *testing.T) {
	m := NewTagBrowserModal(nil, 80, 24)

	_, cmd := m.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter must return close command")
	}
	if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg on enter, got %T", execCmd(cmd))
	}

	_, cmd = m.Update(sendSpecialKey(tea.KeyEsc))
	if cmd == nil {
		t.Fatal("esc must return close command")
	}
	if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg on esc, got %T", execCmd(cmd))
	}
}

func TestForgeMenuModal_AvailableShowsActionsAndEnterTriggersMessages(t *testing.T) {
	m := NewForgeMenuModal("api", forge.ForgeProviderGitLab, 100, 40)
	m.SetTaskID("IN-4242")

	view := stripAnsi(m.View())
	for _, want := range []string{"Create MR/PR", "View Pipeline Status", "List Issues"} {
		if !strings.Contains(view, want) {
			t.Fatalf("forge menu missing %q in view: %s", want, view)
		}
	}

	_, cmd := m.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter should emit create MR msg on first option")
	}
	if msg, ok := execCmd(cmd).(ForgeCreateMRMsg); !ok {
		t.Fatalf("expected ForgeCreateMRMsg, got %T", execCmd(cmd))
	} else if msg.TaskID != "IN-4242" || msg.ServiceName != "api" {
		t.Fatalf("unexpected create msg payload: %+v", msg)
	}

	modal, _ := m.Update(sendKey("j"))
	m = modal.(*ForgeMenuModal)
	_, cmd = m.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter should emit pipeline msg on second option")
	}
	if msg, ok := execCmd(cmd).(ForgePipelineStatusMsg); !ok {
		t.Fatalf("expected ForgePipelineStatusMsg, got %T", execCmd(cmd))
	} else if msg.TaskID != "IN-4242" || msg.ServiceName != "api" {
		t.Fatalf("unexpected pipeline msg payload: %+v", msg)
	}

	modal, _ = m.Update(sendKey("j"))
	m = modal.(*ForgeMenuModal)
	_, cmd = m.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter should emit list issues msg on third option")
	}
	if msg, ok := execCmd(cmd).(ForgeListIssuesMsg); !ok {
		t.Fatalf("expected ForgeListIssuesMsg, got %T", execCmd(cmd))
	} else if msg.TaskID != "IN-4242" || msg.ServiceName != "api" {
		t.Fatalf("unexpected list issues msg payload: %+v", msg)
	}
}

func TestForgeMenuModal_UnavailableShowsMessageAndEnterDismisses(t *testing.T) {
	m := NewForgeMenuModal("api", forge.ForgeProviderUnknown, 100, 40)

	view := stripAnsi(m.View())
	if !strings.Contains(view, "No forge CLI available") {
		t.Fatalf("unavailable forge message missing: %s", view)
	}

	_, cmd := m.Update(sendSpecialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter must dismiss unavailable modal")
	}
	if _, ok := execCmd(cmd).(CloseModalMsg); !ok {
		t.Fatalf("expected CloseModalMsg, got %T", execCmd(cmd))
	}
}
