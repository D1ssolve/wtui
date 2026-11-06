package modal

import (
	"strings"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/task"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/forge"
)

var _ Modal = (*ForgeMenuModal)(nil)

type ForgeMenuModal struct {
	MetadataEditor
	taskID        string
	serviceName   string
	provider      forge.ForgeProvider
	actions       []forgeAction
	selectedIndex int
	available     bool
	inTitleMode   bool
	titleInput    textinput.Model
	titleError    string
}

type forgeAction int

const (
	forgeActionCreateMR forgeAction = iota
	forgeActionMergeMR
	forgeActionPipelineStatus
	forgeActionListIssues
)

func NewForgeMenuModal(serviceName string, provider forge.ForgeProvider, width, height int) *ForgeMenuModal {
	available := provider != forge.ForgeProviderUnknown
	titleInput := textinput.New()
	titleInput.Prompt = ""
	titleInput.Width = 45
	titleInput.PlaceholderStyle = lipgloss.NewStyle().Foreground(modalColorDim)
	menu := &ForgeMenuModal{
		serviceName:   serviceName,
		provider:      provider,
		actions:       []forgeAction{forgeActionCreateMR, forgeActionMergeMR, forgeActionPipelineStatus, forgeActionListIssues},
		available:     available,
		selectedIndex: 0,
		titleInput:    titleInput,
	}
	menu.metadataSize(width, height)
	menu.setMetadataField("title", "MR title", "")
	menu.setMetadataField("description", "MR description", "")
	return menu
}

func (m *ForgeMenuModal) Title() string { return "Forge Actions" }

func (m *ForgeMenuModal) SetTaskID(taskID string) {
	m.taskID = taskID
}

func (m *ForgeMenuModal) SetTerminalSize(width, height int) { m.metadataSize(width, height) }

func (m *ForgeMenuModal) MetadataRequest() (task.MetadataRequest, bool) {
	title := m.titleInput.Value()
	if m.editing {
		title = m.metadataValue("title")
	}
	return task.MetadataRequest{Purpose: ai.PurposeMR, TaskIDs: []string{m.taskID}, Title: title, Description: m.metadataValue("description")}, m.inTitleMode && !m.discarding
}

func (m *ForgeMenuModal) ApplyMetadataDraft(draft ai.Draft) {
	m.applyMetadata(draft)
	m.titleInput.SetValue(draft.Title)
	m.titleInput.CursorEnd()
	if m.editing {
		m.editor.SetValue(m.fields[m.selected].value)
	}
}

func (m *ForgeMenuModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if m.discarding {
		_, cmd := m.updateMetadata(msg)
		return m, cmd
	}
	if m.inTitleMode {
		return m.updateTitle(msg)
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch keyMsg.String() {
	case "esc":
		if m.confirmMetadataDiscard() {
			return m, nil
		}
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
			m.inTitleMode = true
			if m.titleInput.Value() == "" {
				m.titleInput.SetValue(taskID)
			}
			m.setMetadataField("title", "MR title", m.titleInput.Value())
			m.titleInput.CursorEnd()
			m.titleInput.Focus()
			return m, nil
		case forgeActionMergeMR:
			return m, func() tea.Msg { return ForgeMergeMRMsg{TaskID: taskID, ServiceName: serviceName} }
		case forgeActionPipelineStatus:
			provider := m.provider
			return m, func() tea.Msg {
				return ForgePipelineStatusMsg{TaskID: taskID, ServiceName: serviceName, Provider: provider}
			}
		case forgeActionListIssues:
			return m, func() tea.Msg { return ForgeListIssuesMsg{TaskID: taskID, ServiceName: serviceName} }
		default:
			return m, nil
		}
	default:
		return m, nil
	}
}

func (m *ForgeMenuModal) updateTitle(msg tea.Msg) (Modal, tea.Cmd) {
	if !m.editing {
		m.setMetadataField("title", "MR title", m.titleInput.Value())
		if len(m.fields) == 1 {
			m.setMetadataField("description", "MR description", "")
		}
	}
	if handled, cmd := m.updateMetadata(msg); handled {
		if !m.editing {
			m.titleInput.SetValue(m.metadataValue("title"))
			m.titleInput.CursorEnd()
		}
		return m, cmd
	}
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc":
			m.inTitleMode = false
			m.titleInput.Blur()
			m.titleError = ""
			return m, nil
		case "enter":
			title := strings.TrimSpace(m.titleInput.Value())
			if title == "" {
				m.titleError = "Title cannot be blank"
				return m, nil
			}
			taskID := m.taskID
			return m, func() tea.Msg {
				return ForgeCreateMRMsg{TaskID: taskID, Title: title, Description: m.metadataValue("description")}
			}
		}
	}

	var cmd tea.Cmd
	m.titleInput, cmd = m.titleInput.Update(msg)
	m.titleError = ""
	return m, cmd
}

func (m *ForgeMenuModal) View() string {
	if m.discarding {
		return m.MetadataView()
	}
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	normalStyle := lipgloss.NewStyle().Foreground(modalColorNormal)
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Forge menu: " + m.serviceName))
	sb.WriteString("\n\n")
	if m.inTitleMode {
		if m.editing {
			return m.MetadataView()
		}
		sb.WriteString(normalStyle.Render("MR/PR title"))
		sb.WriteString("\n")
		sb.WriteString(m.metadataInputView(m.titleInput))
		if m.titleError != "" {
			sb.WriteString("\n")
			sb.WriteString(lipgloss.NewStyle().Foreground(modalColorDanger).Render(m.titleError))
		}
		sb.WriteString("\n\n")
		if !m.generating {
			sb.WriteString(dimStyle.Render("[Enter] create  [Esc] back"))
		}
		sb.WriteString("\n\n" + m.MetadataView())
		return sb.String()
	}

	if !m.available {
		sb.WriteString(dimStyle.Render("No forge CLI available"))
		sb.WriteString("\n\n")
		sb.WriteString(dimStyle.Render("[Enter/Esc] close"))
		return sb.String()
	}

	names := []string{"Create missing MR/PRs", "Merge MR", "View Pipeline Status", "List Issues"}
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
