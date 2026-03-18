package tui

import "github.com/charmbracelet/lipgloss"

// Primary palette — consistent brand color used across active panel borders and headers.
const (
	colorPrimary  = lipgloss.Color("#7C3AED") // violet — active panel / header accent
	colorInactive = lipgloss.Color("#4A4A4A") // dark gray — inactive panel borders
	colorDirty    = lipgloss.Color("#F59E0B") // amber — dirty/modified service indicator
	colorDimText  = lipgloss.Color("#6B7280") // muted gray — secondary / dimmed text
	colorBoldText = lipgloss.Color("#F3F4F6") // near-white — bold / emphasized text
	colorNormal   = lipgloss.Color("#D1D5DB") // light gray — normal body text
	colorHeaderBg = lipgloss.Color("#1E1E2E") // deep background for header bar
)

// Styles is the set of all lipgloss styles used by the TUI.
// Initialized once via NewStyles(); referenced throughout the package.
type Styles struct {
	// Panel borders
	ActiveBorder   lipgloss.Style // rounded border, primary color foreground
	InactiveBorder lipgloss.Style // rounded border, dim gray foreground

	// Header bar — 1 line fixed at the top
	Header lipgloss.Style

	// Footer hint bar — 1 line fixed at the bottom
	Footer lipgloss.Style

	// Text utilities
	NormalText lipgloss.Style
	DimText    lipgloss.Style
	BoldText   lipgloss.Style

	// Dirty-service indicator
	DirtyColor lipgloss.Style
}

// NewStyles constructs the application's style constants.
// Called once inside tui.New().
func NewStyles() Styles {
	return Styles{
		// Active panel: rounded border, primary violet foreground
		ActiveBorder: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPrimary),

		// Inactive panel: rounded border, dim gray foreground
		InactiveBorder: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorInactive),

		// Header: bold, primary foreground on deep background
		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary).
			Background(colorHeaderBg).
			PaddingLeft(1).
			PaddingRight(1),

		// Footer: dimmed secondary text
		Footer: lipgloss.NewStyle().
			Foreground(colorDimText),

		// Text styles
		NormalText: lipgloss.NewStyle().
			Foreground(colorNormal),

		DimText: lipgloss.NewStyle().
			Foreground(colorDimText),

		BoldText: lipgloss.NewStyle().
			Bold(true).
			Foreground(colorBoldText),

		// Amber used on service rows with uncommitted changes
		DirtyColor: lipgloss.NewStyle().
			Foreground(colorDirty),
	}
}
