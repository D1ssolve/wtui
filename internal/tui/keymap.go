package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap holds all global keybindings for the wtui TUI.
// Each binding pairs one or more actual key sequences with a human-readable
// help string used in the footer hint bar.
type KeyMap struct {
	// Navigation
	Up    key.Binding
	Down  key.Binding
	Left  key.Binding
	Right key.Binding

	// Confirmation / submission
	Enter key.Binding

	// Panel focus cycling
	Tab      key.Binding
	ShiftTab key.Binding

	// Application control
	Quit        key.Binding // q
	ForceQuit   key.Binding // ctrl+c
	Refresh     key.Binding // r
	Help        key.Binding // ?
	Escape      key.Binding // esc
	ForceToggle key.Binding // f

	// Task operations (Tasks panel context)
	InitTask      key.Binding // i — open Init Task dialog
	AddService    key.Binding // a — open Add Service dialog
	RemoveTask    key.Binding // d / Delete — open Remove confirmation
	OpenWorkspace key.Binding // o — open workspace in editor
	GenerateSln   key.Binding // s — generate .sln file
	Filter        key.Binding // / — activate filter mode
	ShellExec     key.Binding // ; — run shell command in task directory
}

// DefaultKeyMap returns the application-wide default keybindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		Left: key.NewBinding(
			key.WithKeys("left", "h"),
			key.WithHelp("←/h", "left"),
		),
		Right: key.NewBinding(
			key.WithKeys("right", "l"),
			key.WithHelp("→/l", "right"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "select"),
		),
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "next panel"),
		),
		ShiftTab: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("shift+tab", "prev panel"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q"),
			key.WithHelp("q", "quit"),
		),
		ForceQuit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "refresh"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Escape: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back"),
		),
		ForceToggle: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "toggle force"),
		),
		InitTask: key.NewBinding(
			key.WithKeys("i"),
			key.WithHelp("i", "init task"),
		),
		AddService: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "add service"),
		),
		RemoveTask: key.NewBinding(
			key.WithKeys("d", "delete"),
			key.WithHelp("d", "remove"),
		),
		OpenWorkspace: key.NewBinding(
			key.WithKeys("o"),
			key.WithHelp("o", "open workspace"),
		),
		GenerateSln: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "generate sln"),
		),
		Filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter"),
		),
		ShellExec: key.NewBinding(
			key.WithKeys(";"),
			key.WithHelp(";", "shell"),
		),
	}
}
