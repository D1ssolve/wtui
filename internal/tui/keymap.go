package tui

import "github.com/charmbracelet/bubbles/key"

type KeyMap struct {
	Up    key.Binding
	Down  key.Binding
	Left  key.Binding
	Right key.Binding

	Enter key.Binding

	Tab      key.Binding
	ShiftTab key.Binding

	Quit        key.Binding // q
	ForceQuit   key.Binding // ctrl+c
	Refresh     key.Binding // r
	Help        key.Binding // ?
	Escape      key.Binding // esc
	ForceToggle key.Binding // f

	InitTask    key.Binding // i — open Init Task dialog
	AddService  key.Binding // a — open Add Service dialog
	RemoveTask  key.Binding // d / Delete — open Remove confirmation
	GenerateSln key.Binding // s — generate .sln file
	Filter      key.Binding // / — activate filter mode
	ShellExec   key.Binding // ; — run shell command in task directory
	PushTask    key.Binding // P — push all services in task
	ToggleLogs  key.Binding // L — toggle log overlay
}

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
		PushTask: key.NewBinding(
			key.WithKeys("P"),
			key.WithHelp("P", "push"),
		),
		ToggleLogs: key.NewBinding(
			key.WithKeys("L"),
			key.WithHelp("L", "logs"),
		),
	}
}
