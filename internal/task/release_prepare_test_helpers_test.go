package task

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func enableReleasePrepareTaskMerge(t *testing.T, m *manager) {
	t.Helper()
	setReleasePrepareTaskMerge(t, m, true)
}

func setReleasePrepareTaskMerge(t *testing.T, m *manager, enabled bool) {
	t.Helper()
	if m.flow == nil {
		t.Fatal("manager has no flow")
	}
	flow := *m.flow
	release := gitflow.ReleaseFlow{BranchPrefix: "release/", Steps: []gitflow.Step{
		{Kind: gitflow.StepCutBranch, From: flow.IntegrationBranch},
		{Kind: gitflow.StepGate, Label: "regression"},
		{Kind: gitflow.StepReview, From: "release", Into: []string{flow.ProductionBranch}},
		{Kind: gitflow.StepBackMerge, From: "release", Into: []string{flow.IntegrationBranch}, When: "diverged"},
		{Kind: gitflow.StepTag, On: "accepted_merge"},
	}}
	if flow.Release != nil {
		release = *flow.Release
	}
	release.Steps = slices.DeleteFunc(slices.Clone(release.Steps), func(s gitflow.Step) bool { return s.Kind == gitflow.StepIntegrateTasks })
	if enabled {
		release.Steps = append([]gitflow.Step{{Kind: gitflow.StepIntegrateTasks}}, release.Steps...)
	}
	flow.Release = &release
	m.flow = &flow
}

type releaseTaskMergeForge struct {
	readiness               map[int]forge.MRReadiness
	mergeCalls              int
	mergeNumbers            []int
	mergeExpectedHeads      []string
	mergeExpectedTargets    []string
	mergeExpectedTargetSHAs []string
	afterMerge              func(number int)
	mergeErr                error
}

func newReleaseTaskMergeForge() *releaseTaskMergeForge {
	return &releaseTaskMergeForge{readiness: map[int]forge.MRReadiness{}}
}

func (f *releaseTaskMergeForge) Provider() forge.ForgeProvider    { return forge.ForgeProviderGitHub }
func (f *releaseTaskMergeForge) IsAvailable(context.Context) bool { return true }
func (f *releaseTaskMergeForge) CreateMR(context.Context, forge.CreateMRParams) (forge.MRInfo, error) {
	return forge.MRInfo{}, errors.New("not used")
}
func (f *releaseTaskMergeForge) MRStatus(_ context.Context, sourceBranch, _ string) ([]forge.MRInfo, error) {
	var rows []forge.MRInfo
	for _, r := range f.readiness {
		if r.SourceBranch == sourceBranch {
			rows = append(rows, forge.MRInfo{Number: r.Number, State: r.State, URL: r.URL, SourceBranch: r.SourceBranch, TargetBranch: r.TargetBranch})
		}
	}
	return rows, nil
}
func (f *releaseTaskMergeForge) MRReadiness(context.Context, string, string, string) (forge.MRReadiness, error) {
	return forge.MRReadiness{}, errors.New("not used")
}
func (f *releaseTaskMergeForge) MRReadinessByNumber(_ context.Context, number int, _, _ string) (forge.MRReadiness, error) {
	return f.readiness[number], nil
}
func (f *releaseTaskMergeForge) MergeMR(_ context.Context, params forge.MergeMRParams) (forge.MRMergeResult, error) {
	f.mergeCalls++
	f.mergeNumbers = append(f.mergeNumbers, params.Number)
	f.mergeExpectedHeads = append(f.mergeExpectedHeads, params.ExpectedHeadSHA)
	f.mergeExpectedTargets = append(f.mergeExpectedTargets, params.ExpectedTargetBranch)
	f.mergeExpectedTargetSHAs = append(f.mergeExpectedTargetSHAs, params.ExpectedTargetSHA)
	if f.afterMerge != nil {
		f.afterMerge(params.Number)
	}
	if f.mergeErr != nil {
		return forge.MRMergeResult{}, f.mergeErr
	}
	return forge.MRMergeResult{Merged: true, MergeCommitSHA: f.readiness[params.Number].MergedSHA}, nil
}
func (f *releaseTaskMergeForge) PipelineStatus(context.Context, string, string) ([]forge.PipelineStatus, error) {
	return nil, nil
}
func (f *releaseTaskMergeForge) TriggerPipeline(context.Context, forge.TriggerPipelineParams) error {
	return nil
}
func (f *releaseTaskMergeForge) ListIssues(context.Context, forge.ListIssuesParams) ([]forge.IssueInfo, error) {
	return nil, nil
}
