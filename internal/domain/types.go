package domain

type Task struct {
	ID       string
	Dir      string
	Services []Service
	Stale    bool // true when Dir does not exist (race condition guard)
}

type Service struct {
	Name         string
	RepoPath     string
	WorktreePath string
	Branch       string
	BaseBranch   string
	IsDirty      bool
	Ahead        int  // commits ahead of origin/<branch>; 0 when unknown or untracked
	Behind       int  // commits behind origin/<branch>; 0 when unknown or untracked
	Stale        bool // true when WorktreePath does not exist on disk
}

type Repo struct {
	Name string
	Path string
}
