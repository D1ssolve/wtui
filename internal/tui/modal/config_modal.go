package modal

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"

	"github.com/D1ssolve/wtui/internal/config"
)

var _ Modal = (*ConfigModal)(nil)

type ConfigModal struct {
	cfg            *config.Config
	scrollOffset   int
	terminalWidth  int
	terminalHeight int
}

func NewConfigModal(cfg *config.Config) *ConfigModal {
	if cfg == nil {
		cfg = &config.Config{}
	}

	return &ConfigModal{cfg: cfg}
}

func (m *ConfigModal) Title() string { return "Configuration" }

func (m *ConfigModal) SetTerminalSize(width, height int) {
	m.terminalWidth = width
	m.terminalHeight = height
}

func (m *ConfigModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return CloseModalMsg{} }
		case "up", "k":
			if m.scrollOffset > 0 {
				m.scrollOffset--
			}
			return m, nil
		case "down", "j":
			m.scrollOffset++
			return m, nil
		case "pgup":
			m.scrollOffset = max(0, m.scrollOffset-m.visibleLines())
			return m, nil
		case "pgdown":
			m.scrollOffset += m.visibleLines()
			return m, nil
		case "home", "g":
			m.scrollOffset = 0
			return m, nil
		case "end", "G":
			m.scrollOffset = m.maxScrollOffset()
			return m, nil
		}
	}

	return m, nil
}

func (m *ConfigModal) contentLines() []string {
	data, err := yaml.Marshal(m.cfg)
	if err != nil {
		data = []byte("error: " + err.Error())
	}
	return strings.Split(string(data), "\n")
}

func (m *ConfigModal) visibleLines() int {
	if m.terminalHeight <= 0 {
		return 20
	}

	maxContentH := max(m.terminalHeight*70/100, 10)
	innerH := min(maxContentH, m.terminalHeight-2)
	available := innerH - 6
	if available < 3 {
		return 3
	}
	return available
}

func (m *ConfigModal) maxScrollOffset() int {
	lines := len(m.contentLines())
	visible := m.visibleLines()
	if lines <= visible {
		return 0
	}
	return lines - visible
}

func (m *ConfigModal) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(modalColorBorder)

	codeStyle := lipgloss.NewStyle().
		Foreground(modalColorNormal)

	hintStyle := lipgloss.NewStyle().Foreground(modalColorDim)

	lines := m.contentLines()
	visible := m.visibleLines()
	maxOffset := m.maxScrollOffset()

	if m.scrollOffset > maxOffset {
		m.scrollOffset = maxOffset
	}

	end := m.scrollOffset + visible
	if end > len(lines) {
		end = len(lines)
	}
	visibleLines := lines[m.scrollOffset:end]

	var sb strings.Builder
	sb.WriteString(titleStyle.Render(m.Title()))
	sb.WriteString("\n\n")
	sb.WriteString(codeStyle.Render(strings.Join(visibleLines, "\n")))
	sb.WriteString("\n")

	if maxOffset > 0 {
		sb.WriteString(hintStyle.Render(fmt.Sprintf("[%d/%d] ", m.scrollOffset+1, maxOffset+1)))
	}
	sb.WriteString(hintStyle.Render("[j/k] scroll  [Esc] close"))

	return sb.String()
}
