package tui

import "fmt"

func renderFooter(m Model) string {
	var hints string

	switch m.focus {
	case FocusTasks:
		hints = "[i] init  [c] clone  [d] remove  [S] sync  [P] push  [R] Rider  [C] VS Code  [?] help  [,] config  [/] filter  [Tab] services  [q] quit"
	case FocusServices:
		if m.lazygitAvailable {
			hints = "[a] add service  [d] remove service  [g] lazygit  [Esc] back  [?] help"
		} else {
			hints = "[a] add service  [s] sync service  [p] push service  [d] remove service  [ctrl+s] stash  [ctrl+u] unstash  [Esc] back  [?] help"
		}
	case FocusOutput:
		hints = "[j/k] scroll  [g/G] top/bottom  [Esc] back"
	default:
		hints = "[q] quit"
	}

	if m.opRunning {
		hints = fmt.Sprintf("%s  %s", m.spinner.View(), hints)
	}

	return m.styles.Footer.Render(hints)
}
