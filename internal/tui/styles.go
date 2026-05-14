package tui

import "github.com/charmbracelet/lipgloss"

const (
	colorDimText  = lipgloss.Color("#6B7280")
	colorHeaderBg = lipgloss.Color("#1E1E2E")
)

type Styles struct {
	Header lipgloss.Style
	Footer lipgloss.Style
}

func NewStyles() Styles {
	return Styles{
		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			Background(colorHeaderBg).
			PaddingLeft(1).
			PaddingRight(1),

		Footer: lipgloss.NewStyle().
			Foreground(colorDimText),
	}
}
