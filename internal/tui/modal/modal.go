package modal

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/D1ssolve/wtui/internal/tui/theme"
)

type Modal interface {
	Update(msg tea.Msg) (Modal, tea.Cmd)
	View() string
	Title() string

	SetTerminalSize(width, height int)
}

const (
	modalColorBorder  = theme.Primary
	modalColorDim     = theme.TextMuted
	modalColorNormal  = theme.Text
	modalColorWarning = theme.Warning
	modalColorDanger  = theme.Danger
	modalColorSuccess = theme.Success
	modalColorInfo    = theme.Info
)

func boxStyle(innerWidth int) lipgloss.Style {
	return theme.FocusedGlassBorder(modalColorBorder).
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
