package tui

import "github.com/charmbracelet/bubbles/key"

type KeyMap struct {
	Tab key.Binding

	PanelTasks    key.Binding
	PanelServices key.Binding
	PanelOutput   key.Binding

	Quit      key.Binding
	ForceQuit key.Binding
	Refresh   key.Binding
	Help      key.Binding
	Escape    key.Binding

	ToggleLogs key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "next panel"),
		),
		PanelTasks: key.NewBinding(
			key.WithKeys("1"),
			key.WithHelp("1", "focus tasks"),
		),
		PanelServices: key.NewBinding(
			key.WithKeys("2"),
			key.WithHelp("2", "focus services"),
		),
		PanelOutput: key.NewBinding(
			key.WithKeys("0"),
			key.WithHelp("0", "focus output"),
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
			key.WithHelp("r", "refresh tasks/repos"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Escape: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back"),
		),
		ToggleLogs: key.NewBinding(
			key.WithKeys("L"),
			key.WithHelp("L", "logs"),
		),
	}
}
