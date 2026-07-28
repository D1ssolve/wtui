package modal

import "github.com/D1ssolve/wtui/internal/forge"

type SubmitPruneMsg struct {
	SelectedTaskIDs []string
}

type ForgeCreateMRMsg struct {
	TaskID      string
	ServiceName string
}

type ForgePipelineStatusMsg struct {
	TaskID      string
	ServiceName string
	Provider    forge.ForgeProvider
}

type ForgeMergeMRMsg struct {
	TaskID      string
	ServiceName string
}

type ForgeListIssuesMsg struct {
	TaskID      string
	ServiceName string
}

type SubmitCreateReleaseMsg struct {
	TaskIDs  []string
	Versions map[string]string
}

type RequestReleaseVersionsMsg struct {
	TaskIDs []string
}

type ConfirmReleaseExecuteMsg struct {
	TaskIDs  []string
	Versions map[string]string
}

type ConfirmMergeMsg struct {
	TaskID      string
	ReleaseID   string
	ServiceName string
}
