package modal

import (
	"fmt"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/task"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestHotfixCloseModal_WaitingShowsTargetsWithoutVersion(t *testing.T) {
	p := task.ClosePlan{TaskID: "H", HotfixReview: true, Fingerprint: "confirmed", Services: []task.ServiceClosePlan{{ServiceName: "api", Reviews: []task.HotfixReview{{Target: "master", State: "merged", URL: "master-url"}, {Target: "develop", State: "missing"}}}}}
	d := NewHotfixCloseModal(p)
	view := d.View()
	for _, text := range []string{"master", "merged", "develop", "missing", "master-url"} {
		if !strings.Contains(view, text) {
			t.Fatalf("missing %s: %s", text, view)
		}
	}
	if strings.Contains(view, "Tag version:") {
		t.Fatal("tag input before merge")
	}
	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := cmd().(SubmitCloseTaskMsg)
	if msg.Fingerprint != "confirmed" || len(msg.TagVersions) != 0 {
		t.Fatalf("submit: %+v", msg)
	}
}

func TestHotfixCloseModal_ScrollsLargeTask(t *testing.T) {
	p := task.ClosePlan{TaskID: "H", HotfixReview: true}
	for i := 0; i < 30; i++ {
		p.Services = append(p.Services, task.ServiceClosePlan{ServiceName: fmt.Sprintf("service-%02d", i), Reviews: []task.HotfixReview{{Target: "master", State: "open"}}})
	}
	d := NewHotfixCloseModal(p)
	d.SetTerminalSize(80, 24)
	if h := lipgloss.Height(d.View()); h > 20 {
		t.Fatalf("modal overflows small terminal: %d lines", h)
	}
	for range 20 {
		d.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	}
	if !strings.Contains(d.View(), "service-29") {
		t.Fatal("cannot scroll to last service")
	}
}

func TestHotfixCloseModal_PreservesPerServiceVersions(t *testing.T) {
	p := task.ClosePlan{TaskID: "H", HotfixReview: true, Fingerprint: "confirmed", Services: []task.ServiceClosePlan{
		{ServiceName: "api", TagPlan: &task.TagPlan{Version: "1.2.4", TagName: "v1.2.4", SourceRef: "master-merge"}},
		{ServiceName: "web", TagPlan: &task.TagPlan{Version: "2.0.1", TagName: "v2.0.1", SourceRef: "other-merge", Locked: true}},
	}}
	d := NewHotfixCloseModal(p)
	if !strings.Contains(d.View(), "master-merge") {
		t.Fatal("missing pinned SHA")
	}
	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := cmd().(SubmitCloseTaskMsg)
	if msg.TagVersions["api"] != "1.2.4" || msg.TagVersions["web"] != "2.0.1" {
		t.Fatalf("versions lost: %+v", msg)
	}
}

func TestMergeConfirm_SelectsSecondTarget(t *testing.T) {
	d := NewMergeConfirmDialog("H", "", "api", []MergeServiceStatus{
		{ServiceName: "api", Status: "ready", Number: 1, TargetBranch: "master", HeadSHA: "source"},
		{ServiceName: "api", Status: "ready", Number: 2, TargetBranch: "develop", HeadSHA: "source"},
	})
	d.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	d.Update(tea.KeyMsg{Type: tea.KeyUp}) // A queued command must retain the confirmed target.
	msg := cmd().(ConfirmMergeMsg)
	if msg.Number != 2 || msg.TargetBranch != "develop" || msg.HeadSHA != "source" {
		t.Fatalf("wrong selection: %+v", msg)
	}
}

func TestMergeConfirm_MissingTargetCannotMergeOtherRequests(t *testing.T) {
	d := NewMergeConfirmDialog("H", "", "api", []MergeServiceStatus{{ServiceName: "api", Status: "no_mr", TargetBranch: "develop"}, {ServiceName: "api", Status: "ready", Number: 1, TargetBranch: "master", HeadSHA: "source"}})
	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("missing MR must not dispatch a merge-all command")
	}
}
