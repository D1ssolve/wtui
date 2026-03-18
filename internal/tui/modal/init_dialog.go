package modal

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── InitDialog ────────────────────────────────────────────────────────────────

// InitDialog is a 4-field form for initializing a new task group.
//
// Fields (in Tab order):
//
//	0: Task ID         — placeholder "IN-6748"
//	1: Services        — placeholder "service1 service2 ..."
//	2: Branch Prefix   — pre-filled from defaultBranchPrefix; placeholder "feature/"
//	3: Base Branch     — placeholder "main (leave empty for auto-detect)"
type InitDialog struct {
	fields              [4]textinput.Model
	focusIndex          int
	defaultBranchPrefix string
}

// field labels rendered in the dialog view.
var initFieldLabels = [4]string{
	"Task ID:",
	"Services:",
	"Branch Prefix:",
	"Base Branch:",
}

// NewInitDialog creates an InitDialog pre-filled with defaultBranchPrefix in the
// Branch Prefix field.  Field 0 receives initial keyboard focus.
func NewInitDialog(defaultBranchPrefix string) *InitDialog {
	d := &InitDialog{defaultBranchPrefix: defaultBranchPrefix}

	placeholders := [4]string{
		"IN-6748",
		"service1 service2 ...",
		"feature/",
		"main (leave empty for auto-detect)",
	}

	for i := range d.fields {
		ti := textinput.New()
		ti.Prompt = ""
		ti.Placeholder = placeholders[i]
		ti.Width = 40
		ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(modalColorDim)
		d.fields[i] = ti
	}

	// Pre-fill Branch Prefix field.
	if defaultBranchPrefix != "" {
		d.fields[2].SetValue(defaultBranchPrefix)
	}

	// Focus the first field.
	d.focusField(0)

	return d
}

// Title implements Modal.
func (d *InitDialog) Title() string { return "New Task" }

// Update implements Modal.
func (d *InitDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return d, func() tea.Msg { return CloseModalMsg{} }

		case "tab":
			// Advance to next field (wraps from 3 → 0).
			return d, d.nextField()

		case "shift+tab":
			// Move to previous field (wraps from 0 → 3).
			return d, d.prevField()

		case "enter":
			if d.focusIndex == 3 {
				// Last field: parse and submit.
				return d, d.submit()
			}
			// Any other field: advance like Tab.
			return d, d.nextField()
		}
	}

	// Forward other messages (character input, cursor movement, etc.) to the
	// currently focused field only.
	var cmd tea.Cmd
	d.fields[d.focusIndex], cmd = d.fields[d.focusIndex].Update(msg)
	return d, cmd
}

// View implements Modal.
func (d *InitDialog) View() string {
	labelStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(modalColorNormal).
		Width(16) // fixed width so inputs line up

	var sb strings.Builder

	// Title line.
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(modalColorBorder)
	sb.WriteString(titleStyle.Render("New Task"))
	sb.WriteString("\n\n")

	// Each field: label + text input on the same logical row.
	for i := range d.fields {
		sb.WriteString(labelStyle.Render(initFieldLabels[i]))
		sb.WriteString(" ")
		sb.WriteString(d.fields[i].View())
		sb.WriteString("\n")
		if i < 3 {
			sb.WriteString("\n")
		}
	}

	// Hint bar.
	hintStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	sb.WriteString("\n")
	sb.WriteString(hintStyle.Render("[Enter] confirm  [Esc] cancel"))

	return sb.String()
}

// ── helpers ───────────────────────────────────────────────────────────────────

// focusField focuses field i and blurs all others.
func (d *InitDialog) focusField(i int) tea.Cmd {
	d.focusIndex = i
	var cmds []tea.Cmd
	for j := range d.fields {
		if j == i {
			cmds = append(cmds, d.fields[j].Focus())
		} else {
			d.fields[j].Blur()
		}
	}
	return tea.Batch(cmds...)
}

// nextField advances to the next field (wrapping 3 → 0).
func (d *InitDialog) nextField() tea.Cmd {
	next := (d.focusIndex + 1) % 4
	return d.focusField(next)
}

// prevField moves to the previous field (wrapping 0 → 3).
func (d *InitDialog) prevField() tea.Cmd {
	prev := (d.focusIndex + 3) % 4 // +3 == -1 mod 4
	return d.focusField(prev)
}

// submit builds and emits a SubmitInitMsg.
func (d *InitDialog) submit() tea.Cmd {
	msg := SubmitInitMsg{
		TaskID:       strings.TrimSpace(d.fields[0].Value()),
		Services:     parseServices(d.fields[1].Value()),
		BranchPrefix: strings.TrimSpace(d.fields[2].Value()),
		BaseBranch:   strings.TrimSpace(d.fields[3].Value()),
	}
	return func() tea.Msg { return msg }
}

// parseServices splits raw input on whitespace and commas, discarding empty
// tokens.  This is the canonical service-list parser used by Init and Add.
func parseServices(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool {
		return r == ' ' || r == ','
	})
}
