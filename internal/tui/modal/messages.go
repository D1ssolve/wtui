package modal

// SubmitInitMsg is emitted by the InitDialog when the user confirms the form.
// Services is parsed from comma- or space-separated input.
type SubmitInitMsg struct {
	TaskID       string
	Services     []string // parsed from comma or space separated input
	BranchPrefix string
	BaseBranch   string
}

// SubmitAddMsg is emitted by the AddDialog when the user confirms the form.
// Services is parsed from comma- or space-separated input.
type SubmitAddMsg struct {
	TaskID   string
	Services []string // parsed from comma or space separated input
}

// SubmitRemoveMsg is emitted by the RemoveDialog when the user confirms removal.
// Force is true if the user checked the "Force remove" checkbox.
type SubmitRemoveMsg struct {
	TaskID string
	Force  bool
}

// CloseModalMsg is emitted by any modal when the user presses Esc (or the
// equivalent cancel keybinding).  The parent model sets m.modal = nil on
// receipt of this message.
type CloseModalMsg struct{}

// SubmitOpenFileMsg is emitted by OpenDialog when the user confirms a file+app selection.
type SubmitOpenFileMsg struct {
	Path string // absolute path to file
	App  string // binary path or name
}
