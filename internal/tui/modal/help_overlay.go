package modal

import (
	"github.com/D1ssolve/wtui/internal/tui/theme"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/domain"
)

// HelpSection is one titled group of shortcut rows; the caller orders sections.
type HelpSection struct {
	Title string
	Rows  []HelpRow
}

type HelpRow struct {
	Key  string
	Desc string
}

type HelpOverlay struct {
	sections       []HelpSection
	workflowTitle  string
	workflow       *domain.WorkflowSummary
	scrollOffset   int
	terminalWidth  int
	terminalHeight int
}

func NewHelpOverlay(sections []HelpSection) *HelpOverlay {
	return &HelpOverlay{sections: sections}
}

func (h *HelpOverlay) Title() string { return "Keyboard Shortcuts" }

func (h *HelpOverlay) SetTerminalSize(width, height int) {
	h.terminalWidth = width
	h.terminalHeight = height
	h.clampScroll()
}

func (h *HelpOverlay) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "esc", "?":
			return h, func() tea.Msg { return CloseModalMsg{} }
		case "q", "ctrl+c":
			return h, tea.Quit
		case "up", "k":
			h.scrollOffset--
			h.clampScroll()
			return h, nil
		case "down", "j":
			h.scrollOffset++
			h.clampScroll()
			return h, nil
		case "pgup":
			h.scrollOffset -= h.contentVisible()
			h.clampScroll()
			return h, nil
		case "pgdown":
			h.scrollOffset += h.contentVisible()
			h.clampScroll()
			return h, nil
		case "home", "g":
			h.scrollOffset = 0
			return h, nil
		case "end", "G":
			h.scrollOffset = h.maxScrollOffset()
			return h, nil
		}
	}
	return h, nil
}

func (h *HelpOverlay) contentLines() []string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(modalColorBorder)

	sectionStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(modalColorNormal)

	var sb strings.Builder

	if workflowLines := h.renderWorkflow(); len(workflowLines) > 0 {
		sb.WriteString(strings.Join(workflowLines, "\n"))
		sb.WriteString("\n\n")
	}

	sb.WriteString(titleStyle.Render("Keyboard Shortcuts"))
	sb.WriteString("\n\n")
	for _, section := range h.sections {
		sb.WriteString(sectionStyle.Render(section.Title + ":"))
		sb.WriteString("\n")
		for _, row := range section.Rows {
			sb.WriteString(h.shortcutRow(row.Key, row.Desc))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString(theme.HintLine("[Esc] or [?] to close"))

	return strings.Split(sb.String(), "\n")
}

func (h *HelpOverlay) visibleLines() int {
	if h.terminalHeight <= 0 {
		return len(h.contentLines())
	}

	maxContentH := max(h.terminalHeight*70/100, 10)
	innerH := min(maxContentH, h.terminalHeight-2)
	available := innerH - 6
	if available < 3 {
		return 3
	}
	return available
}
