package modal

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type AddDialog struct {
	taskID string
	input  textinput.Model
}

func NewAddDialog(taskID string) *AddDialog {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "service1 service2 ..."
	ti.Width = 40
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(modalColorDim)

	ti.Focus()

	return &AddDialog{
		taskID: taskID,
		input:  ti,
	}
}

func (d *AddDialog) Title() string {
	return fmt.Sprintf("Add Service to %s", d.taskID)
}

func (d *AddDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return d, func() tea.Msg { return CloseModalMsg{} }

		case "enter":
			services := parseServices(d.input.Value())
			taskID := d.taskID
			return d, func() tea.Msg {
				return SubmitAddMsg{TaskID: taskID, Services: services}
			}

		case "tab", "shift+tab":
			return d, nil
		}
	}

	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	return d, cmd
}

func (d *AddDialog) View() string {
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
	sb.WriteString(labelStyle.Render("Services:"))
	sb.WriteString(" ")
	sb.WriteString(d.input.View())
	sb.WriteString("\n\n")
	sb.WriteString(hintStyle.Render("[Enter] confirm  [Esc] cancel"))

	return sb.String()
}
