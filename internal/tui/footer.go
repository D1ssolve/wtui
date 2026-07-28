package tui

import (
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
)

func renderFooter(m Model) string {
	var hints string

	switch m.focus {
	case FocusTasks:
		parts := []string{
			"[Enter] services",
			"[i] init",
			"[C] close",
			"[M] merge MRs",
		}
		parts = append(parts, "[.] status", "[?] help", "[q] quit")
		hints = joinFooterHints(parts)
	case FocusServices:
		parts := []string{
			"[a] add",
			"[m] forge",
			"[p] pipeline",
			"[v] validate",
			"[M] merge MRs",
			"[Esc] back",
			"[.] status",
			"[?] help",
		}
		hints = joinFooterHints(parts)
	case FocusOutput:
		hints = "[j/k] scroll  [g/G] top/bottom  [Esc] back"
	case FocusReleases:
		parts := []string{
			"[N] prepare release",
			"[r] refresh",
		}
		if rel := m.releasesPanel.SelectedRelease(); rel != nil {
			switch rel.Status {
			case domain.ReleaseStatusPrepared:
				parts = append(parts, "[F] promote")
			case domain.ReleaseStatusAwaitingMasterMerge:
				parts = append(parts, "[M] merge MRs")
			case domain.ReleaseStatusMasterMerged:
				parts = append(parts, "[F] finalize")
			}
		}
		parts = append(parts, "[?] help", "[q] quit")
		hints = joinFooterHints(parts)
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
