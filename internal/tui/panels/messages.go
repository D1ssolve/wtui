package panels

import (
	"github.com/D1ssolve/wtui/internal/forge"
)

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

type PlanCloseTaskMsg struct{ TaskID string }

type ScanPrunableTasksMsg struct{}

type ValidateTaskMsg struct{ TaskID string }

type OpenTagBrowserMsg struct{ TaskID string }

type OpenForgeMenuMsg struct {
	TaskID      string
	ServiceName string
	Provider    forge.ForgeProvider
}

type ForgePipelineStatusMsg struct {
	TaskID      string
	ServiceName string
	Branch      string
	RepoPath    string
}

type OpenCreateReleaseDialogMsg struct{}

type ReleaseVersionsLoadedMsg struct {
	Versions map[string]string // serviceName → proposed version
}

type FinishReleaseMsg struct {
	ReleaseID string
}
