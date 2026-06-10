package gitflow

type BranchType string

const (
	BranchTypeFeature BranchType = "feature"
	BranchTypeHotfix  BranchType = "hotfix"
	BranchTypeRelease BranchType = "release"
	BranchTypeBugfix  BranchType = "bugfix"
	BranchTypeChore   BranchType = "chore"
	BranchTypeUnknown BranchType = "unknown"
)

type MergeStrategy string

const (
	MergeStrategyMerge  MergeStrategy = "merge_commit"
	MergeStrategySquash MergeStrategy = "squash"
	MergeStrategyRebase MergeStrategy = "rebase"
	MergeStrategyFFOnly MergeStrategy = "ff_only"
)

type CloseStrategy string

const (
	CloseStrategyDirectMerge   CloseStrategy = "direct_merge"
	CloseStrategyReviewRequest CloseStrategy = "review_request"
	CloseStrategyNone          CloseStrategy = "none"
)

type ResolvedGitFlow struct {
	ProductionBranch  string
	IntegrationBranch string
	DefaultBranchType BranchType
	AllowMixed        bool
	BranchTypes       map[BranchType]BranchTypeRule
}

type BranchTypeRule struct {
	Prefixes                     []string
	BaseBranch                   string
	MergeTargets                 []string
	ReviewTargets                []string
	CloseStrategy                CloseStrategy
	MergeStrategy                MergeStrategy
	RequiresClean                bool
	TagOnClose                   bool
	TagSource                    string
	DeleteSourceBranchAfterMerge bool
	TriggerPipelineOnClose       bool
}
