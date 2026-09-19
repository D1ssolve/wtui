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

func overlayContentSize(termW, termH int) (int, int) {
	style := boxStyle(0)
	width := min(max(termW*50/100, 50), max(termW-4, 0)) - style.GetHorizontalPadding()
	return max(0, min(width, termW-style.GetHorizontalFrameSize())),
		max(0, min(max(termH*70/100, 10), termH-style.GetVerticalFrameSize()))
}

func OverlayView(content string, termW, termH int) string {
	width := min(max(termW*50/100, 50), max(termW-4, 1))
	height := min(max(termH*70/100, 10), termH-2)
	boxed := boxStyle(width).Height(height).Render(content)
	return lipgloss.Place(termW, termH, lipgloss.Center, lipgloss.Center, boxed)
}
