package panels

// FocusServicesMsg is emitted by the Tasks panel when the user presses Enter
// on a selected task, requesting focus to shift to the Services panel.
type FocusServicesMsg struct{ TaskID string }

// FocusTasksMsg is emitted by the Services or Output panels when the user
// presses Esc, requesting focus to return to the Tasks panel.
type FocusTasksMsg struct{}

// OpenInitDialogMsg is emitted when the user presses [i] in the Tasks panel,
// requesting the Init Task dialog to be opened.
type OpenInitDialogMsg struct{}

// OpenRemoveDialogMsg is emitted when the user presses [d] or Delete in the
// Tasks panel, requesting the Remove confirmation dialog for the given task.
type OpenRemoveDialogMsg struct{ TaskID string }

// GenerateSlnMsg is emitted when the user presses [s] in the Tasks panel,
// requesting .sln file regeneration for the given task.
type GenerateSlnMsg struct{ TaskID string }

// CloneTaskMsg is emitted when [c] is pressed in the Tasks panel,
// requesting the clone dialog for the selected source task.
type CloneTaskMsg struct{ SrcTaskID string }

// OpenConfigModalMsg is emitted when [,] is pressed in the Tasks panel,
// requesting the read-only config modal.
type OpenConfigModalMsg struct{}

// TaskSelectionChangedMsg is emitted when the cursor in the Tasks panel
// moves to a different task. The root model uses it to reload the services panel.
type TaskSelectionChangedMsg struct{ TaskID string }

// OpenAddServiceMsg is emitted when the user presses [a] in the Services panel,
// requesting the Add Service dialog for the given task.
type OpenAddServiceMsg struct {
	TaskID           string
	ExistingServices []string
}

// ShellExecMsg is emitted when the user presses [;] in the Tasks panel,
// requesting a shell command prompt for the given task directory.
type ShellExecMsg struct{ TaskDir string }

// SyncTaskMsg is emitted when the user presses [S] in the Tasks panel,
// requesting a sync (fetch + rebase) for all services in the selected task.
type SyncTaskMsg struct{ TaskID string }

// PushTaskMsg is emitted when the user presses [P] in the Tasks panel,
// requesting a push for all services in the selected task.
type PushTaskMsg struct{ TaskID string }

// PushServiceMsg is emitted when the user presses [p] in the Services panel,
// requesting a git push for the selected service.
type PushServiceMsg struct {
	TaskID      string
	ServiceName string
}

// StashServiceMsg is emitted when the user presses [ctrl+s] or [ctrl+u] in the
// Services panel, requesting a stash or unstash operation for the selected service.
type StashServiceMsg struct {
	TaskID      string
	ServiceName string
	Pop         bool // false = stash, true = unstash (stash pop)
}

type OpenRemoveServiceDialogMsg struct {
	TaskID      string
	ServiceName string
	BranchName  string
}
