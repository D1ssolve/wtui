package modal

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/task"
)

type SyncStrategyDialog struct {
	taskID        string
	serviceName   string
	selectedIndex int
}

type strategyOption struct {
	name        string
	description string
	strategy    task.SyncStrategy
}

var strategyOptions = []strategyOption{
	{
		name:        "Merge",
		description: "Create a merge commit (safer, preserves history)",
		strategy:    task.SyncStrategyMerge,
	},
	{
		name:        "Rebase",
		description: "Rebase onto upstream (cleaner history, may cause conflicts)",
		strategy:    task.SyncStrategyRebase,
	},
	{
		name:        "Cancel",
		description: "Close without syncing",
		strategy:    task.SyncStrategyNoop,
	},
}

func NewSyncStrategyDialog(taskID string) *SyncStrategyDialog {
	return &SyncStrategyDialog{
		taskID:        taskID,
		selectedIndex: 0,
	}
}

func NewSyncServiceStrategyDialog(taskID, serviceName string) *SyncStrategyDialog {
	return &SyncStrategyDialog{taskID: taskID, serviceName: serviceName}
}

func (d *SyncStrategyDialog) Title() string { return "Sync Strategy" }

func (d *SyncStrategyDialog) SetTerminalSize(_, _ int) {}

func (d *SyncStrategyDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":

			if d.selectedIndex > 0 {
				d.selectedIndex--
			} else {
				d.selectedIndex = len(strategyOptions) - 1
			}
			return d, nil

		case "down", "j":

			if d.selectedIndex < len(strategyOptions)-1 {
				d.selectedIndex++
			} else {
				d.selectedIndex = 0
			}
			return d, nil

		case "enter":
			selectedStrategy := strategyOptions[d.selectedIndex].strategy
			taskID := d.taskID
			if d.serviceName != "" {
				serviceName := d.serviceName
				return d, func() tea.Msg {
					return SubmitSyncServiceStrategyMsg{
						TaskID:      taskID,
						ServiceName: serviceName,
						Strategy:    selectedStrategy,
					}
				}
			}
			return d, func() tea.Msg {
				return SubmitSyncStrategyMsg{
					TaskID:   taskID,
					Strategy: selectedStrategy,
				}
			}

		case "esc":
			return d, func() tea.Msg { return CloseModalMsg{} }
		}
	}

	return d, nil
}

func (d *SyncStrategyDialog) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(modalColorBorder)

	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)

	var sb strings.Builder

	title := "Sync task " + d.taskID
	if d.serviceName != "" {
		title = "Sync service " + d.taskID + "/" + d.serviceName
	}
	sb.WriteString(titleStyle.Render(title))
	sb.WriteString("\n\n")

	for i, opt := range strategyOptions {
		var indicator string
		if i == d.selectedIndex {
			indicator = "◉ "
		} else {
			indicator = "○ "
		}

		if i == d.selectedIndex {
			sb.WriteString(normalStyle.Bold(true).Render(indicator + opt.name))
		} else {
			sb.WriteString(dimStyle.Render(indicator + opt.name))
		}
		sb.WriteString("\n")

		sb.WriteString(dimStyle.Render("    " + opt.description))
		sb.WriteString("\n\n")
	}

	sb.WriteString(dimStyle.Render("[j/k or arrows] navigate  [Enter] confirm  [Esc] cancel"))

	return sb.String()
}
