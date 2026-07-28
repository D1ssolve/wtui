package domain

type WorkflowPhase string

const (
	TaskWorkflowCode            WorkflowPhase = "code"
	TaskWorkflowMR              WorkflowPhase = "mr"
	TaskWorkflowReviewCI        WorkflowPhase = "review_ci"
	TaskWorkflowMerge           WorkflowPhase = "merge"
	TaskWorkflowReleaseEligible WorkflowPhase = "release_eligible"

	ReleaseWorkflowDevelop       WorkflowPhase = "develop"
	ReleaseWorkflowReleaseBranch WorkflowPhase = "release_branch"
	ReleaseWorkflowRegression    WorkflowPhase = "regression"
	ReleaseWorkflowMasterMR      WorkflowPhase = "master_mr"
	ReleaseWorkflowTag           WorkflowPhase = "tag"
)

type WorkflowStep struct {
	Phase WorkflowPhase
	Label string
	State string
}

type ServiceWorkflow struct {
	ServiceName string
	Status      string
	Detail      string
}

type WorkflowSummary struct {
	Steps      []WorkflowStep
	Services   []ServiceWorkflow
	Current    WorkflowPhase
	NextAction string
	Blocker    string
}
