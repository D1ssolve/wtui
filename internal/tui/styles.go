package tui

import "github.com/charmbracelet/lipgloss"

import "github.com/D1ssolve/wtui/internal/tui/theme"

const colorDimText = theme.TextMuted

type Styles struct {
	Footer lipgloss.Style
}

func NewStyles() Styles {
	return Styles{
		Footer: lipgloss.NewStyle().
			Foreground(colorDimText).
			PaddingLeft(1).
			PaddingRight(1),
	}
}
