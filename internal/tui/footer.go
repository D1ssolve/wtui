package tui

import (
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/tui/theme"
	"github.com/charmbracelet/lipgloss"
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
		hints = renderFooterHints(parts, m.width)
	case FocusServices:
		parts := []string{
			"[a] add",
			"[m] forge",
			"[v] validate",
			"[Esc] back",
			"[.] status",
			"[?] help",
		}
		hints = renderFooterHints(parts, m.width)
	case FocusOutput:
		hints = renderFooterHints([]string{"[j/k] scroll", "[g/G] top/bottom", "[Esc] back"}, m.width)
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
		hints = renderFooterHints(parts, m.width)
	default:
		hints = renderFooterHints([]string{"[q] quit", "[?] help"}, m.width)
	}

	if m.opRunning {
		hints = fmt.Sprintf("%s  %s", m.spinner.View(), hints)
	}

	return m.styles.Footer.Render(hints)
}

func renderFooterHints(parts []string, width int) string {
	selected := append([]string(nil), parts...)
	if width > 0 && width < 100 && len(selected) > 4 {
		selected = compactFooterHints(selected)
	}
	render := func(values []string) string {
		styled := make([]string, 0, len(values))
		for _, value := range values {
			key, label, ok := strings.Cut(value, " ")
			if !ok {
				styled = append(styled, lipgloss.NewStyle().Foreground(theme.TextMuted).Render(value))
				continue
			}
			key = lipgloss.NewStyle().Foreground(theme.Text).Render(key)
			label = lipgloss.NewStyle().Foreground(theme.TextMuted).Render(label)
			styled = append(styled, key+" "+label)
		}
		return strings.Join(styled, "  ")
	}
	for len(selected) > 1 && width > 0 && lipgloss.Width(render(selected)) > width-2 {
		selected = append(selected[:len(selected)-2], selected[len(selected)-1])
	}
	return render(selected)
}

func compactFooterHints(parts []string) []string {
	result := make([]string, 0, 4)
	result = append(result, parts[0])
	for _, part := range parts[1:] {
		if strings.HasPrefix(part, "[?]") || strings.HasPrefix(part, "[Esc]") || strings.HasPrefix(part, "[q]") {
			result = append(result, part)
		}
	}
	return result
}
