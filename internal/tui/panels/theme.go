package panels

import (
	"github.com/charmbracelet/lipgloss"

	uitheme "github.com/D1ssolve/wtui/internal/tui/theme"
)

const panelColorPrimary = uitheme.Primary

const (
	colorDim    = uitheme.TextMuted
	colorNormal = uitheme.Text
	colorBold   = uitheme.Text
	colorDirty  = uitheme.Warning
)

var (
	badgeStyle = lipgloss.NewStyle().
			Foreground(colorBold).
			Padding(0, 1)

	branchTypeFeatureStyle = badgeStyle.Copy().Foreground(uitheme.Info)
	branchTypeHotfixStyle  = badgeStyle.Copy().Foreground(uitheme.Danger)
	branchTypeReleaseStyle = badgeStyle.Copy().Foreground(uitheme.Primary)
	forgeBadgeStyle        = badgeStyle.Copy().Foreground(uitheme.Success)
)
