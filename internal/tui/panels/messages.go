package panels

type FocusServicesMsg struct{ TaskID string }

type FocusTasksMsg struct{}

type OpenInitDialogMsg struct{}

type OpenRemoveDialogMsg struct{ TaskID string }

type OpenConfigModalMsg struct{}

type TaskSelectionChangedMsg struct{ TaskID string }

type OpenAddServiceMsg struct {
	TaskID           string
	ExistingServices []string
}

type ShellExecMsg struct{ TaskDir string }

type RiderTaskMsg struct {
	TaskID  string
	TaskDir string
}

type OpenSyncStrategyDialogMsg struct{ TaskID string }

type PushTaskMsg struct{ TaskID string }

type PushServiceMsg struct {
	TaskID      string
	ServiceName string
}

type StashServiceMsg struct {
	TaskID      string
	ServiceName string
	Pop         bool
}

type OpenRemoveServiceDialogMsg struct {
	TaskID      string
	ServiceName string
	BranchName  string
}
