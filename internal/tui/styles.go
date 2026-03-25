package tui

import "github.com/charmbracelet/lipgloss"

const (
	colorDimText  = lipgloss.Color("#6B7280") // muted gray — footer text
	colorHeaderBg = lipgloss.Color("#1E1E2E") // deep background for header bar
)

// Styles holds the pre-built lipgloss styles used directly by the root TUI
// model (header and footer). Panel-level styles live in internal/tui/panels/theme.go;
// modal-level styles live in internal/tui/modal/modal.go.
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
