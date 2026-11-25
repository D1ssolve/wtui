package tui

import (
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/tui/theme"
	"github.com/charmbracelet/lipgloss"
)

func renderFooter(m Model) string {
	if m.shellInput != nil {
		return m.styles.Footer.Render("; " + m.shellInput.input + "█")
	}
	if hint := m.operationCancelHint(); hint != "" {
		return m.styles.Footer.Render(hint)
	}

	hints := renderFooterHints(fitFooterHints(footerHints(m), m.width))
	if m.opRunning {
		hints = fmt.Sprintf("%s  %s", m.spinner.View(), hints)
	}
	return m.styles.Footer.Render(hints)
}

func renderFooterHints(hints []footerHint) string {
	label := lipgloss.NewStyle().Foreground(theme.TextMuted)
	next := lipgloss.NewStyle().Foreground(theme.Primary).Bold(true)
	styled := make([]string, 0, len(hints))
	for _, hint := range hints {
		part := theme.Keycaps(hint.key) + " " + label.Render(hint.label)
		if hint.next {
			part = next.Render("▶") + " " + theme.Keycaps(hint.key) + " " + next.Render(hint.label)
		}
		styled = append(styled, part)
	}
	return strings.Join(styled, "  ")
}

// fitFooterHints drops unpinned hints from the end until the line fits, so
// help, the command palette and the next workflow step are always visible.
func fitFooterHints(hints []footerHint, width int) []footerHint {
	selected := append([]footerHint(nil), hints...)
	for width > 0 && lipgloss.Width(renderFooterHints(selected)) > width-2 {
		drop := -1
		for i := len(selected) - 1; i >= 0; i-- {
			if !selected[i].pinned {
				drop = i
				break
			}
		}
		if drop < 0 {
			break
		}
		selected = append(selected[:drop], selected[drop+1:]...)
	}
	return selected
}
