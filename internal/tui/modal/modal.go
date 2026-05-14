package modal

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Modal interface {
	Update(msg tea.Msg) (Modal, tea.Cmd)
	View() string
	Title() string

	SetTerminalSize(width, height int)
}

const (
	modalColorBorder  = lipgloss.Color("#7C3AED")
	modalColorDim     = lipgloss.Color("#6B7280")
	modalColorNormal  = lipgloss.Color("#D1D5DB")
	modalColorWarning = lipgloss.Color("#F59E0B")
	modalColorGray    = lipgloss.Color("#4A4A4A")
)

func boxStyle(innerWidth int) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(modalColorBorder).
		Width(innerWidth).
		Padding(0, 1)
}

func OverlayView(content string, termW, termH, maxContentH int) string {
	innerW := max(termW*50/100, 50)
	maxInnerW := max(termW-4, 1)
	if innerW > maxInnerW {
		innerW = maxInnerW
	}

	innerH := maxContentH
	if innerH > termH-2 {
		innerH = termH - 2
	}

	boxed := boxStyle(innerW).Height(innerH).Render(content)
	return lipgloss.Place(termW, termH, lipgloss.Center, lipgloss.Center, boxed)
}
