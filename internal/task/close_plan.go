package task

import "github.com/D1ssolve/wtui/internal/gitflow"

type ClosePlan struct {
	TaskID        string
	BranchType    gitflow.BranchType
	Services      []ServiceClosePlan
	RequiresTag   bool
	RequiresForge bool
	Warnings      []string
}

type ServiceClosePlan struct {
	ServiceName    string
	SourceBranch   string
	TargetBranches []string
	ReviewTargets  []string
	CloseStrategy  gitflow.CloseStrategy
	MergeStrategy  gitflow.MergeStrategy
	TagPlan        *TagPlan
	ForgePlan      *ReviewRequestPlan
	PipelinePlan   *PipelinePlan
}

type TagPlan struct {
	TagName  string
	Version  string
	SourceRef string
	Annotated bool
	Message  string
	Push     bool
}

type ReviewRequestPlan struct {
	TargetBranch string
	Title        string
	Description  string
	Draft        bool
	RemoveSource bool
}

type PipelinePlan struct {
	Branch       string
	WorkflowFile string
	Variables    map[string]string
}

type CloseTaskParams struct {
	TaskID      string
	ServiceName string
	StatusCh    chan<- string
	DryRun      bool
	TagVersion  string
}

type CloseTaskResult struct {
	TaskID     string
	BranchType gitflow.BranchType
	Steps      []CloseTaskStep
	TagCreated string
	MRURLs     []string
	Success    bool
}

type CloseTaskStep struct {
	Name    string
	Status  StepStatus
	Message string
}

type StepStatus string

const (
	StepStatusOK      StepStatus = "ok"
	StepStatusSkipped StepStatus = "skipped"
	StepStatusFailed  StepStatus = "failed"
)
