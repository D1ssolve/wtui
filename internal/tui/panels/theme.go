package panels

import "github.com/charmbracelet/lipgloss"

// panelColorPrimary is the app-wide violet accent, shared across all panels.
// It mirrors tui.ColorPrimary — both must equal #7C3AED.
const panelColorPrimary = lipgloss.Color("#7C3AED")

// Shared color palette used across all panel files.
// Keep these in sync with tui.Styles and modal color constants.
const (
	colorInactive = lipgloss.Color("#4A4A4A") // dark gray  — inactive panel borders
	colorDim      = lipgloss.Color("#6B7280") // muted gray — secondary / dimmed text
	colorNormal   = lipgloss.Color("#D1D5DB") // light gray — normal body text
	colorBold     = lipgloss.Color("#F3F4F6") // near-white — emphasized text
	colorDirty    = lipgloss.Color("#F59E0B") // amber      — dirty / modified indicator
)
