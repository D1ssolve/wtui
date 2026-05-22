package modal

import "github.com/D1ssolve/wtui/internal/task"

type SubmitInitMsg struct {
	TaskID       string
	Services     []string
	BranchPrefix string
	BaseBranch   string
}

type SubmitAddMsg struct {
	TaskID   string
	Services []string
}

type SubmitRemoveTaskMsg struct {
	TaskID         string
	Force          bool
	DeleteBranches bool
}

type CloseModalMsg struct{}

type SubmitRemoveServiceMsg struct {
	TaskID       string
	ServiceName  string
	RemoveBranch bool
}

type SubmitSyncStrategyMsg struct {
	TaskID   string
	Strategy task.SyncStrategy
}

type SubmitSyncServiceStrategyMsg struct {
	TaskID      string
	ServiceName string
	Strategy    task.SyncStrategy
}

type SubmitRemoteBranchStrategyMsg struct {
	TaskID       string
	ServiceName  string
	Strategy     task.RemoteBranchStrategy
	BranchSuffix string
}

type RemoteBranchConflictMsg struct {
	TaskID      string
	ServiceName string
	BranchName  string
	RepoPath    string
}

type SubmitStashMsg struct {
	TaskID           string
	ServiceName      string
	Pop              bool
	IncludeUntracked bool
}
