package modal

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── HelpOverlay ───────────────────────────────────────────────────────────────

// HelpOverlay is a full-screen overlay that shows all keyboard shortcuts.
// It is opened with `?` and closed with `?` or `Esc`.
type HelpOverlay struct{}

// NewHelpOverlay creates a HelpOverlay.
func NewHelpOverlay() *HelpOverlay { return &HelpOverlay{} }

// Title implements Modal.
func (h *HelpOverlay) Title() string { return "Keyboard Shortcuts" }

// Update implements Modal.
// Both `?` and `Esc` close the overlay by emitting CloseModalMsg.
func (h *HelpOverlay) Update(msg tea.Msg) (Modal, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "esc", "?":
			return h, func() tea.Msg { return CloseModalMsg{} }
		}
	}
	return h, nil
}

// View implements Modal.
//
// Renders a two-column keybinding reference grouped by panel.
func (h *HelpOverlay) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(modalColorBorder)

	sectionStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(modalColorNormal)

	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#A78BFA")). // soft violet for key tokens
		Width(16)

	descStyle := lipgloss.NewStyle().
		Foreground(modalColorNormal)

	dimStyle := lipgloss.NewStyle().Foreground(modalColorDim)

	// row renders a single keybinding line.
	row := func(key, desc string) string {
		return "  " + keyStyle.Render(key) + descStyle.Render(desc)
	}

	var sb strings.Builder

	// ── Title ──────────────────────────────────────────────────────────────
	sb.WriteString(titleStyle.Render("Keyboard Shortcuts"))
	sb.WriteString("\n\n")

	// ── Tasks Panel ────────────────────────────────────────────────────────
	sb.WriteString(sectionStyle.Render("Tasks Panel:"))
	sb.WriteString("\n")
	sb.WriteString(row("i", "Init new task group"))
	sb.WriteString("\n")
	sb.WriteString(row("d/Del", "Remove task group"))
	sb.WriteString("\n")
	sb.WriteString(row("o", "Open file picker"))
	sb.WriteString("\n")
	sb.WriteString(row("s", "Generate .sln"))
	sb.WriteString("\n")
	sb.WriteString(row(";", "Run shell command in task dir"))
	sb.WriteString("\n")
	sb.WriteString(row("/", "Filter tasks"))
	sb.WriteString("\n")
	sb.WriteString(row("Enter", "View services (opens Services panel)"))
	sb.WriteString("\n")
	sb.WriteString(row("r", "Refresh"))
	sb.WriteString("\n\n")

	// ── Services Panel ─────────────────────────────────────────────────────
	sb.WriteString(sectionStyle.Render("Services Panel:"))
	sb.WriteString("\n")
	sb.WriteString(row("a", "Add service to task"))
	sb.WriteString("\n")
	sb.WriteString(row("Esc", "Back to tasks"))
	sb.WriteString("\n\n")

	// ── Output Panel ───────────────────────────────────────────────────────
	sb.WriteString(sectionStyle.Render("Output Panel:"))
	sb.WriteString("\n")
	sb.WriteString(row("j/k", "Scroll up/down"))
	sb.WriteString("\n")
	sb.WriteString(row("g/G", "Top/bottom"))
	sb.WriteString("\n")
	sb.WriteString(row("Esc", "Back to tasks"))
	sb.WriteString("\n\n")

	// ── Global ─────────────────────────────────────────────────────────────
	sb.WriteString(sectionStyle.Render("Global:"))
	sb.WriteString("\n")
	sb.WriteString(row("Tab", "Tasks ↔ Output"))
	sb.WriteString("\n")
	sb.WriteString(row("Shift+Tab", "Output ↔ Tasks"))
	sb.WriteString("\n")
	sb.WriteString(row("?", "Toggle this help"))
	sb.WriteString("\n")
	sb.WriteString(row("q/Ctrl+C", "Quit"))
	sb.WriteString("\n\n")

	// ── Close hint ─────────────────────────────────────────────────────────
	sb.WriteString(dimStyle.Render("[Esc] or [?] to close"))

	return sb.String()
}
