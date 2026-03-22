package tui

import "github.com/charmbracelet/lipgloss"

const (
	colorInactive = lipgloss.Color("#4A4A4A") // dark gray — inactive panel borders
	colorDirty    = lipgloss.Color("#F59E0B") // amber — dirty/modified service indicator
	colorDimText  = lipgloss.Color("#6B7280") // muted gray — secondary / dimmed text
	colorBoldText = lipgloss.Color("#F3F4F6") // near-white — bold / emphasized text
	colorNormal   = lipgloss.Color("#D1D5DB") // light gray — normal body text
	colorHeaderBg = lipgloss.Color("#1E1E2E") // deep background for header bar
)

type Styles struct {
	ActiveBorder   lipgloss.Style // rounded border, primary color foreground
	InactiveBorder lipgloss.Style // rounded border, dim gray foreground

	Header lipgloss.Style
	Footer lipgloss.Style

	NormalText lipgloss.Style
	DimText    lipgloss.Style
	BoldText   lipgloss.Style

	DirtyColor lipgloss.Style
}

func NewStyles() Styles {
	return Styles{
		ActiveBorder: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorPrimary),

		InactiveBorder: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorInactive),

		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			Background(colorHeaderBg).
			PaddingLeft(1).
			PaddingRight(1),

		Footer: lipgloss.NewStyle().
			Foreground(colorDimText),

		NormalText: lipgloss.NewStyle().
			Foreground(colorNormal),

		DimText: lipgloss.NewStyle().
			Foreground(colorDimText),

		BoldText: lipgloss.NewStyle().
			Bold(true).
			Foreground(colorBoldText),

		DirtyColor: lipgloss.NewStyle().
			Foreground(colorDirty),
	}
}
