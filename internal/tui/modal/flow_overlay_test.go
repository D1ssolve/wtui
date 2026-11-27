package modal

import (
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/gitflow"
	tea "github.com/charmbracelet/bubbletea"
)

func TestFlowOverlay_MarksPositionAndListsTransitions(t *testing.T) {
	flow, err := gitflow.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	o := NewFlowOverlay(flow, FlowPosition{TaskType: "hotfix", ReleaseState: "awaiting_master_merge", Visited: []string{"preparing", "prepared", "promoting"}})
	view := stripAnsi(o.View())
	for _, want := range []string{
		"▶ hotfix", "[C]", "✓ preparing", "▶ awaiting_master_merge", "hotfix/*",
		"[F] create master MRs ─▶ promoting", "[M] reconcile (merge ready reviews)",
		"when review merged ─▶ master_merged", "○ finishing",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("overlay missing %q:\n%s", want, view)
		}
	}
}

func TestFlowOverlay_ScrollsWithinHeight(t *testing.T) {
	flow, _ := gitflow.Resolve(nil)
	o := NewFlowOverlay(flow, FlowPosition{})
	o.SetTerminalSize(80, 14)
	if !strings.Contains(stripAnsi(o.View()), "scroll 1-6") {
		t.Fatalf("expected a scroll hint:\n%s", stripAnsi(o.View()))
	}
	o.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if !strings.Contains(stripAnsi(o.View()), "released") {
		t.Fatalf("G must reach the last state:\n%s", stripAnsi(o.View()))
	}
	if _, cmd := o.Update(tea.KeyMsg{Type: tea.KeyEsc}); cmd == nil {
		t.Fatal("esc must close the overlay")
	}
}

func TestFlowOverlay_PresetTabsDrawEachPreset(t *testing.T) {
	flow, err := gitflow.ParseWorkflowYAML("workflow: {extends: git-flow, branches: {production: prod}}")
	if err != nil {
		t.Fatal(err)
	}
	o := NewFlowOverlay(flow, FlowPosition{TaskType: "feature"})
	press := func(k tea.KeyMsg) string { o.Update(k); return stripAnsi(o.View()) }
	right := tea.KeyMsg{Type: tea.KeyRight}
	if view := stripAnsi(o.View()); !strings.Contains(view, "production prod") || !strings.Contains(view, "▶ feature") {
		t.Fatalf("current tab must show the configured flow and position:\n%s", view)
	}
	want := map[string]string{"git-flow": "production master · integration develop", "github-flow": "production main · integration main", "trunk-based": "production main · integration main"}
	for _, name := range gitflow.PresetNames() {
		view := press(right)
		if !strings.Contains(view, "preset "+name) || !strings.Contains(view, want[name]) || strings.Contains(view, "▶ feature") {
			t.Fatalf("tab %s must draw the preset without the selection:\n%s", name, view)
		}
	}
	if view := press(right); !strings.Contains(view, "production prod") {
		t.Fatalf("right after the last preset must wrap to the current flow:\n%s", view)
	}
	if view := press(tea.KeyMsg{Type: tea.KeyLeft}); !strings.Contains(view, "preset trunk-based") {
		t.Fatalf("left from current must wrap to the last preset:\n%s", view)
	}
}

func TestFlowOverlay_FramesWideEnoughForChains(t *testing.T) {
	flow, err := gitflow.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	o := NewFlowOverlay(flow, FlowPosition{})
	o.SetTerminalSize(100, 32)
	if view := stripAnsi(o.OverlayView()); !strings.Contains(view, "feature/* from develop ─[C]─▶ review into develop (merge_commit) ─▶ done") {
		t.Fatalf("a 100-column terminal must fit the feature chain on one line:\n%s", view)
	}
}

func TestFlowOverlay_DrawsTaskTypeMachineAtItsState(t *testing.T) {
	flow, err := gitflow.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	machine := &gitflow.Machine{Initial: "coding", States: []gitflow.State{
		{ID: "coding", Kind: gitflow.StateWait, Wait: gitflow.WaitManual, Events: []gitflow.Event{{Name: "close", Key: "C", Action: gitflow.EventActionClose, To: "qa"}}},
		{ID: "qa", Kind: gitflow.StateWait, Wait: gitflow.WaitManual, Events: []gitflow.Event{{Name: "accept", Key: "A", Label: "accept", To: "done"}}},
		{ID: "done", Kind: gitflow.StateFinal, Outcome: gitflow.OutcomeReleased},
	}}
	rule := flow.BranchTypes[gitflow.BranchTypeFeature]
	rule.Machine = machine
	flow.BranchTypes[gitflow.BranchTypeFeature] = rule
	view := stripAnsi(NewFlowOverlay(flow, FlowPosition{TaskType: "feature", TaskState: "qa"}).View())
	for _, want := range []string{"feature/* from develop, machine:", "[C] close (confirm plan) ─▶ qa", "▶ qa", "[A] accept ─▶ done"} {
		if !strings.Contains(view, want) {
			t.Fatalf("overlay missing %q:\n%s", want, view)
		}
	}
}
