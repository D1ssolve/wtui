package modal

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type HelpOverlay struct {
	lazygitAvailable bool
}

func NewHelpOverlayWithOptions(lazygitAvailable bool) *HelpOverlay {
	return &HelpOverlay{lazygitAvailable: lazygitAvailable}
}

func (h *HelpOverlay) Title() string { return "Keyboard Shortcuts" }

func (h *HelpOverlay) SetTerminalSize(width, height int) {}

func (h *HelpOverlay) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "esc", "?":
			return h, func() tea.Msg { return CloseModalMsg{} }
		}
	}
	return h, nil
}

func (h *HelpOverlay) View() string {
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
	sb.WriteString(row("P", "Push task (git push)"))
	sb.WriteString("\n")
	sb.WriteString(row("R", "Open <taskID>.sln in Rider"))
	sb.WriteString("\n")
	sb.WriteString(row("C", "Open <taskID>.code-workspace in VS Code"))
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
	} else {
		sb.WriteString(row("p", "Push service (git push -u)"))
		sb.WriteString("\n")
		sb.WriteString(row("s", "Sync service (fetch + merge/rebase)"))
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
	sb.WriteString(row("q/Ctrl+C", "Quit"))
	sb.WriteString("\n\n")

	sb.WriteString(dimStyle.Render("[Esc] or [?] to close"))

	return sb.String()
}
