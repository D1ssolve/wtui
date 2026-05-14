package modal

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/diss0x/wtui/internal/config"
)

var _ Modal = (*ConfigModal)(nil)

type ConfigModal struct {
	cfg *config.Config
}

func NewConfigModal(cfg *config.Config) *ConfigModal {
	if cfg == nil {
		cfg = &config.Config{}
	}

	return &ConfigModal{cfg: cfg}
}

func (m *ConfigModal) Title() string { return "Configuration" }

func (m *ConfigModal) SetTerminalSize(width, height int) {}

func (m *ConfigModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return CloseModalMsg{} }
		}
	}

	return m, nil
}

func (m *ConfigModal) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(modalColorBorder)

	keyStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(modalColorNormal).
		Width(20)

	valueStyle := lipgloss.NewStyle().
		Foreground(modalColorNormal)

	hintStyle := lipgloss.NewStyle().Foreground(modalColorDim)

	rows := [][2]string{
		{"root_dir:", m.cfg.RootDir},
		{"tasks_root:", m.cfg.TasksRoot},
		{"branch_prefix:", m.cfg.BranchPrefix},
		{"editor:", m.cfg.Editor},
		{"discovery_depth:", fmt.Sprintf("%d", m.cfg.DiscoveryDepth)},
		{"output_panel_lines:", fmt.Sprintf("%d", m.cfg.OutputPanelLines)},
		{"log_level:", m.cfg.LogLevel},
	}

	var sb strings.Builder
	sb.WriteString(titleStyle.Render(m.Title()))
	sb.WriteString("\n\n")

	for i, row := range rows {
		sb.WriteString(keyStyle.Render(row[0]))
		sb.WriteString(" ")
		sb.WriteString(valueStyle.Render(row[1]))
		if i < len(rows)-1 {
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n\n")
	sb.WriteString(hintStyle.Render("[Esc] close"))

	return sb.String()
}
