package tui

import "github.com/charmbracelet/lipgloss"

const appTitle = "wtui — git worktree manager"

func renderHeader(m Model) string {
	title := m.styles.Header.Render(appTitle)

	return lipgloss.PlaceHorizontal(
		m.width,
		lipgloss.Left,
		title,
		lipgloss.WithWhitespaceBackground(colorHeaderBg),
	)
}
