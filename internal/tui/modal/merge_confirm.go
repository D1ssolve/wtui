package modal

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var _ Modal = (*MergeConfirmDialog)(nil)

type MergeServiceStatus struct {
	ServiceName string
	Status      string
	Blockers    []string
}

type MergeConfirmDialog struct {
	taskID    string
	releaseID string
	services  []MergeServiceStatus
}

func NewMergeConfirmDialog(taskID, releaseID string, services []MergeServiceStatus) *MergeConfirmDialog {
	return &MergeConfirmDialog{taskID: taskID, releaseID: releaseID, services: append([]MergeServiceStatus(nil), services...)}
}

func (d *MergeConfirmDialog) Title() string { return "Confirm Merge Ready MRs" }

func (d *MergeConfirmDialog) SetTerminalSize(_, _ int) {}

func (d *MergeConfirmDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}
	switch keyMsg.String() {
	case "enter", "y":
		return d, func() tea.Msg { return ConfirmMergeMsg{TaskID: d.taskID, ReleaseID: d.releaseID} }
	case "esc", "n":
		return d, func() tea.Msg { return CloseModalMsg{} }
	default:
		return d, nil
	}
}

func (d *MergeConfirmDialog) View() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normal := lipgloss.NewStyle().Foreground(modalColorNormal)
	dim := lipgloss.NewStyle().Foreground(modalColorDim)

	var b strings.Builder
	b.WriteString(title.Render(d.Title()))
	b.WriteString("\n\n")
	if d.taskID != "" {
		b.WriteString(normal.Render("Task: " + d.taskID))
	} else {
		b.WriteString(normal.Render("Release: " + d.releaseID))
	}
	b.WriteString("\n\nService | Status | Blockers\n")
	for _, service := range d.services {
		line := service.ServiceName + " | " + service.Status
		if len(service.Blockers) > 0 {
			line += " | " + strings.Join(service.Blockers, "; ")
		}
		b.WriteString(normal.Render(line))
		b.WriteByte('\n')
	}
	b.WriteString("\n")
	b.WriteString(dim.Render("[Enter/y] merge ready  [Esc/n] cancel"))
	return b.String()
}
