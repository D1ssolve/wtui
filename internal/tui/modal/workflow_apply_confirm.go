package modal

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type ReviewWorkflowApplyMsg struct{ Builder *WorkflowBuilder }
type ConfirmWorkflowApplyMsg struct{ Dialog *WorkflowApplyConfirm }

type WorkflowApplyConfirm struct {
	builder *WorkflowBuilder
	path    string
	active  int
}

func NewWorkflowApplyConfirm(b *WorkflowBuilder, path string, activeReleases int) *WorkflowApplyConfirm {
	return &WorkflowApplyConfirm{builder: b, path: path, active: activeReleases}
}

func (d *WorkflowApplyConfirm) Title() string             { return "Apply workflow" }
func (d *WorkflowApplyConfirm) Builder() *WorkflowBuilder { return d.builder }
func (d *WorkflowApplyConfirm) SetTerminalSize(w, h int)  { d.builder.SetTerminalSize(w, h) }

func (d *WorkflowApplyConfirm) notice() string {
	text := fmt.Sprintf("Apply workflow to %q?\n\nReplace the entire workflow block, including any custom machines and service rules. Other settings stay unchanged. An existing config is backed up.", d.path)
	if d.active > 0 {
		text += fmt.Sprintf("\n\n%d in-flight release(s): changing their workflow can prevent them from continuing.", d.active)
	}
	return ansi.Wrap(text+"\n\n[y] apply   [Esc] keep editing", d.builder.width(), "")
}
func (d *WorkflowApplyConfirm) fits() bool {
	return lipgloss.Height(d.notice())+boxStyle(0).GetVerticalFrameSize() <= d.builder.parent.height
}
func (d *WorkflowApplyConfirm) View() string {
	if !d.fits() {
		return ansi.Wrap("Enlarge terminal to review before applying. Esc: back.", d.builder.width(), "")
	}
	return d.notice()
}
func (d *WorkflowApplyConfirm) OverlayView() string {
	p := d.builder.parent
	return lipgloss.Place(p.width, p.height, lipgloss.Center, lipgloss.Center, boxStyle(p.boxWidth()).Render(d.View()))
}
func (d *WorkflowApplyConfirm) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc", "n":
			return d.builder, nil
		case "y":
			if d.fits() {
				return d, func() tea.Msg { return ConfirmWorkflowApplyMsg{Dialog: d} }
			}
		}
	}
	return d, nil
}
