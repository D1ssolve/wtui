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
