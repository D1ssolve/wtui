package gitflow

import "strings"

type BranchType string

const (
	BranchTypeFeature BranchType = "feature"
	BranchTypeHotfix  BranchType = "hotfix"
	BranchTypeRelease BranchType = "release"
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
	Preset            string `json:"-"`
	ProductionBranch  string
	IntegrationBranch string
	DefaultBranchType BranchType
	AllowMixed        bool
	BranchTypes       map[BranchType]BranchTypeRule
	Release           *ReleaseFlow
}

// Step is one compiled flow block; branch aliases are already resolved.
type Step struct {
	Kind  string        `json:"kind"`
	Into  []string      `json:"into,omitempty"`
	From  string        `json:"from,omitempty"`
	Merge MergeStrategy `json:"merge,omitempty"`
	On    string        `json:"on,omitempty"`
	Label string        `json:"label,omitempty"`
	When  string        `json:"when,omitempty"`
	// ViaActiveRelease: merge into the active release branch instead of integration when one exists.
	ViaActiveRelease bool   `json:"via_active_release,omitempty"`
	Name             string `json:"name,omitempty"`
	Command          string `json:"command,omitempty"`
	Timeout          string `json:"timeout,omitempty"`
}

type ReleaseFlow struct {
	BranchPrefix string
	Steps        []Step
	Machine      *Machine
	// FromMachine marks a release described by `release.machine` rather than `release.steps`.
	FromMachine bool
	DependsOn   map[string][]string
	// MR is applied to the production review MRs a release opens.
	MR MRPolicy
}

// MRPolicy holds the optional defaults for every MR wtui opens for a task type or release.
type MRPolicy struct {
	Draft              bool     `json:"draft,omitempty"`
	Labels             []string `json:"labels,omitempty"`
	Reviewers          []string `json:"reviewers,omitempty"`
	Assignees          []string `json:"assignees,omitempty"`
	RemoveSourceBranch bool     `json:"remove_source_branch,omitempty"`
}

// Describe is the one-line policy text used by `wtui flow` and close previews; empty for no policy.
func (p MRPolicy) Describe() string {
	var parts []string
	if p.Draft {
		parts = append(parts, "draft")
	}
	for _, list := range []struct {
		name   string
		values []string
	}{{"labels", p.Labels}, {"reviewers", p.Reviewers}, {"assignees", p.Assignees}} {
		if len(list.values) > 0 {
			parts = append(parts, list.name+" "+strings.Join(list.values, ", "))
		}
	}
	if p.RemoveSourceBranch {
		parts = append(parts, "remove source branch")
	}
	return strings.Join(parts, "; ")
}

const (
	StepIntegrateTasks = "integrate_tasks"
	StepCutBranch      = "cut_branch"
	StepGate           = "gate"
	StepReview         = "review"
	StepMerge          = "merge"
	StepBackMerge      = "back_merge"
	StepTag            = "tag"
	StepPipeline       = "pipeline"
	StepCherryPick     = "cherry_pick"

	// OpenReleases targets the release branches of wtui releases that are cut but not yet finished.
	OpenReleases = "open_releases"
)

func (f *ResolvedGitFlow) TaskMergeAtReleasePrepare() bool {
	return f.ReleaseHasStep(StepIntegrateTasks)
}

func (f *ResolvedGitFlow) ReleaseHasStep(kind string) bool {
	if f == nil || f.Release == nil {
		return false
	}
	for _, step := range f.Release.Steps {
		if step.Kind == kind {
			return true
		}
	}
	return false
}

// TagsReleaseBranch reports a configured release flow without a production review:
// the release is finished by tagging the release branch itself.
func (f *ResolvedGitFlow) TagsReleaseBranch() bool {
	return f != nil && f.Release != nil && !f.ReleaseHasStep(StepReview)
}

type BranchTypeRule struct {
	Prefixes               []string
	BaseBranch             string
	MergeTargets           []string
	ReviewTargets          []string
	CloseStrategy          CloseStrategy
	MergeStrategy          MergeStrategy
	RequiresClean          bool
	TagOnClose             bool
	TagSource              string
	TriggerPipelineOnClose bool
	// Shared branches are long-lived team branches: never task-owned, always push-protected.
	Shared bool
	// ViaActiveRelease redirects integration targets to the active release branch when one exists.
	ViaActiveRelease bool
	// CherryPickToOpenReleases picks the accepted review merge into every open release branch.
	CherryPickToOpenReleases bool
	Close                    []Step
	MR                       MRPolicy
	// Machine is the optional task lifecycle compiled from task_types.<name>.machine.
	Machine *Machine `json:"-"`
}
