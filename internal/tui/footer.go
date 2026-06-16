package tui

import (
	"fmt"
	"strings"
)

func renderFooter(m Model) string {
	var hints string

	switch m.focus {
	case FocusTasks:
		parts := []string{
			"[Enter] services",
			"[i] init",
			"[C] close",
		}
		parts = append(parts, "[.] status", "[?] help", "[q] quit")
		hints = joinFooterHints(parts)
	case FocusServices:
		parts := []string{
			"[a] add",
			"[m] forge",
			"[p] pipeline",
			"[v] validate",
			"[Esc] back",
			"[.] status",
			"[?] help",
		}
		hints = joinFooterHints(parts)
	case FocusOutput:
		hints = "[j/k] scroll  [g/G] top/bottom  [Esc] back"
	case FocusReleases:
		hints = joinFooterHints([]string{
			"[N] new release",
			"[r] refresh",
			"[?] help",
			"[q] quit",
		})
	default:
		hints = "[q] quit  [?] help"
	}

	if m.opRunning {
		hints = fmt.Sprintf("%s  %s", m.spinner.View(), hints)
	}

	return m.styles.Footer.Render(hints)
}

func joinFooterHints(parts []string) string {
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(p)
	}
	return b.String()
}
