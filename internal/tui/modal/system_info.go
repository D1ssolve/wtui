package modal

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var _ Modal = (*SystemInfoModal)(nil)

// ToolInfo describes one external tool's availability.
type ToolInfo struct {
	Name      string
	Available bool
	Version   string
	Purpose   string
}

// SystemInfoModal shows which external tools are connected/configured.
type SystemInfoModal struct {
	Tools          []ToolInfo
	ForgeProvider  string
	GitLabHost     string
	GitHubHost     string
	Preset         string
	terminalWidth  int
	terminalHeight int
}

// NewSystemInfoModal creates a modal that lists external tool status.
func NewSystemInfoModal(lazygit, glab, gh bool, forgeProvider, gitlabHost, githubHost, preset string) *SystemInfoModal {
	tools := []ToolInfo{
		{Name: "git", Available: true, Purpose: "required"},
		{Name: "lazygit", Available: lazygit, Purpose: "TUI git client"},
		{Name: "glab", Available: glab, Purpose: "GitLab CLI"},
		{Name: "gh", Available: gh, Purpose: "GitHub CLI"},
	}
	return &SystemInfoModal{
		Tools:         tools,
		ForgeProvider: forgeProvider,
		GitLabHost:    gitlabHost,
		GitHubHost:    githubHost,
		Preset:        preset,
	}
}

func (m *SystemInfoModal) Title() string { return "System Status" }

func (m *SystemInfoModal) SetTerminalSize(width, height int) {
	m.terminalWidth = width
	m.terminalHeight = height
}

func (m *SystemInfoModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc", ".":
			return m, func() tea.Msg { return CloseModalMsg{} }
		}
	}
	return m, nil
}

func (m *SystemInfoModal) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorBorder)
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(modalColorNormal)
	availableStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#34D399"))
	missingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171"))
	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#A78BFA")).Width(14)

	row := func(name, value string) string {
		return "  " + keyStyle.Render(name) + value
	}

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("System Status"))
	sb.WriteString("\n\n")

	sb.WriteString(sectionStyle.Render("External Tools"))
	sb.WriteString("\n")
	for _, tool := range m.Tools {
		status := missingStyle.Render("✗ not found")
		if tool.Available {
			status = availableStyle.Render("✓ available")
		}
		sb.WriteString(fmt.Sprintf("  %-12s %s  %s\n", tool.Name, status, dimStyle.Render(tool.Purpose)))
	}

	sb.WriteString("\n")
	sb.WriteString(sectionStyle.Render("Forge Config"))
	sb.WriteString("\n")
	provider := m.ForgeProvider
	if provider == "" {
		provider = "auto"
	}
	sb.WriteString(row("Provider:", provider))
	sb.WriteString("\n")
	sb.WriteString(row("GitLab host:", m.GitLabHost))
	sb.WriteString("\n")
	sb.WriteString(row("GitHub host:", m.GitHubHost))
	sb.WriteString("\n")

	if m.Preset != "" {
		sb.WriteString("\n")
		sb.WriteString(sectionStyle.Render("Git Flow"))
		sb.WriteString("\n")
		sb.WriteString(row("Preset:", m.Preset))
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("[Esc] or [.] to close"))

	return sb.String()
}

// OpenSystemInfoMsg is emitted to open the system status modal.
type OpenSystemInfoMsg struct{}
