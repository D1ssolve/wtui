package modal

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/forge"
)

var _ Modal = (*ForgeMenuModal)(nil)

type ForgeMenuModal struct {
	taskID         string
	serviceName    string
	provider       forge.ForgeProvider
	actions        []forgeAction
	selectedIndex  int
	available      bool
	terminalWidth  int
	terminalHeight int
}

type forgeAction int

const (
	forgeActionCreateMR forgeAction = iota
	forgeActionPipelineStatus
	forgeActionListIssues
)

func NewForgeMenuModal(serviceName string, provider forge.ForgeProvider, width, height int) *ForgeMenuModal {
	available := provider != forge.ForgeProviderUnknown
	return &ForgeMenuModal{
		serviceName:    serviceName,
		provider:       provider,
		actions:        []forgeAction{forgeActionCreateMR, forgeActionPipelineStatus, forgeActionListIssues},
		available:      available,
		selectedIndex:  0,
		terminalWidth:  width,
		terminalHeight: height,
	}
}

func (m *ForgeMenuModal) Title() string { return "Forge Actions" }

func (m *ForgeMenuModal) SetTaskID(taskID string) {
	m.taskID = taskID
}

func (m *ForgeMenuModal) SetTerminalSize(width, height int) {
	m.terminalWidth = width
	m.terminalHeight = height
}

func (m *ForgeMenuModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch keyMsg.String() {
	case "esc":
		return m, func() tea.Msg { return CloseModalMsg{} }
	case "up", "k":
		if m.available {
			m.selectedIndex = (m.selectedIndex - 1 + len(m.actions)) % len(m.actions)
		}
		return m, nil
	case "down", "j":
		if m.available {
			m.selectedIndex = (m.selectedIndex + 1) % len(m.actions)
		}
		return m, nil
	case "enter":
		if !m.available {
			return m, func() tea.Msg { return CloseModalMsg{} }
		}
		taskID := m.taskID
		serviceName := m.serviceName
		switch m.actions[m.selectedIndex] {
		case forgeActionCreateMR:
			return m, func() tea.Msg { return ForgeCreateMRMsg{TaskID: taskID, ServiceName: serviceName} }
		case forgeActionPipelineStatus:
			return m, func() tea.Msg { return ForgePipelineStatusMsg{TaskID: taskID, ServiceName: serviceName} }
		case forgeActionListIssues:
			return m, func() tea.Msg { return ForgeListIssuesMsg{TaskID: taskID, ServiceName: serviceName} }
		default:
			return m, nil
		}
	default:
		return m, nil
	}
}

func (m *ForgeMenuModal) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Forge menu: " + m.serviceName))
	sb.WriteString("\n\n")

	if !m.available {
		sb.WriteString(dimStyle.Render("No forge CLI available"))
		sb.WriteString("\n\n")
		sb.WriteString(dimStyle.Render("[Enter/Esc] close"))
		return sb.String()
	}

	names := []string{"Create MR/PR", "View Pipeline Status", "List Issues"}
	for i, name := range names {
		prefix := "○ "
		style := dimStyle
		if i == m.selectedIndex {
			prefix = "◉ "
			style = normalStyle.Bold(true)
		}
		sb.WriteString(style.Render(prefix + name))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("[j/k or arrows] navigate  [Enter] select  [Esc] cancel"))
	return sb.String()
}
