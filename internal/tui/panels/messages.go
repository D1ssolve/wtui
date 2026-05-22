package panels

type FocusServicesMsg struct{ TaskID string }

type FocusTasksMsg struct{}

type OpenInitDialogMsg struct{}

type OpenCloneDialogMsg struct{ TaskID string }

type OpenRemoveDialogMsg struct{ TaskID string }

type OpenConfigModalMsg struct{}

type TaskSelectionChangedMsg struct{ TaskID string }

type OpenAddServiceMsg struct {
	TaskID           string
	ExistingServices []string
}

type ShellExecMsg struct{ TaskDir string } // reserved for future use

type RiderTaskMsg struct {
	TaskID  string
	TaskDir string
}

type CodeWorkspaceTaskMsg struct {
	TaskID  string
	TaskDir string
}

type OpenSyncStrategyDialogMsg struct{ TaskID string }

type OpenSyncServiceStrategyDialogMsg struct {
	TaskID      string
	ServiceName string
}

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

type OpenStashDialogMsg struct {
	TaskID      string
	ServiceName string
	Pop         bool
}

type OpenRemoveServiceDialogMsg struct {
	TaskID      string
	ServiceName string
	BranchName  string
}

type OpenLazygitServiceMsg struct {
	TaskID       string
	ServiceName  string
	WorktreePath string
	Stale        bool
}
