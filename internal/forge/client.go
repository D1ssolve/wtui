package forge

import "context"

type ForgeClient interface {
	Provider() ForgeProvider
	IsAvailable(ctx context.Context) bool
	CreateMR(ctx context.Context, params CreateMRParams) (MRInfo, error)
	MRStatus(ctx context.Context, sourceBranch, repo string) ([]MRInfo, error)
	MRReadiness(ctx context.Context, sourceBranch, repo string, worktreePath string) (MRReadiness, error)
	MRReadinessByNumber(ctx context.Context, number int, repo, worktreePath string) (MRReadiness, error)
	MergeMR(ctx context.Context, params MergeMRParams) (MRMergeResult, error)
	PipelineStatus(ctx context.Context, branch, repo string) ([]PipelineStatus, error)
	TriggerPipeline(ctx context.Context, params TriggerPipelineParams) error
	ListIssues(ctx context.Context, params ListIssuesParams) ([]IssueInfo, error)
}

// MergeQueuer is implemented by forges that can add a reviewed MR to a merge queue.
type MergeQueuer interface {
	// EnqueueMR queues the MR for merging once its head still equals ExpectedHeadSHA;
	// the merge happens later on the forge.
	EnqueueMR(ctx context.Context, params MergeMRParams) error
}

// ReleasePublisher is implemented by forges that can publish a Release object for a tag
// that already exists on the forge. It never creates or moves a tag: an existing release
// is returned as is, and creation happens only after the lookup confirmed absence.
type ReleasePublisher interface {
	PublishRelease(ctx context.Context, params PublishParams) (url string, err error)
}
