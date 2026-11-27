package modal

import (
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/gitflow"
	tea "github.com/charmbracelet/bubbletea"
)

func TestWorkflowBuilder_EditPreviewsWithoutChangingPreset(t *testing.T) {
	flow, _ := gitflow.Resolve(nil)
	parent := NewFlowOverlay(flow, FlowPosition{})
	parent.SetTerminalSize(80, 24)
	parent.Update(tea.KeyMsg{Type: tea.KeyRight})
	editor, _ := parent.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	b, ok := editor.(*WorkflowBuilder)
	if !ok {
		t.Fatalf("e on preset opened %T", editor)
	}
	b.Update(tea.KeyMsg{Type: tea.KeyEnter})
	b.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	b.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("live")})
	b.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if b.draft.Production != "live" || !strings.Contains(stripAnsi(b.View()), "production live") {
		t.Fatalf("expected compiled graph to reflect edited branch:\n%s", b.View())
	}
	back, _ := b.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if back != parent || !strings.Contains(stripAnsi(parent.View()), "production master") {
		t.Fatal("leaving the builder changed the preset")
	}
}

func TestWorkflowBuilder_InvalidDraftShowsCompilerErrorThenRecovers(t *testing.T) {
	flow, _ := gitflow.Resolve(nil)
	d, _ := gitflow.NewDraft("git-flow")
	parent := NewFlowOverlay(flow, FlowPosition{})
	b := newWorkflowBuilder(parent, d)
	b.SetTerminalSize(60, 16)
	params := d.Type("feature").Close[0].Params
	params["merge"] = []string{"typo"}
	b.refresh()
	b.Update(tea.KeyMsg{Type: tea.KeyTab})
	view := stripAnsi(b.View())
	if !strings.Contains(view, "task_types.feature.close") || strings.Contains(view, "Branches  production") {
		t.Fatalf("invalid input must replace the old diagram with a diagnostic:\n%s", view)
	}
	params["merge"] = []string{"squash"}
	b.refresh()
	if view = stripAnsi(b.View()); !strings.Contains(view, "Branches  production") || strings.Contains(view, "Invalid workflow") {
		t.Fatalf("valid correction must restore the diagram:\n%s", view)
	}
}
