package domain

import "github.com/Masterminds/semver/v3"

type Task struct {
	ID       string
	Dir      string
	Services []Service
	Stale    bool
}

type Service struct {
	Name         string
	RepoPath     string
	WorktreePath string
	RemoteURL    string
	Branch       string
	BaseBranch   string
	IsDirty      bool
	Ahead        int
	Behind       int
	Stale        bool
}

type Repo struct {
	Name string
	Path string
}

type RepoState int

const (
	RepoStateClean RepoState = iota
	RepoStateDirty
	RepoStateUntracked
	RepoStateConflicted
	RepoStateMerging
	RepoStateRebasing
	RepoStateCherryPick
	RepoStateReverting
	RepoStateBisect
	RepoStateDetached
	RepoStateUnreachable
)

type ServiceValidation struct {
	ServiceName    string
	WorktreePath   string
	Branch         string
	States         []RepoState
	ChangedCount   int
	UntrackedCount int
	ConflictPaths  []string
	Err            error
}

type TaskValidation struct {
	TaskID   string
	Services []ServiceValidation
	AllClean bool
	Blocking bool
}

type TagInfo struct {
	Name        string
	Ref         string
	Message     string
	IsAnnotated bool
	IsSemver    bool
	Version     *semver.Version
}

type PruneCandidate struct {
	TaskID   string
	Dir      string
	Prunable bool
	Services []ServicePrune
}

type ServicePrune struct {
	ServiceName string
	Branch      string
	MergeTarget string
	IsMerged    bool
	IsStale     bool
	Err         error
}
