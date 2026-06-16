package tui

import (
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/gitflow"
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
		if shouldShowPromoteHint(m) {
			parts = append(parts, "[Q] promote")
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

func shouldShowPromoteHint(m Model) bool {
	selected := m.tasksPanel.SelectedTask()
	if selected == nil || selected.ParentID != "" || selected.Phase != string(gitflow.BranchTypeFeature) {
		return false
	}
	if m.flow == nil {
		return false
	}
	_, ok := m.flow.BranchTypes[gitflow.BranchTypeRelease]
	return ok
}
