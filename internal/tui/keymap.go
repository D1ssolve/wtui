package tui

import "github.com/charmbracelet/bubbles/key"

type KeyMap struct {
	Tab key.Binding

	PanelTasks    key.Binding
	PanelServices key.Binding
	PanelOutput   key.Binding
	PanelReleases key.Binding

	Quit      key.Binding
	ForceQuit key.Binding
	Refresh   key.Binding
	Help      key.Binding
	Escape    key.Binding

	ToggleLogs key.Binding

	CloseTask       key.Binding
	PruneTask       key.Binding
	ValidateTask    key.Binding
	TagBrowser      key.Binding
	ForgeMenu       key.Binding
	NewRelease      key.Binding
	MergeMRs        key.Binding
	ReleaseAction   key.Binding
	RetryRelease    key.Binding
	ServiceValidate key.Binding
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
		PanelReleases: key.NewBinding(
			key.WithKeys("3"),
			key.WithHelp("3", "focus releases"),
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
		CloseTask: key.NewBinding(
			key.WithKeys("C"),
			key.WithHelp("C", "close task"),
		),
		PruneTask: key.NewBinding(
			key.WithKeys("P"),
			key.WithHelp("P", "prune tasks"),
		),
		ValidateTask: key.NewBinding(
			key.WithKeys("V"),
			key.WithHelp("V", "validate task"),
		),
		TagBrowser: key.NewBinding(
			key.WithKeys("T"),
			key.WithHelp("T", "browse tags"),
		),
		ForgeMenu: key.NewBinding(
			key.WithKeys("m"),
			key.WithHelp("m", "forge menu"),
		),
		NewRelease: key.NewBinding(
			key.WithKeys("N"),
			key.WithHelp("N", "new release"),
		),
		MergeMRs: key.NewBinding(
			key.WithKeys("M"),
			key.WithHelp("M", "merge ready MRs"),
		),
		ReleaseAction: key.NewBinding(
			key.WithKeys("F"),
			key.WithHelp("F", "promote/finalize release"),
		),
		RetryRelease: key.NewBinding(
			key.WithKeys("R"),
			key.WithHelp("R", "retry release"),
		),
		ServiceValidate: key.NewBinding(
			key.WithKeys("v"),
			key.WithHelp("v", "validate task"),
		),
	}
}
