package domain

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
