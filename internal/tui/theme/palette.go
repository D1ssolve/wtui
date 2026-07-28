package theme

import "github.com/charmbracelet/lipgloss"

const (
	Background     = lipgloss.Color("#090E1A")
	Surface        = lipgloss.Color("#0D1424")
	SurfaceRaised  = lipgloss.Color("#111A2D")
	Border         = lipgloss.Color("#50617A")
	BorderStrong   = lipgloss.Color("#8DAECC")
	Primary        = lipgloss.Color("#C5A6FF")
	PrimaryMuted   = lipgloss.Color("#272140")
	Text           = lipgloss.Color("#F1F6FF")
	TextMuted      = lipgloss.Color("#A8B5CA")
	Success        = lipgloss.Color("#68E6AE")
	Warning        = lipgloss.Color("#FFD27A")
	Danger         = lipgloss.Color("#FF8299")
	Info           = lipgloss.Color("#75D8FF")
	GlassHighlight = lipgloss.Color("#D7ECFF")
	GlassShadow    = lipgloss.Color("#6B7D99")
)

func GlassBorder(highlight lipgloss.TerminalColor) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderTopForeground(highlight).
		BorderLeftForeground(highlight).
		BorderBottomForeground(GlassShadow).
		BorderRightForeground(GlassShadow)
}

func FocusedGlassBorder(highlight lipgloss.TerminalColor) lipgloss.Style {
	return GlassBorder(highlight).Border(lipgloss.ThickBorder())
}
