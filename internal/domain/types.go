package domain

type Task struct {
	ID       string
	Dir      string
	Services []Service
}

type Service struct {
	Name         string
	RepoPath     string
	WorktreePath string
	Branch       string
	BaseBranch   string
	IsDirty      bool
}

type Repo struct {
	Name string
	Path string
}
