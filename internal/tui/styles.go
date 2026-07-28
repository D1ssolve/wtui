package tui

import "github.com/charmbracelet/lipgloss"

import "github.com/D1ssolve/wtui/internal/tui/theme"

const colorDimText = theme.TextMuted

type Styles struct {
	Header lipgloss.Style
	Footer lipgloss.Style
}

func NewStyles() Styles {
	return Styles{
		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			Padding(0, 1),

		Footer: lipgloss.NewStyle().
			Foreground(colorDimText).
			PaddingLeft(1).
			PaddingRight(1),
	}
}
