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

type MRReadiness struct {
	Number         int
	State          string
	URL            string
	SourceBranch   string
	TargetBranch   string
	HeadSHA        string
	MergedSHA      string
	Approved       bool
	CIState        string
	Mergeable      bool
	Ready          bool
	Blockers       []string
	SupportsSHAPin bool
	// SupportsTargetBinding reports atomic target-branch/repo binding during
	// merge. gh/glab report false: callers can request a non-atomic branch
	// preflight while the requested source head remains server pinned.
	SupportsTargetBinding bool
	StatusChecksBlocking  bool
}

type MergeMRParams struct {
	WorktreePath string
	Repo         string
	Number       int
	// ExpectedHeadSHA requires a server-side source head pin.
	ExpectedHeadSHA string
	// ExpectedTargetBranch requests a fresh readiness check immediately before
	// merge, rejecting target or expected head drift and non-open/not-ready MRs.
	// This preflight is not atomic target binding: retargeting can still race it.
	ExpectedTargetBranch string
	// ExpectedTargetSHA requests an atomic target-tip pin only from adapters
	// advertising SupportsTargetBinding; gh/glab reject it.
	ExpectedTargetSHA string
	Method            string
}

type MRMergeResult struct {
	Merged         bool
	MergeCommitSHA string
}

type PipelineStatus struct {
	ID           string
	Status       string
	Conclusion   string
	Branch       string
	URL          string
	WorkflowName string
	Jobs         []PipelineJob
}

type PipelineJob struct {
	ID     string
	Name   string
	Stage  string
	Status string
	URL    string
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
