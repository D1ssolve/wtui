package modal

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── AddDialog ─────────────────────────────────────────────────────────────────

// AddDialog is a 1-field form for adding one or more services to an existing
// task group.
//
// Fields:
//
//	0: Services — placeholder "service1 service2 ..."
type AddDialog struct {
	taskID string
	input  textinput.Model
}

// NewAddDialog creates an AddDialog pre-associated with the given taskID.
// The Services input field receives initial keyboard focus.
func NewAddDialog(taskID string) *AddDialog {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "service1 service2 ..."
	ti.Width = 40
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(modalColorDim)

	// Focus the input immediately.
	ti.Focus() //nolint:errcheck — v1 Focus() returns tea.Cmd, not error

	return &AddDialog{
		taskID: taskID,
		input:  ti,
	}
}

// Title implements Modal.
func (d *AddDialog) Title() string {
	return fmt.Sprintf("Add Service to %s", d.taskID)
}

// Update implements Modal.
func (d *AddDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return d, func() tea.Msg { return CloseModalMsg{} }

		case "enter":
			// Parse services and emit the submit message.
			services := parseServices(d.input.Value())
			taskID := d.taskID
			return d, func() tea.Msg {
				return SubmitAddMsg{TaskID: taskID, Services: services}
			}

		case "tab", "shift+tab":
			// Single-field form: Tab/Shift+Tab are no-ops (no other field to
			// cycle to).
			return d, nil
		}
	}

	// Forward all other messages to the text input.
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	return d, cmd
}

// View implements Modal.
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
