package panels

import "github.com/charmbracelet/lipgloss"

const panelColorPrimary = lipgloss.Color("#7C3AED")

const (
	colorInactive = lipgloss.Color("#4A4A4A")
	colorDim      = lipgloss.Color("#6B7280")
	colorNormal   = lipgloss.Color("#D1D5DB")
	colorBold     = lipgloss.Color("#F3F4F6")
	colorDirty    = lipgloss.Color("#F59E0B")
)

var (
	badgeStyle = lipgloss.NewStyle().
			Foreground(colorBold).
			Background(lipgloss.Color("#374151")).
			Padding(0, 1)

	branchTypeFeatureStyle = badgeStyle.Copy().Background(lipgloss.Color("#2563EB"))
	branchTypeHotfixStyle  = badgeStyle.Copy().Background(lipgloss.Color("#DC2626"))
	branchTypeReleaseStyle = badgeStyle.Copy().Background(lipgloss.Color("#7C3AED"))
	branchTypeBugfixStyle  = badgeStyle.Copy().Background(lipgloss.Color("#D97706"))
	branchTypeChoreStyle   = badgeStyle.Copy().Background(lipgloss.Color("#4B5563"))
	forgeBadgeStyle        = badgeStyle.Copy().Background(lipgloss.Color("#065F46"))
)
