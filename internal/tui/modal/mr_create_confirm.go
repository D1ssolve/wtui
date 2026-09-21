package modal

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var _ Modal = (*MRCreateConfirmDialog)(nil)

type MRCreateConfirmItem struct {
	ServiceName string
	Reason      string
}

type MRCreateConfirmDialog struct {
	taskID   string
	title    string
	services []MRCreateConfirmItem
}

func NewMRCreateConfirmDialog(taskID, title string, services []MRCreateConfirmItem) *MRCreateConfirmDialog {
	return &MRCreateConfirmDialog{taskID: taskID, title: title, services: append([]MRCreateConfirmItem(nil), services...)}
}

func (d *MRCreateConfirmDialog) Title() string { return "Confirm MR/PR Creation" }

func (d *MRCreateConfirmDialog) SetTerminalSize(_, _ int) {}

func (d *MRCreateConfirmDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}
	switch keyMsg.String() {
	case "enter", "y":
		taskID, title := d.taskID, d.title
		return d, func() tea.Msg {
			return ForgeConfirmCreateMRMsg{TaskID: taskID, Title: title}
		}
	case "esc", "n":
		return d, func() tea.Msg { return CloseModalMsg{} }
	default:
		return d, nil
	}
}

func (d *MRCreateConfirmDialog) View() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normal := lipgloss.NewStyle().Foreground(modalColorNormal)
	dim := lipgloss.NewStyle().Foreground(modalColorDim)
	warn := lipgloss.NewStyle().Foreground(modalColorWarning)

	var b strings.Builder
	b.WriteString(title.Render(d.Title()))
	b.WriteString("\n\n")
	b.WriteString(normal.Render("Task: " + d.taskID))
	b.WriteString("\n")
	b.WriteString(warn.Render("These services need confirmation before creating an MR/PR:"))
	b.WriteString("\n\n")
	for _, service := range d.services {
		b.WriteString(normal.Render("- " + service.ServiceName + ": " + service.Reason))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(dim.Render("[Enter/y] create anyway  [Esc/n] cancel"))
	return b.String()
}
