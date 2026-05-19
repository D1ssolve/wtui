package modal

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/diss0x/wtui/internal/task"
)

type SyncServiceStrategyDialog struct {
	taskID      string
	serviceName string

	selectedIndex int

	terminalWidth  int
	terminalHeight int
}

var serviceStrategyOptions = []strategyOption{
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

func NewSyncServiceStrategyDialog(taskID, serviceName string) *SyncServiceStrategyDialog {
	return &SyncServiceStrategyDialog{
		taskID:        taskID,
		serviceName:   serviceName,
		selectedIndex: 0,
	}
}

func (d *SyncServiceStrategyDialog) Title() string { return "Sync Strategy" }

func (d *SyncServiceStrategyDialog) SetTerminalSize(width, height int) {
	d.terminalWidth = width
	d.terminalHeight = height
}

func (d *SyncServiceStrategyDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if d.selectedIndex > 0 {
				d.selectedIndex--
			} else {
				d.selectedIndex = len(serviceStrategyOptions) - 1
			}
			return d, nil

		case "down", "j":
			if d.selectedIndex < len(serviceStrategyOptions)-1 {
				d.selectedIndex++
			} else {
				d.selectedIndex = 0
			}
			return d, nil

		case "enter":
			selectedStrategy := serviceStrategyOptions[d.selectedIndex].strategy
			taskID := d.taskID
			serviceName := d.serviceName
			return d, func() tea.Msg {
				return SubmitSyncServiceStrategyMsg{
					TaskID:      taskID,
					ServiceName: serviceName,
					Strategy:    selectedStrategy,
				}
			}

		case "esc":
			return d, func() tea.Msg { return CloseModalMsg{} }
		}
	}

	return d, nil
}

func (d *SyncServiceStrategyDialog) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(modalColorBorder)

	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)

	var sb strings.Builder

	sb.WriteString(titleStyle.Render(fmt.Sprintf("Sync service %s/%s", d.taskID, d.serviceName)))
	sb.WriteString("\n\n")

	for i, opt := range serviceStrategyOptions {
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