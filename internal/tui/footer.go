package tui

import "fmt"

func renderFooter(m Model) string {
	if m.shellInput != nil {
		prompt := "; " + m.shellInput.input + "█"
		return m.styles.Footer.Render(prompt)
	}

	var hints string

	switch m.focus {
	case FocusTasks:
		hints = "[i] init  [d] remove  [S] sync strategy  [P] push task  [R] Rider  [;] shell  [,] config  [/] filter  [r] refresh tasks/repos  [L] logs  [Tab] output  [Enter] services  [?] help  [q] quit"
	case FocusServices:
		hints = "[a] add service  [p] push service  [d] remove service  [ctrl+s] stash  [ctrl+u] unstash  [Esc] back  [?] help"
	case FocusOutput:
		hints = "[j/k] scroll  [g/G] top/bottom  [Esc] tasks  [Tab] back"
	default:
		hints = "[q] quit"
	}

	if m.opRunning {
		hints = fmt.Sprintf("%s  %s", m.spinner.View(), hints)
	}

	return m.styles.Footer.Render(hints)
}
