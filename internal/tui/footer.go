package tui

import "fmt"

// renderFooter returns the single-line footer hint bar string.
// The displayed key hints are context-sensitive, changing based on which panel
// currently has focus.  It is a pure function with no side effects.
func renderFooter(m Model) string {
	// Shell input prompt overrides the normal footer while active.
	if m.shellInput != nil {
		prompt := "; " + m.shellInput.input + "█"
		return m.styles.Footer.Render(prompt)
	}

	var hints string

	switch m.focus {
	case FocusTasks:
		hints = "[i] init  [d] remove  [o] open  [s] sln  [;] shell  [/] filter  [Tab] services  [?] help  [q] quit"
	case FocusServices:
		hints = "[a] add service  [Esc] back  [Tab] output  [?] help"
	case FocusOutput:
		hints = "[j/k] scroll  [g/G] top/bottom  [Esc] back"
	default:
		hints = "[q] quit"
	}

	// Append spinner indicator when an operation is running.
	if m.opRunning {
		hints = fmt.Sprintf("%s  %s", m.spinner.View(), hints)
	}

	return m.styles.Footer.Render(hints)
}
