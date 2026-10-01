package modal

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/domain"
)

type HelpOverlay struct {
	lazygitAvailable bool
	workflowTitle    string
	workflow         *domain.WorkflowSummary
	scrollOffset     int
	terminalWidth    int
	terminalHeight   int
}

func NewHelpOverlayWithOptions(lazygitAvailable bool) *HelpOverlay {
	return &HelpOverlay{lazygitAvailable: lazygitAvailable}
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

	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)

	var sb strings.Builder

	if workflowLines := h.renderWorkflow(); len(workflowLines) > 0 {
		sb.WriteString(strings.Join(workflowLines, "\n"))
		sb.WriteString("\n\n")
	}

	sb.WriteString(titleStyle.Render("Keyboard Shortcuts"))
	sb.WriteString("\n\n")
	sb.WriteString(sectionStyle.Render("Global:"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("Tab / 1 / 2 / 3 / 0", "Move focus: next / Tasks / Services / Releases / Output"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("r", "Refresh tasks, releases, and repository cache"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("L", "Toggle logs (commands / application)"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("?", "Toggle this help"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow(".", "System status (tools / forge)"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("q / Ctrl+C", "Quit"))
	sb.WriteString("\n\n")
	sb.WriteString(sectionStyle.Render("Log Overlay:"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("Tab", "Switch Commands / Application"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("d", "Enable session DEBUG / restore configured level"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("f", "Selected task / all events"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("j/k, g/G", "Scroll, top / bottom (follow new events)"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("L / Esc", "Close logs; recording level stays active"))
	sb.WriteString("\n\n")

	sb.WriteString(sectionStyle.Render("Tasks Panel:"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("j/k, arrows", "Move selection"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("g/G, h/l", "First/last task, previous/next page"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("i", "Init new task group"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("c", "Clone selected task group"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("F", "Convert hotfix to feature"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("d/Del", "Remove task group"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("S", "Open sync strategy selection"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("C", "Plan close selected task"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("D / P", "Open cleanup review (scan, select, confirm)"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("V", "Validate selected task"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("M", "Inspect and merge ready task MRs"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("T", "Browse task tags"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("R", "Open <taskID>.sln in Rider"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("O", "Open <taskID>.code-workspace in VS Code"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow(";", "Run shell command in selected task directory"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow(",", "Show effective config"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("/", "Filter tasks"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("Enter", "View services (opens Services panel)"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("r", "Refresh tasks and repository cache"))
	sb.WriteString("\n\n")

	sb.WriteString(sectionStyle.Render("Services Panel:"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("j/k, arrows", "Move selection"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("h/l", "Previous/next page"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("a", "Add service to task"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("d/Del", "Remove service from task"))
	sb.WriteString("\n")
	if h.lazygitAvailable {
		sb.WriteString(h.shortcutRow("g", "Open lazygit for selected service"))
		sb.WriteString("\n")
	}
	sb.WriteString(h.shortcutRow("m", "Open forge action menu"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("v", "Validate current task"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("Esc", "Back to tasks"))
	sb.WriteString("\n\n")

	sb.WriteString(sectionStyle.Render("Releases Panel:"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("j/k, arrows", "Move selection"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("N", "Create release"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("F", "Promote or finalize selected release when available"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("R", "Retry failed recoverable release"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("O", "Open selected release folder in configured editor"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("I", "Open selected release folder in Rider"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("M", "Merge selected release MRs when available"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("D", "Open cleanup review (scan, select, confirm)"))
	sb.WriteString("\n\n")
	sb.WriteString(sectionStyle.Render("Release Confirmation:"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("j/k, arrows, g/G", "Scroll preview; top/bottom"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("Enter/y", "Execute release"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("Esc/n", "Cancel"))
	sb.WriteString("\n\n")

	sb.WriteString(sectionStyle.Render("Output Panel:"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("j/k", "Scroll up/down"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("g/G", "Top/bottom"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("mouse wheel", "Scroll (always active)"))
	sb.WriteString("\n")
	sb.WriteString(h.shortcutRow("Esc", "Back to tasks"))
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
