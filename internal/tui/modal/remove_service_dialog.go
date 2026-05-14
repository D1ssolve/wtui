package modal

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type RemoveServiceDialog struct {
	taskID      string
	serviceName string
	branch      string
}

func NewRemoveServiceDialog(taskID, serviceName, branch string) *RemoveServiceDialog {
	return &RemoveServiceDialog{
		taskID:      taskID,
		serviceName: serviceName,
		branch:      branch,
	}
}

func (d *RemoveServiceDialog) Title() string { return "Remove Service" }

func (d *RemoveServiceDialog) SetTerminalSize(width, height int) {}

func (d *RemoveServiceDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "f":
			return d, func() tea.Msg {
				return SubmitRemoveServiceMsg{
					TaskID:       d.taskID,
					ServiceName:  d.serviceName,
					RemoveBranch: true,
				}
			}

		case "y":
			return d, func() tea.Msg {
				return SubmitRemoveServiceMsg{
					TaskID:       d.taskID,
					ServiceName:  d.serviceName,
					RemoveBranch: false,
				}
			}

		case "n", "esc":
			return d, func() tea.Msg { return CloseModalMsg{} }
		}
	}

	return d, nil
}

func (d *RemoveServiceDialog) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)

	var sb strings.Builder

	sb.WriteString(titleStyle.Render(fmt.Sprintf("Remove service %q (%q)?", d.serviceName, d.branch)))
	sb.WriteString("\n\n")
	sb.WriteString(normalStyle.Render("[f] Remove worktree + branch"))
	sb.WriteString("\n")
	sb.WriteString(normalStyle.Render("[y] Remove worktree only"))
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("[n/Esc] Cancel"))

	return sb.String()
}
