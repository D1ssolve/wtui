package modal

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type RemoveTaskDialog struct {
	taskID        string
	serviceCount  int
	dirtyServices []string
}

func NewRemoveTaskDialog(taskID string, serviceCount int, dirtyServices []string) *RemoveTaskDialog {
	return &RemoveTaskDialog{
		taskID:        taskID,
		serviceCount:  serviceCount,
		dirtyServices: dirtyServices,
	}
}

func (d *RemoveTaskDialog) Title() string { return "Remove Task" }

// SetTerminalSize implements Modal.
func (d *RemoveTaskDialog) SetTerminalSize(width, height int) {}

func (d *RemoveTaskDialog) UpdateInfo(serviceCount int, dirtyServices []string) {
	d.serviceCount = serviceCount
	d.dirtyServices = dirtyServices
}

func (d *RemoveTaskDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y":
			taskID := d.taskID
			return d, func() tea.Msg {
				return SubmitRemoveTaskMsg{TaskID: taskID, Force: false, DeleteBranches: false}
			}

		case "f":
			taskID := d.taskID
			return d, func() tea.Msg {
				return SubmitRemoveTaskMsg{TaskID: taskID, Force: true, DeleteBranches: false}
			}

		case "b":
			taskID := d.taskID
			return d, func() tea.Msg {
				return SubmitRemoveTaskMsg{TaskID: taskID, Force: false, DeleteBranches: true}
			}

		case "n", "esc":
			return d, func() tea.Msg { return CloseModalMsg{} }
		}
	}

	return d, nil
}

func (d *RemoveTaskDialog) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	warnStyle := lipgloss.NewStyle().Foreground(modalColorWarning)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)

	var sb strings.Builder

	sb.WriteString(titleStyle.Render(fmt.Sprintf("Remove task %q?", d.taskID)))
	sb.WriteString("\n")
	worktreeWord := "worktree"
	if d.serviceCount != 1 {
		worktreeWord = "worktrees"
	}
	sb.WriteString(normalStyle.Render(
		fmt.Sprintf("This will delete %d %s.", d.serviceCount, worktreeWord),
	))

	if len(d.dirtyServices) > 0 {
		sb.WriteString("\n")
		for _, svc := range d.dirtyServices {
			sb.WriteString("\n")
			sb.WriteString(warnStyle.Render(
				fmt.Sprintf("⚠ %s has uncommitted changes.", svc),
			))
		}
	}

	sb.WriteString("\n\n")
	sb.WriteString(normalStyle.Render("[y] Remove worktrees"))
	sb.WriteString("\n")
	sb.WriteString(normalStyle.Render("[f] Force remove (ignore dirty)"))
	sb.WriteString("\n")
	sb.WriteString(normalStyle.Render("[b] Remove worktrees + branches"))
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("[n/Esc] Cancel"))

	return sb.String()
}
