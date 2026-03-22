package modal

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Modal interface {
	Update(msg tea.Msg) (Modal, tea.Cmd)
	View() string
	Title() string
}

const (
	// modalColorBorder matches tui.ColorPrimary (#7C3AED) — the app-wide violet accent.
	modalColorBorder  = lipgloss.Color("#7C3AED")
	modalColorDim     = lipgloss.Color("#6B7280") // muted gray — secondary text / hints
	modalColorNormal  = lipgloss.Color("#D1D5DB") // light gray — body text
	modalColorWarning = lipgloss.Color("#F59E0B") // amber — dirty service warnings
	modalColorGray    = lipgloss.Color("#4A4A4A") // dark gray — disabled text
)

func boxStyle(innerWidth int) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(modalColorBorder).
		Width(innerWidth).
		Padding(0, 1)
}

func OverlayView(content string, termW, termH int) string {
	innerW := max(termW*50/100, 50)
	maxInner := max(termW-4, 1)
	if innerW > maxInner {
		innerW = maxInner
	}

	boxed := boxStyle(innerW).Render(content)
	return lipgloss.Place(termW, termH, lipgloss.Center, lipgloss.Center, boxed)
}
