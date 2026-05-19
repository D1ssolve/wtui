package modal

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type StashDialog struct {
	taskID      string
	serviceName string
	pop         bool

	selectedIndex int

	terminalWidth  int
	terminalHeight int
}

type stashOption struct {
	name            string
	description     string
	includeUntracked bool
	pop             bool
}

func stashOptions(pop bool) []stashOption {
	op := "Stash"
	if pop {
		op = "Stash pop"
	}
	return []stashOption{
		{
			name:            op + " (tracked only)",
			description:     "Stash tracked files only, leave untracked files untouched",
			includeUntracked: false,
			pop:             pop,
		},
		{
			name:            op + " (include untracked)",
			description:     "Stash both tracked and untracked files",
			includeUntracked: true,
			pop:             pop,
		},
		{
			name:        "Cancel",
			description: "Close without stashing",
			includeUntracked: false,
			pop:             pop,
		},
	}
}

func NewStashDialog(taskID, serviceName string, pop bool) *StashDialog {
	return &StashDialog{
		taskID:         taskID,
		serviceName:    serviceName,
		pop:            pop,
		selectedIndex:  0,
	}
}

func (d *StashDialog) Title() string { return "Stash" }

func (d *StashDialog) SetTerminalSize(width, height int) {
	d.terminalWidth = width
	d.terminalHeight = height
}

func (d *StashDialog) Update(msg tea.Msg) (Modal, tea.Cmd) {
	options := stashOptions(d.pop)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if d.selectedIndex > 0 {
				d.selectedIndex--
			} else {
				d.selectedIndex = len(options) - 1
			}
			return d, nil

		case "down", "j":
			if d.selectedIndex < len(options)-1 {
				d.selectedIndex++
			} else {
				d.selectedIndex = 0
			}
			return d, nil

		case "enter":
			selected := options[d.selectedIndex]
			taskID := d.taskID
			serviceName := d.serviceName
			return d, func() tea.Msg {
				return SubmitStashMsg{
					TaskID:           taskID,
					ServiceName:      serviceName,
					Pop:              selected.pop,
					IncludeUntracked: selected.includeUntracked,
				}
			}

		case "esc":
			return d, func() tea.Msg { return CloseModalMsg{} }
		}
	}

	return d, nil
}

func (d *StashDialog) View() string {
	options := stashOptions(d.pop)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(modalColorBorder)

	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)

	op := "Stash"
	if d.pop {
		op = "Stash pop"
	}

	var sb strings.Builder

	sb.WriteString(titleStyle.Render(fmt.Sprintf("%s changes for %s/%s", op, d.taskID, d.serviceName)))
	sb.WriteString("\n\n")

	for i, opt := range options {
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