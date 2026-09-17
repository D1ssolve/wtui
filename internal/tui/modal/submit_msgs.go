package modal

import (
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/task"
)

type SubmitPruneMsg struct {
	SelectedTaskIDs []string
}

type ForgeCreateMRMsg struct {
	TaskID string
	Title  string
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
	Title           string
	TaskIDs         []string
	Versions        map[string]string
	TagDescriptions map[string]string
}

type RequestReleaseVersionsMsg struct {
	TaskIDs []string
}

type ConfirmReleaseExecuteMsg struct {
	Title           string
	TaskIDs         []string
	Versions        map[string]string
	TagDescriptions map[string]string
}

type ConfirmMergeMsg struct {
	Number       int
	TargetBranch string
	HeadSHA      string
	TaskID       string
	ReleaseID    string
	ServiceName  string
}

type SubmitReleaseCleanupMsg struct {
	ReleaseID string
	Selection task.ReleaseCleanupSelection
}

type ConfirmReleaseCleanupMsg struct {
	ReleaseID  string
	Generation uint64
}

type ConfirmRemoteReleaseCleanupMsg struct {
	ReleaseID  string
	Generation uint64
}
