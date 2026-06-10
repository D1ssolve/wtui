package forge

import "context"

type ForgeClient interface {
	Provider() ForgeProvider
	IsAvailable(ctx context.Context) bool
	CreateMR(ctx context.Context, params CreateMRParams) (MRInfo, error)
	MRStatus(ctx context.Context, sourceBranch, repo string) ([]MRInfo, error)
	PipelineStatus(ctx context.Context, branch, repo string) ([]PipelineStatus, error)
	TriggerPipeline(ctx context.Context, params TriggerPipelineParams) error
	ListIssues(ctx context.Context, params ListIssuesParams) ([]IssueInfo, error)
}
