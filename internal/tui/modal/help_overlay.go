package modal

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type HelpOverlay struct {
	lazygitAvailable bool
	scrollOffset     int
	terminalHeight   int
}

func NewHelpOverlayWithOptions(lazygitAvailable bool) *HelpOverlay {
	return &HelpOverlay{lazygitAvailable: lazygitAvailable}
}

func (h *HelpOverlay) Title() string { return "Keyboard Shortcuts" }

func (h *HelpOverlay) SetTerminalSize(width, height int) {
	_ = width
	h.terminalHeight = height
	h.clampScroll()
}

func (h *HelpOverlay) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "esc", "?":
			return h, func() tea.Msg { return CloseModalMsg{} }
		case "up", "k":
			h.scrollOffset--
			h.clampScroll()
			return h, nil
		case "down", "j":
			h.scrollOffset++
			h.clampScroll()
			return h, nil
		case "pgup":
			h.scrollOffset -= h.visibleLines()
			h.clampScroll()
			return h, nil
		case "pgdown":
			h.scrollOffset += h.visibleLines()
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

	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#A78BFA")).
		Width(16)

	descStyle := lipgloss.NewStyle().
		Foreground(modalColorNormal)

	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)

	row := func(key, desc string) string {
		return "  " + keyStyle.Render(key) + descStyle.Render(desc)
	}

	var sb strings.Builder

	sb.WriteString(titleStyle.Render("Keyboard Shortcuts"))
	sb.WriteString("\n\n")

	sb.WriteString(sectionStyle.Render("Tasks Panel:"))
	sb.WriteString("\n")
	sb.WriteString(row("i", "Init new task group"))
	sb.WriteString("\n")
	sb.WriteString(row("c", "Clone selected task group"))
	sb.WriteString("\n")
	sb.WriteString(row("d/Del", "Remove task group"))
	sb.WriteString("\n")
	sb.WriteString(row("S", "Open sync strategy selection"))
	sb.WriteString("\n")
	sb.WriteString(row("C", "Plan close selected task"))
	sb.WriteString("\n")
	sb.WriteString(row("P", "Scan prunable tasks"))
	sb.WriteString("\n")
	sb.WriteString(row("V", "Validate selected task"))
	sb.WriteString("\n")
	sb.WriteString(row("T", "Browse task tags"))
	sb.WriteString("\n")
	sb.WriteString(row("R", "Open <taskID>.sln in Rider"))
	sb.WriteString("\n")
	sb.WriteString(row("O", "Open <taskID>.code-workspace in VS Code"))
	sb.WriteString("\n")
	sb.WriteString(row(";", "Run shell command in selected task directory"))
	sb.WriteString("\n")
	sb.WriteString(row(",", "Show effective config"))
	sb.WriteString("\n")
	sb.WriteString(row("/", "Filter tasks"))
	sb.WriteString("\n")
	sb.WriteString(row("Enter", "View services (opens Services panel)"))
	sb.WriteString("\n")
	sb.WriteString(row("r", "Refresh tasks and repository cache"))
	sb.WriteString("\n\n")

	sb.WriteString(sectionStyle.Render("Services Panel:"))
	sb.WriteString("\n")
	sb.WriteString(row("a", "Add service to task"))
	sb.WriteString("\n")
	sb.WriteString(row("d/Del", "Remove service from task"))
	sb.WriteString("\n")
	if h.lazygitAvailable {
		sb.WriteString(row("g", "Open lazygit for selected service"))
		sb.WriteString("\n")
		sb.WriteString(row("m", "Open forge action menu"))
		sb.WriteString("\n")
		sb.WriteString(row("p", "Show pipeline status"))
		sb.WriteString("\n")
		sb.WriteString(row("v", "Validate current task"))
		sb.WriteString("\n")
	} else {
		sb.WriteString(row("P", "Push service (git push -u)"))
		sb.WriteString("\n")
		sb.WriteString(row("s", "Sync service (fetch + merge/rebase)"))
		sb.WriteString("\n")
		sb.WriteString(row("m", "Open forge action menu"))
		sb.WriteString("\n")
		sb.WriteString(row("p", "Show pipeline status"))
		sb.WriteString("\n")
		sb.WriteString(row("v", "Validate current task"))
		sb.WriteString("\n")
		sb.WriteString(row("Ctrl+s", "Stash service changes"))
		sb.WriteString("\n")
		sb.WriteString(row("Ctrl+u", "Unstash service changes"))
		sb.WriteString("\n")
	}
	sb.WriteString(row("Esc", "Back to tasks"))
	sb.WriteString("\n\n")

	sb.WriteString(sectionStyle.Render("Output Panel:"))
	sb.WriteString("\n")
	sb.WriteString(row("j/k", "Scroll up/down"))
	sb.WriteString("\n")
	sb.WriteString(row("g/G", "Top/bottom"))
	sb.WriteString("\n")
	sb.WriteString(row("mouse wheel", "Scroll (always active)"))
	sb.WriteString("\n")
	sb.WriteString(row("Esc", "Back to tasks"))
	sb.WriteString("\n\n")

	sb.WriteString(sectionStyle.Render("Global:"))
	sb.WriteString("\n")
	sb.WriteString(row("L", "Toggle log overlay"))
	sb.WriteString("\n")
	sb.WriteString(row("?", "Toggle this help"))
	sb.WriteString("\n")
	sb.WriteString(row(".", "System status (tools / forge)"))
	sb.WriteString("\n")
	sb.WriteString(row("q/Ctrl+C", "Quit"))
	sb.WriteString("\n\n")

	sb.WriteString(dimStyle.Render("[Esc] or [?] to close"))

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

func (h *HelpOverlay) maxScrollOffset() int {
	lines := len(h.contentLines())
	visible := h.visibleLines()
	if lines <= visible {
		return 0
	}
	return lines - visible
}

func (h *HelpOverlay) clampScroll() {
	if h.scrollOffset < 0 {
		h.scrollOffset = 0
	}
	maxOffset := h.maxScrollOffset()
	if h.scrollOffset > maxOffset {
		h.scrollOffset = maxOffset
	}
}

func (h *HelpOverlay) View() string {
	lines := h.contentLines()
	visible := h.visibleLines()
	h.clampScroll()

	end := min(len(lines), h.scrollOffset+visible)
	viewLines := lines[h.scrollOffset:end]
	if len(viewLines) == 0 {
		return ""
	}

	return strings.Join(viewLines, "\n")
}
