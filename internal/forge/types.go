package forge

type ForgeProvider string

const (
	ForgeProviderGitLab  ForgeProvider = "gitlab"
	ForgeProviderGitHub  ForgeProvider = "github"
	ForgeProviderUnknown ForgeProvider = "unknown"
)

type ErrorCategory string

const (
	ErrCategoryNotInstalled ErrorCategory = "not_installed"
	ErrCategoryAuthError    ErrorCategory = "auth_error"
	ErrCategoryNetwork      ErrorCategory = "network"
	ErrCategoryParseError   ErrorCategory = "parse_error"
	ErrCategoryUnknown      ErrorCategory = "unknown"
)

type CreateMRParams struct {
	WorktreePath string
	SourceBranch string
	TargetBranch string
	Title        string
	Description  string
	Repo         string
	Draft        bool
	RemoveSource bool
	Labels       []string
	Reviewers    []string
}

type MRInfo struct {
	Number       int
	Title        string
	State        string
	URL          string
	SourceBranch string
	TargetBranch string
}

type PipelineStatus struct {
	ID           string
	Status       string
	Conclusion   string
	Branch       string
	URL          string
	WorkflowName string
}

type TriggerPipelineParams struct {
	WorktreePath string
	Branch       string
	Repo         string
	Variables    map[string]string
	WorkflowFile string
}

type ListIssuesParams struct {
	WorktreePath string
	Repo         string
	State        string
	Labels       []string
	Assignee     string
}

type IssueInfo struct {
	Number int
	Title  string
	State  string
	URL    string
	Labels []string
}
