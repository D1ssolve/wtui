package domain

type WorkflowPhase string

const (
	TaskWorkflowCode            WorkflowPhase = "code"
	TaskWorkflowMR              WorkflowPhase = "mr"
	TaskWorkflowReviewCI        WorkflowPhase = "review_ci"
	TaskWorkflowMerge           WorkflowPhase = "merge"
	TaskWorkflowReleaseEligible WorkflowPhase = "release_eligible"
)

type WorkflowStep struct {
	Phase WorkflowPhase
	Label string
	State string
	// Kind classifies release machine states for display: review, gate or tag.
	Kind string
}

type ServiceWorkflow struct {
	ServiceName string
	Status      string
	Detail      string
	// Current/NextAction/Blocker are derived guidance; Status/Detail stay
	// authoritative for existing service-card rendering.
	Current    WorkflowPhase
	NextAction string
	Blocker    string
}

// WorkflowEvent is a key the user can press in the current workflow state.
type WorkflowEvent struct {
	Name        string
	Key         string
	Label       string
	Action      string
	Cancellable bool
}

type WorkflowSummary struct {
	// Gated marks a task driven by its type's machine: C and M work only where its events offer them.
	Gated      bool
	Events     []WorkflowEvent
	Steps      []WorkflowStep
	Services   []ServiceWorkflow
	Current    WorkflowPhase
	NextAction string
	Blocker    string
	Outcome    string
}
