package tui

import "github.com/charmbracelet/lipgloss"

const appTitle = "wtui — git worktree manager"

// renderHeader returns the single-line header bar string, padded to the full
// terminal width.  It is a pure function with no side effects.
func renderHeader(m Model) string {
	// Build the title segment.
	title := m.styles.Header.Render(appTitle)

	// Pad the rendered title to fill the full terminal width so the background
	// color extends edge-to-edge.  lipgloss.PlaceHorizontal handles the ANSI
	// width accounting correctly (multi-byte runes, escape codes, etc.).
	return lipgloss.PlaceHorizontal(
		m.width,
		lipgloss.Left,
		title,
		lipgloss.WithWhitespaceBackground(colorHeaderBg),
	)
}
