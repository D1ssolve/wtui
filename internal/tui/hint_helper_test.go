package tui

import (
	"github.com/charmbracelet/x/ansi"

	"github.com/D1ssolve/wtui/internal/tui/theme"
)

// hint is the plain text a "[Key] label" hint renders to, so assertions do not
// depend on how keycaps are drawn.
func hint(s string) string { return ansi.Strip(theme.HintLine(s)) }
