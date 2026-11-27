package modal

import (
	"fmt"
	"slices"
	"strings"

	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// WorkflowBuilder keeps edits local; the preset browser always remains unchanged.
type WorkflowBuilder struct {
	parent           *FlowOverlay
	draft            *gitflow.Draft
	cursor, offset   int
	preview, editing bool
	input            textinput.Model
	graph            string
	err              error
}

func newWorkflowBuilder(parent *FlowOverlay, d *gitflow.Draft) *WorkflowBuilder {
	b := &WorkflowBuilder{parent: parent, draft: d, input: textinput.New()}
	b.refresh()
	return b
}

func (b *WorkflowBuilder) refresh() {
	flow, err := b.draft.Compile()
	b.err = err
	if err != nil {
		b.graph = "Invalid workflow: " + err.Error()
	} else {
		b.graph = strings.Join(flowGraphLines(flow, FlowPosition{}), "\n")
	}
	b.offset = 0
}

func (b *WorkflowBuilder) Title() string { return "Workflow builder" }
func (b *WorkflowBuilder) ValidatedYAML() (string, error) {
	data := b.draft.YAML()
	_, err := gitflow.ParseWorkflowYAML(data)
	return data, err
}
func (b *WorkflowBuilder) SetTerminalSize(w, h int) {
	b.parent.SetTerminalSize(w, h)
	b.input.Width = max(1, b.width()-5)
}
func (b *WorkflowBuilder) width() int {
	return max(1, b.parent.boxWidth()-boxStyle(0).GetHorizontalPadding())
}
func (b *WorkflowBuilder) OverlayView() string {
	return lipgloss.Place(b.parent.width, b.parent.height, lipgloss.Center, lipgloss.Center, boxStyle(b.parent.boxWidth()).Render(b.View()))
}

func (b *WorkflowBuilder) Update(msg tea.Msg) (Modal, tea.Cmd) {
	fields := builderFields(b.draft)
	f := fields[b.cursor]
	key, ok := msg.(tea.KeyMsg)
	if b.editing {
		if ok {
			switch key.String() {
			case "esc", "enter":
				if key.String() == "enter" {
					f.set(b.input.Value())
					b.refresh()
				}
				b.editing = false
				b.input.Blur()
				return b, nil
			}
		}
		var cmd tea.Cmd
		b.input, cmd = b.input.Update(msg)
		return b, cmd
	}
	if !ok {
		return b, nil
	}
	switch key.String() {
	case "ctrl+s":
		if b.err == nil {
			return b, func() tea.Msg { return ReviewWorkflowApplyMsg{Builder: b} }
		}
	case "esc":
		if b.preview {
			b.preview = false
		} else {
			return b.parent, nil
		}
	case "tab":
		b.preview = !b.preview
	case "up", "k", "down", "j", "pgup", "pgdown", "home", "end":
		p := &b.cursor
		last := len(fields) - 1
		if b.preview {
			p = &b.offset
			last = max(0, len(b.graphRows())-b.previewRows())
		}
		switch key.String() {
		case "up", "k":
			*p--
		case "down", "j":
			*p++
		case "pgup":
			*p -= b.formRows()
		case "pgdown":
			*p += b.formRows()
		case "home":
			*p = 0
		case "end":
			*p = last
		}
		*p = max(0, min(*p, last))
	case "enter", "left", "right":
		if b.preview {
			return b, nil
		}
		if len(f.choices) > 0 {
			i := slices.Index(f.choices, f.value) + 1
			if key.String() == "left" {
				i -= 2
			}
			f.set(f.choices[(i+2*len(f.choices))%len(f.choices)])
		} else if key.String() != "enter" {
			return b, nil
		} else if f.act != nil {
			f.act()
		} else {
			b.editing = true
			b.input.SetValue(f.value)
			b.input.CursorEnd()
			return b, b.input.Focus()
		}
		b.cursor = min(b.cursor, len(builderFields(b.draft))-1)
		b.refresh()
	}
	return b, nil
}

func (b *WorkflowBuilder) formRows() int    { return max(1, (b.parent.height-6)/2) }
func (b *WorkflowBuilder) previewRows() int { return max(1, b.parent.height-6) }
func (b *WorkflowBuilder) graphRows() []string {
	return strings.Split(ansi.Wrap(b.graph, b.width(), ""), "\n")
}
func (b *WorkflowBuilder) View() string {
	width := b.width()
	clip := func(s string) string { return ansi.Truncate(s, width, "…") }
	rows := []string{clip(b.Title() + " · " + b.draft.Extends)}
	graph := b.graphRows()
	n := b.previewRows()
	start := min(b.offset, max(0, len(graph)-n))
	hint := "↑↓ scroll · Tab form · Ctrl+S apply"
	if !b.preview {
		fields := builderFields(b.draft)
		from := max(0, b.cursor-b.formRows()+1)
		for i := from; i < min(len(fields), from+b.formRows()); i++ {
			f := fields[i]
			mark := "  "
			if i == b.cursor {
				mark = "▶ "
			}
			line := f.label
			if f.set != nil {
				line += ": " + f.value
			}
			if i == b.cursor && b.editing {
				line = b.input.View()
			}
			rows = append(rows, clip(mark+line))
		}
		rows = append(rows, clip(fmt.Sprintf("Preview · field %d/%d", b.cursor+1, len(fields))))
		n = max(1, n-b.formRows()-1)
		start = 0
		hint = "Ctrl+S apply · Enter edit · Tab diagram · Esc discard"
		if b.editing {
			hint = "Enter accept · Esc cancel"
		}
	}
	rows = append(rows, graph[start:min(len(graph), start+n)]...)
	rows = append(rows, clip(hint))
	return strings.Join(rows, "\n")
}
