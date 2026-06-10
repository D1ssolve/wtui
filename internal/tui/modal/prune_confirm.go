package modal

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/domain"
)

var _ Modal = (*PruneConfirmModal)(nil)

type PruneConfirmModal struct {
	rows           []pruneCandidateRow
	selectedIndex  int
	terminalWidth  int
	terminalHeight int
}

type pruneCandidateRow struct {
	taskID       string
	serviceCount int
	selectable   bool
	selected     bool
	statusText   string
	reason       string
}

func NewPruneConfirmModal(candidates []domain.PruneCandidate, width, height int) *PruneConfirmModal {
	rows := make([]pruneCandidateRow, 0, len(candidates))
	for _, c := range candidates {
		row := pruneCandidateRow{
			taskID:       c.TaskID,
			serviceCount: len(c.Services),
			selectable:   c.Prunable,
			selected:     c.Prunable,
		}

		if c.Prunable {
			row.statusText = "prunable"
		} else {
			row.statusText = "blocked"
			row.reason = blockedReason(c)
		}

		rows = append(rows, row)
	}

	m := &PruneConfirmModal{
		rows:           rows,
		terminalWidth:  width,
		terminalHeight: height,
	}
	m.selectedIndex = m.firstSelectableIndex()
	return m
}

func (m *PruneConfirmModal) Title() string { return "Prune Tasks" }

func (m *PruneConfirmModal) SetTerminalSize(width, height int) {
	m.terminalWidth = width
	m.terminalHeight = height
}

func (m *PruneConfirmModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch keyMsg.String() {
	case "up", "k":
		m.moveSelection(-1)
		return m, nil
	case "down", "j":
		m.moveSelection(1)
		return m, nil
	case " ":
		m.toggleCurrent()
		return m, nil
	case "enter":
		selected := make([]string, 0, len(m.rows))
		for _, row := range m.rows {
			if row.selectable && row.selected {
				selected = append(selected, row.taskID)
			}
		}
		return m, func() tea.Msg { return SubmitPruneMsg{SelectedTaskIDs: selected} }
	case "esc":
		return m, func() tea.Msg { return CloseModalMsg{} }
	default:
		return m, nil
	}
}

func (m *PruneConfirmModal) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Select tasks to prune"))
	sb.WriteString("\n\n")

	if len(m.rows) == 0 {
		sb.WriteString(dimStyle.Render("No prune candidates found."))
		sb.WriteString("\n\n")
		sb.WriteString(dimStyle.Render("[Enter/Esc] close"))
		return sb.String()
	}

	sb.WriteString(dimStyle.Render("TaskID | Services | Status"))
	sb.WriteString("\n")

	for i, row := range m.rows {
		prefix := "  "
		if i == m.selectedIndex {
			prefix = "▸ "
		}

		checkbox := "[ ]"
		if row.selectable && row.selected {
			checkbox = "[x]"
		}
		if !row.selectable {
			checkbox = "[-]"
		}

		status := row.statusText
		if row.reason != "" {
			status += " (" + row.reason + ")"
		}

		line := fmt.Sprintf("%s%-20s | %2d | %s", checkbox+" "+prefix, row.taskID, row.serviceCount, status)
		if row.selectable {
			if i == m.selectedIndex {
				sb.WriteString(normalStyle.Bold(true).Render(line))
			} else {
				sb.WriteString(normalStyle.Render(line))
			}
		} else {
			sb.WriteString(dimStyle.Render(line))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("[j/k or arrows] navigate  [Space] toggle  [Enter] confirm  [Esc] cancel"))
	return sb.String()
}

func (m *PruneConfirmModal) moveSelection(step int) {
	if len(m.rows) == 0 {
		m.selectedIndex = 0
		return
	}
	m.selectedIndex = (m.selectedIndex + step + len(m.rows)) % len(m.rows)
}

func (m *PruneConfirmModal) toggleCurrent() {
	if len(m.rows) == 0 || m.selectedIndex < 0 || m.selectedIndex >= len(m.rows) {
		return
	}
	if !m.rows[m.selectedIndex].selectable {
		return
	}
	m.rows[m.selectedIndex].selected = !m.rows[m.selectedIndex].selected
}

func (m *PruneConfirmModal) firstSelectableIndex() int {
	for i, row := range m.rows {
		if row.selectable {
			return i
		}
	}
	if len(m.rows) == 0 {
		return 0
	}
	return 0
}

func blockedReason(candidate domain.PruneCandidate) string {
	reasons := make([]string, 0, len(candidate.Services))
	for _, svc := range candidate.Services {
		switch {
		case svc.Err != nil:
			reasons = append(reasons, fmt.Sprintf("%s: %v", svc.ServiceName, svc.Err))
		case !svc.IsMerged && !svc.IsStale:
			reasons = append(reasons, svc.ServiceName+": not merged")
		}
	}
	if len(reasons) == 0 {
		return "not prunable"
	}
	return strings.Join(reasons, "; ")
}
