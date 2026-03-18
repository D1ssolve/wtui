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

// OpenWorkspaceMsg is emitted when the user presses [o] in the Tasks panel,
// requesting the workspace file for the given task to be opened in the editor.
type OpenWorkspaceMsg struct{ TaskID string }

// GenerateSlnMsg is emitted when the user presses [s] in the Tasks panel,
// requesting .sln file regeneration for the given task.
type GenerateSlnMsg struct{ TaskID string }

// OpenFilePickerMsg is emitted when [o] is pressed in the Tasks panel.
// Triggers loading open candidates and opening the OpenDialog modal.
type OpenFilePickerMsg struct{ TaskID string }

// TaskSelectionChangedMsg is emitted when the cursor in the Tasks panel
// moves to a different task. The root model uses it to reload the services panel.
type TaskSelectionChangedMsg struct{ TaskID string }

// OpenAddServiceMsg is emitted when the user presses [a] in the Services panel,
// requesting the Add Service dialog for the given task.
type OpenAddServiceMsg struct{ TaskID string }

// ShellExecMsg is emitted when the user presses [;] in the Tasks panel,
// requesting a shell command prompt for the given task directory.
type ShellExecMsg struct{ TaskDir string }
