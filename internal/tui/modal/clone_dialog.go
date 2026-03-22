package modal

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var _ Modal = (*CloneDialog)(nil)

type CloneDialog struct {
	srcTaskID string
	input     textinput.Model
}

func NewCloneDialog(srcTaskID string) *CloneDialog {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "IN-9999"
	ti.Width = 40
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(modalColorDim)
	ti.Focus()

	return &CloneDialog{
		srcTaskID: srcTaskID,
		input:     ti,
	}
}

func (d *CloneDialog) Title() string {
	return fmt.Sprintf("Clone Task %s", d.srcTaskID)
}

func (d *CloneDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return d, func() tea.Msg { return CloseModalMsg{} }

		case "enter":
			src := d.srcTaskID
			dst := strings.TrimSpace(d.input.Value())
			return d, func() tea.Msg {
				return SubmitCloneMsg{Src: src, Dst: dst}
			}

		case "tab", "shift+tab":
			return d, nil
		}
	}

	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	return d, cmd
}

func (d *CloneDialog) View() string {
	labelStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(modalColorNormal)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(modalColorBorder)

	hintStyle := lipgloss.NewStyle().Foreground(modalColorDim)

	var sb strings.Builder

	sb.WriteString(titleStyle.Render(d.Title()))
	sb.WriteString("\n\n")
	sb.WriteString(labelStyle.Render("Destination Task ID:"))
	sb.WriteString(" ")
	sb.WriteString(d.input.View())
	sb.WriteString("\n\n")
	sb.WriteString(hintStyle.Render("[Enter] confirm  [Esc] cancel"))

	return sb.String()
}
