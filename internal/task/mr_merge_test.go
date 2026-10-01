package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/git"
)

type mergeForgeClient struct {
	readiness         map[string]forge.MRReadiness
	readinessByNumber map[int]forge.MRReadiness
	postMerge         map[int]forge.MRReadiness
	history           map[string][]forge.MRInfo
	readErrs          map[string]error
	mergeErrs         map[int]error
	merges            []forge.MergeMRParams
}

func (f *mergeForgeClient) Provider() forge.ForgeProvider    { return forge.ForgeProviderGitLab }
func (f *mergeForgeClient) IsAvailable(context.Context) bool { return true }
func (f *mergeForgeClient) CreateMR(context.Context, forge.CreateMRParams) (forge.MRInfo, error) {
	return forge.MRInfo{}, nil
}
func (f *mergeForgeClient) MRStatus(context.Context, string, string) ([]forge.MRInfo, error) {
	return nil, nil
}
func (f *mergeForgeClient) MRHistory(_ context.Context, branch, _ string) ([]forge.MRInfo, error) {
	return f.history[branch], nil
}
func (f *mergeForgeClient) MRReadiness(_ context.Context, branch, _, _ string) (forge.MRReadiness, error) {
	return f.readiness[branch], f.readErrs[branch]
}
func (f *mergeForgeClient) MRReadinessByNumber(_ context.Context, number int, _, _ string) (forge.MRReadiness, error) {
	if r, ok := f.postMerge[number]; ok {
		return r, nil
	}
	return f.readinessByNumber[number], nil
}
func (f *mergeForgeClient) MergeMR(_ context.Context, params forge.MergeMRParams) (forge.MRMergeResult, error) {
	f.merges = append(f.merges, params)
	if err := f.mergeErrs[params.Number]; err != nil {
		return forge.MRMergeResult{}, err
	}
	return forge.MRMergeResult{Merged: true, MergeCommitSHA: "merge-sha"}, nil
}
func (f *mergeForgeClient) PipelineStatus(context.Context, string, string) ([]forge.PipelineStatus, error) {
	return nil, nil
}
func (f *mergeForgeClient) TriggerPipeline(context.Context, forge.TriggerPipelineParams) error {
	return nil
}
func (f *mergeForgeClient) ListIssues(context.Context, forge.ListIssuesParams) ([]forge.IssueInfo, error) {
	return nil, nil
}

func TestMergeTaskMRs_MergesOnlyReadyServices(t *testing.T) {
	client := &mergeForgeClient{readiness: map[string]forge.MRReadiness{
		"feature/ready":   {Number: 1, State: "open", TargetBranch: "develop", HeadSHA: "ready-sha", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true},
		"feature/blocked": {Number: 2, State: "open", Blockers: []string{"checks failing"}, SupportsSHAPin: true, SupportsTargetBinding: true},
	},
		postMerge: map[int]forge.MRReadiness{
			1: {Number: 1, State: "merged", SourceBranch: "feature/ready", TargetBranch: "develop", HeadSHA: "ready-sha", MergedSHA: "merge-sha"},
		},
	}
	mgr, _ := newMRMergeTestManager(t, map[string]string{"ready": "feature/ready", "blocked": "feature/blocked"}, "git@gitlab.com:group/repo.git", client)

	result, err := mgr.MergeTaskMRs(t.Context(), "TASK-1")
	if err != nil {
		t.Fatalf("MergeTaskMRs() err = %v", err)
	}
	if !slices.Equal(result.Merged, []string{"ready"}) {
		t.Fatalf("Merged = %v, want [ready]", result.Merged)
	}
	if !slices.Equal(result.Skipped, []string{"blocked"}) {
		t.Fatalf("Skipped = %v, want [blocked]", result.Skipped)
	}
	if len(client.merges) != 1 || client.merges[0].Number != 1 || client.merges[0].ExpectedHeadSHA != "ready-sha" {
		t.Fatalf("merges = %#v, want one SHA-pinned merge for MR 1", client.merges)
	}
	if client.merges[0].ExpectedTargetBranch != "develop" {
		t.Fatalf("ExpectedTargetBranch = %q, want develop", client.merges[0].ExpectedTargetBranch)
	}
	if client.merges[0].ExpectedTargetSHA != "origin/develop-sha" {
		t.Fatalf("ExpectedTargetSHA = %q, want freshly resolved origin/develop tip", client.merges[0].ExpectedTargetSHA)
	}
}

func TestMergeServiceMR_MergesOnlySelectedService(t *testing.T) {
	client := &mergeForgeClient{readiness: map[string]forge.MRReadiness{
		"feature/api":    {Number: 1, State: "open", TargetBranch: "develop", HeadSHA: "api-sha", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true},
		"feature/worker": {Number: 2, State: "open", TargetBranch: "develop", HeadSHA: "worker-sha", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true},
	},
		postMerge: map[int]forge.MRReadiness{
			2: {Number: 2, State: "merged", SourceBranch: "feature/worker", TargetBranch: "develop", HeadSHA: "worker-sha", MergedSHA: "merge-sha"},
		},
	}
	mgr, _ := newMRMergeTestManager(t, map[string]string{"api": "feature/api", "worker": "feature/worker"}, "git@gitlab.com:group/repo.git", client)

	result, err := mgr.MergeServiceMR(t.Context(), "TASK-1", "worker")
	if err != nil {
		t.Fatalf("MergeServiceMR() err = %v", err)
	}
	if !slices.Equal(result.Merged, []string{"worker"}) {
		t.Fatalf("Merged = %v, want [worker]", result.Merged)
	}
	if len(client.merges) != 1 || client.merges[0].Number != 2 {
		t.Fatalf("merges = %#v, want only worker MR 2", client.merges)
	}
}

func TestMergeTaskMRs_RecordsHeadDriftAndContinues(t *testing.T) {
	headDrift := errors.New("head SHA changed")
	client := &mergeForgeClient{
		readiness: map[string]forge.MRReadiness{
			"feature/a": {Number: 1, State: "open", TargetBranch: "develop", HeadSHA: "old-sha", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true},
			"feature/b": {Number: 2, State: "open", TargetBranch: "develop", HeadSHA: "b-sha", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true},
		},
		mergeErrs: map[int]error{1: headDrift},
		postMerge: map[int]forge.MRReadiness{
			2: {Number: 2, State: "merged", SourceBranch: "feature/b", TargetBranch: "develop", HeadSHA: "b-sha", MergedSHA: "merge-sha"},
		},
	}
	mgr, _ := newMRMergeTestManager(t, map[string]string{"a": "feature/a", "b": "feature/b"}, "git@gitlab.com:group/repo.git", client)

	result, err := mgr.MergeTaskMRs(t.Context(), "TASK-1")
	if err != nil {
		t.Fatalf("MergeTaskMRs() err = %v", err)
	}
	if !errors.Is(result.Errs["a"], headDrift) {
		t.Fatalf("Errs[a] = %v, want head drift", result.Errs["a"])
	}
	if !slices.Equal(result.Merged, []string{"b"}) || !slices.Contains(result.Skipped, "a") {
		t.Fatalf("result = %#v, want a failed and b merged", result)
	}
	if len(client.merges) != 2 {
		t.Fatalf("merge calls = %d, want 2", len(client.merges))
	}
}

func TestMergeTaskMRs_RejectsForgeWithoutSHAPinSupport(t *testing.T) {
	client := &mergeForgeClient{
		readiness: map[string]forge.MRReadiness{
			"feature/a": {Number: 1, State: "open", TargetBranch: "develop", HeadSHA: "a-sha", Ready: true, SupportsSHAPin: false, SupportsTargetBinding: true},
		},
	}
	mgr, _ := newMRMergeTestManager(t, map[string]string{"a": "feature/a"}, "git@gitlab.com:group/repo.git", client)

	result, err := mgr.MergeTaskMRs(t.Context(), "TASK-1")
	if err != nil {
		t.Fatalf("MergeTaskMRs() err = %v", err)
	}
	if len(client.merges) != 0 {
		t.Fatalf("merges = %#v, want no unpinned merge", client.merges)
	}
	if result.Errs["a"] == nil || !strings.Contains(result.Errs["a"].Error(), "SHA-pinned") {
		t.Fatalf("Errs[a] = %v, want SHA-pin rejection", result.Errs["a"])
	}
	if !slices.Contains(result.Skipped, "a") || slices.Contains(result.Merged, "a") {
		t.Fatalf("result = %#v, want a failed per-service", result)
	}
}

func TestMergeTaskMRs_RejectsForgeWithoutTargetBindingSupport(t *testing.T) {
	client := &mergeForgeClient{
		readiness: map[string]forge.MRReadiness{
			"feature/a": {Number: 1, State: "open", TargetBranch: "develop", HeadSHA: "a-sha", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: false},
		},
	}
	mgr, _ := newMRMergeTestManager(t, map[string]string{"a": "feature/a"}, "git@gitlab.com:group/repo.git", client)

	result, err := mgr.MergeTaskMRs(t.Context(), "TASK-1")
	if err != nil {
		t.Fatalf("MergeTaskMRs() err = %v", err)
	}
	if len(client.merges) != 0 {
		t.Fatalf("merges = %#v, want no target-unbound merge", client.merges)
	}
	if result.Errs["a"] == nil || !strings.Contains(result.Errs["a"].Error(), "target-bound") {
		t.Fatalf("Errs[a] = %v, want target-binding rejection", result.Errs["a"])
	}
	if !slices.Contains(result.Skipped, "a") || slices.Contains(result.Merged, "a") {
		t.Fatalf("result = %#v, want a failed per-service", result)
	}
}

func TestMergeTaskMRs_RejectsMissingMRIdentity(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*forge.MRReadiness)
		wantErr string
	}{
		{name: "empty head SHA", mutate: func(r *forge.MRReadiness) { r.HeadSHA = "" }, wantErr: "head SHA"},
		{name: "empty target branch", mutate: func(r *forge.MRReadiness) { r.TargetBranch = "" }, wantErr: "target branch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mr := forge.MRReadiness{Number: 1, State: "open", SourceBranch: "feature/a", TargetBranch: "develop", HeadSHA: "a-sha", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true}
			tc.mutate(&mr)
			client := &mergeForgeClient{
				readiness: map[string]forge.MRReadiness{"feature/a": mr},
			}
			mgr, _ := newMRMergeTestManager(t, map[string]string{"a": "feature/a"}, "git@gitlab.com:group/repo.git", client)

			result, err := mgr.MergeTaskMRs(t.Context(), "TASK-1")
			if err != nil {
				t.Fatalf("MergeTaskMRs() err = %v", err)
			}
			if len(client.merges) != 0 {
				t.Fatalf("merges = %#v, want no merge for incomplete identity", client.merges)
			}
			if result.Errs["a"] == nil || !strings.Contains(result.Errs["a"].Error(), tc.wantErr) {
				t.Fatalf("Errs[a] = %v, want %q rejection", result.Errs["a"], tc.wantErr)
			}
		})
	}
}

func TestMergeTaskMRs_UnpinnedHeadDriftSkipsMerge(t *testing.T) {
	client := &mergeForgeClient{
		readiness: map[string]forge.MRReadiness{
			"feature/a": {Number: 17, State: "open", TargetBranch: "develop", HeadSHA: "inspected-sha", Ready: true, SupportsTargetBinding: true},
		},
		readinessByNumber: map[int]forge.MRReadiness{17: {Number: 17, HeadSHA: "changed-sha"}},
	}
	mgr, _ := newMRMergeTestManager(t, map[string]string{"a": "feature/a"}, "git@gitlab.com:group/repo.git", client)

	result, err := mgr.MergeTaskMRs(t.Context(), "TASK-1")
	if err != nil {
		t.Fatalf("MergeTaskMRs() err = %v", err)
	}
	if len(client.merges) != 0 || result.Errs["a"] == nil || !strings.Contains(result.Errs["a"].Error(), "SHA-pinned") {
		t.Fatalf("merges = %#v, result = %#v", client.merges, result)
	}
}

func TestInspectTaskMerge_NoMRIsSkippedByMerge(t *testing.T) {
	client := &mergeForgeClient{readiness: map[string]forge.MRReadiness{
		"feature/a": {SourceBranch: "feature/a", Blockers: []string{"merge request not found"}},
	}}
	mgr, _ := newMRMergeTestManager(t, map[string]string{"a": "feature/a"}, "git@gitlab.com:group/repo.git", client)

	inspection, err := mgr.InspectTaskMerge(t.Context(), "TASK-1")
	if err != nil {
		t.Fatalf("InspectTaskMerge() err = %v", err)
	}
	if len(inspection.Services) != 1 || inspection.Services[0].Status != "no_mr" {
		t.Fatalf("inspection = %#v, want no_mr", inspection)
	}
	result, err := mgr.MergeTaskMRs(t.Context(), "TASK-1")
	if err != nil {
		t.Fatalf("MergeTaskMRs() err = %v", err)
	}
	if !slices.Equal(result.Skipped, []string{"a"}) || len(client.merges) != 0 {
		t.Fatalf("result = %#v, merges = %#v; want skipped only", result, client.merges)
	}
}

func TestInspectTaskMerge_UnparseableRepoMarksServiceFailed(t *testing.T) {
	client := &mergeForgeClient{}
	mgr, _ := newMRMergeTestManager(t, map[string]string{"a": "feature/a"}, "git@gitlab.com:", client)

	inspection, err := mgr.InspectTaskMerge(t.Context(), "TASK-1")
	if err != nil {
		t.Fatalf("InspectTaskMerge() err = %v", err)
	}
	if len(inspection.Services) != 1 || inspection.Services[0].Status != "failed" {
		t.Fatalf("inspection = %#v, want failed", inspection)
	}
	if blockers := inspection.Services[0].Blockers; len(blockers) != 1 || !strings.Contains(blockers[0], "not parseable") {
		t.Fatalf("blockers = %v, want parse error", blockers)
	}
}

func TestMergeTaskMRs_SkipsReadyMRWithWrongTarget(t *testing.T) {
	client := &mergeForgeClient{readiness: map[string]forge.MRReadiness{
		"feature/a": {Number: 1, State: "open", SourceBranch: "feature/a", TargetBranch: "master", HeadSHA: "a-sha", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true},
		"feature/b": {Number: 2, State: "open", SourceBranch: "feature/b", TargetBranch: "develop", HeadSHA: "b-sha", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true},
	}}
	mgr, _ := newMRMergeTestManager(t, map[string]string{"a": "feature/a", "b": "feature/b"}, "git@gitlab.com:group/repo.git", client)

	inspection, err := mgr.InspectTaskMerge(t.Context(), "TASK-1")
	if err != nil {
		t.Fatalf("InspectTaskMerge() err = %v", err)
	}
	if inspection.Services[0].Status != "blocked" {
		t.Fatalf("status = %q, want blocked: %#v", inspection.Services[0].Status, inspection.Services[0])
	}
	if blockers := inspection.Services[0].Blockers; len(blockers) == 0 || !strings.Contains(blockers[len(blockers)-1], "targets master, want develop") {
		t.Fatalf("blockers = %v, want wrong-target blocker", blockers)
	}

	client.postMerge = map[int]forge.MRReadiness{
		2: {Number: 2, State: "merged", SourceBranch: "feature/b", TargetBranch: "develop", HeadSHA: "b-sha", MergedSHA: "merge-sha"},
	}
	result, err := mgr.MergeTaskMRs(t.Context(), "TASK-1")
	if err != nil {
		t.Fatalf("MergeTaskMRs() err = %v", err)
	}
	if !slices.Equal(result.Merged, []string{"b"}) || !slices.Contains(result.Skipped, "a") {
		t.Fatalf("result = %#v, want a skipped and b merged", result)
	}
	if len(client.merges) != 1 || client.merges[0].Number != 2 {
		t.Fatalf("merges = %#v, want only MR 2 merged", client.merges)
	}
}

func TestMergeServiceMR_SkipsReadyMRWithWrongSource(t *testing.T) {
	client := &mergeForgeClient{readiness: map[string]forge.MRReadiness{
		"feature/a": {Number: 1, State: "open", SourceBranch: "feature/other", TargetBranch: "develop", HeadSHA: "a-sha", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true},
	}}
	mgr, _ := newMRMergeTestManager(t, map[string]string{"a": "feature/a"}, "git@gitlab.com:group/repo.git", client)

	inspection, err := mgr.InspectTaskMerge(t.Context(), "TASK-1")
	if err != nil {
		t.Fatalf("InspectTaskMerge() err = %v", err)
	}
	if inspection.Services[0].Status != "blocked" {
		t.Fatalf("status = %q, want blocked: %#v", inspection.Services[0].Status, inspection.Services[0])
	}
	if blockers := inspection.Services[0].Blockers; len(blockers) == 0 || !strings.Contains(blockers[len(blockers)-1], "source is feature/other, want feature/a") {
		t.Fatalf("blockers = %v, want wrong-source blocker", blockers)
	}

	selected := MRSelection{Number: 1, TargetBranch: "develop", HeadSHA: "a-sha"}
	result, err := mgr.MergeServiceMR(t.Context(), "TASK-1", "a", selected)
	if err != nil {
		t.Fatalf("MergeServiceMR() err = %v", err)
	}
	if !slices.Equal(result.Skipped, []string{"a"}) || len(client.merges) != 0 {
		t.Fatalf("result = %#v, merges = %#v; want skipped without merge", result, client.merges)
	}
}

func TestInspectTaskMerge_PopulatedMatchingSourceAndTargetStayReady(t *testing.T) {
	client := &mergeForgeClient{readiness: map[string]forge.MRReadiness{
		"feature/a": {Number: 1, State: "open", SourceBranch: "feature/a", TargetBranch: "develop", HeadSHA: "a-sha", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true},
	}}
	mgr, _ := newMRMergeTestManager(t, map[string]string{"a": "feature/a"}, "git@gitlab.com:group/repo.git", client)

	inspection, err := mgr.InspectTaskMerge(t.Context(), "TASK-1")
	if err != nil {
		t.Fatalf("InspectTaskMerge() err = %v", err)
	}
	if inspection.Services[0].Status != "ready" {
		t.Fatalf("status = %q, want ready: %#v", inspection.Services[0].Status, inspection.Services[0])
	}
}

func TestMergeTaskMRs_UnpinnedSourceTargetDriftSkipsMerge(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*forge.MRReadiness)
	}{
		{name: "target drift", mutate: func(r *forge.MRReadiness) { r.TargetBranch = "master" }},
		{name: "source drift", mutate: func(r *forge.MRReadiness) { r.SourceBranch = "feature/other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mr := forge.MRReadiness{Number: 1, State: "open", SourceBranch: "feature/a", TargetBranch: "develop", HeadSHA: "a-sha", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true}
			tc.mutate(&mr)
			client := &mergeForgeClient{
				readiness: map[string]forge.MRReadiness{"feature/a": mr},
			}
			mgr, _ := newMRMergeTestManager(t, map[string]string{"a": "feature/a"}, "git@gitlab.com:group/repo.git", client)

			result, err := mgr.MergeTaskMRs(t.Context(), "TASK-1")
			if err != nil {
				t.Fatalf("MergeTaskMRs() err = %v", err)
			}
			if len(client.merges) != 0 {
				t.Fatalf("merges = %#v, want none", client.merges)
			}
			if !slices.Contains(result.Skipped, "a") || slices.Contains(result.Merged, "a") {
				t.Fatalf("result = %#v, want a skipped per-service", result)
			}
		})
	}
}

func TestMergeTaskMRs_FetchesFreshTargetTipBeforeMerge(t *testing.T) {
	client := &mergeForgeClient{
		readiness: map[string]forge.MRReadiness{
			"feature/a": {Number: 1, State: "open", SourceBranch: "feature/a", TargetBranch: "develop", HeadSHA: "a-sha", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true},
		},
		postMerge: map[int]forge.MRReadiness{
			1: {Number: 1, State: "merged", SourceBranch: "feature/a", TargetBranch: "develop", HeadSHA: "a-sha", MergedSHA: "merge-sha"},
		},
	}
	mgr, gitMock := newMRMergeTestManager(t, map[string]string{"a": "feature/a"}, "git@gitlab.com:group/repo.git", client)
	fetched := false
	gitMock.fetchFn = func(string) error {
		fetched = true
		return nil
	}
	gitMock.resolveRefFn = func(_ string, ref string) (string, error) {
		if ref == "origin/develop" && !fetched {
			return "stale-tip", nil
		}
		return "fresh-tip", nil
	}

	result, err := mgr.MergeTaskMRs(t.Context(), "TASK-1")
	if err != nil {
		t.Fatalf("MergeTaskMRs() err = %v", err)
	}
	if !slices.Equal(result.Merged, []string{"a"}) {
		t.Fatalf("result = %#v, want a merged", result)
	}
	if len(client.merges) != 1 {
		t.Fatalf("merges = %#v, want one merge", client.merges)
	}
	params := client.merges[0]
	if params.ExpectedTargetSHA != "fresh-tip" {
		t.Fatalf("ExpectedTargetSHA = %q, want freshly resolved tip", params.ExpectedTargetSHA)
	}
	if params.ExpectedTargetBranch != "develop" || params.ExpectedHeadSHA != "a-sha" {
		t.Fatalf("merge params = %#v, want exact target branch and head bindings", params)
	}
}

func TestMergeTaskMRs_FreshTargetResolutionFailureSkipsMerge(t *testing.T) {
	client := &mergeForgeClient{
		readiness: map[string]forge.MRReadiness{
			"feature/a": {Number: 1, State: "open", SourceBranch: "feature/a", TargetBranch: "develop", HeadSHA: "a-sha", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true},
		},
	}
	mgr, gitMock := newMRMergeTestManager(t, map[string]string{"a": "feature/a"}, "git@gitlab.com:group/repo.git", client)
	gitMock.resolveRefErr = errors.New("no such ref")

	result, err := mgr.MergeTaskMRs(t.Context(), "TASK-1")
	if err != nil {
		t.Fatalf("MergeTaskMRs() err = %v", err)
	}
	if len(client.merges) != 0 {
		t.Fatalf("merges = %#v, want no merge without a resolvable target tip", client.merges)
	}
	if result.Errs["a"] == nil {
		t.Fatalf("result = %#v, want target resolution failure", result)
	}
}

func TestMergeTaskMRs_RejectedReturnedSHAConflict(t *testing.T) {
	client := &mergeForgeClient{
		readiness: map[string]forge.MRReadiness{
			"feature/a": {Number: 1, State: "open", TargetBranch: "develop", HeadSHA: "a-sha", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true},
		},
		postMerge: map[int]forge.MRReadiness{
			1: {Number: 1, State: "merged", SourceBranch: "feature/a", TargetBranch: "develop", HeadSHA: "a-sha", MergedSHA: "authoritative-sha"},
		},
	}
	mgr, _ := newMRMergeTestManager(t, map[string]string{"a": "feature/a"}, "git@gitlab.com:group/repo.git", client)

	result, err := mgr.MergeTaskMRs(t.Context(), "TASK-1")
	if err != nil {
		t.Fatalf("MergeTaskMRs() err = %v", err)
	}
	if len(client.merges) != 1 {
		t.Fatalf("merges = %#v, want merge attempted", client.merges)
	}
	if result.Errs["a"] == nil || !strings.Contains(result.Errs["a"].Error(), "authoritative") {
		t.Fatalf("Errs[a] = %v, want returned-vs-authoritative SHA conflict", result.Errs["a"])
	}
	if !slices.Contains(result.Skipped, "a") || slices.Contains(result.Merged, "a") {
		t.Fatalf("result = %#v, want a failed per-service", result)
	}
}

func TestMergeTaskMRs_PostMergeIdentityDriftFailsService(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*forge.MRReadiness)
		wantErr string
	}{
		{name: "state not merged", mutate: func(r *forge.MRReadiness) { r.State = "open" }, wantErr: "state"},
		{name: "head drifted", mutate: func(r *forge.MRReadiness) { r.HeadSHA = "changed-sha" }, wantErr: "head"},
		{name: "source drifted", mutate: func(r *forge.MRReadiness) { r.SourceBranch = "feature/other" }, wantErr: "source"},
		{name: "target drifted", mutate: func(r *forge.MRReadiness) { r.TargetBranch = "master" }, wantErr: "target"},
		{name: "missing merge SHA", mutate: func(r *forge.MRReadiness) { r.MergedSHA = "" }, wantErr: "merge SHA"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fresh := forge.MRReadiness{Number: 1, State: "merged", SourceBranch: "feature/a", TargetBranch: "develop", HeadSHA: "a-sha", MergedSHA: "merge-sha"}
			tc.mutate(&fresh)
			client := &mergeForgeClient{
				readiness: map[string]forge.MRReadiness{
					"feature/a": {Number: 1, State: "open", SourceBranch: "feature/a", TargetBranch: "develop", HeadSHA: "a-sha", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true},
				},
				postMerge: map[int]forge.MRReadiness{1: fresh},
			}
			mgr, _ := newMRMergeTestManager(t, map[string]string{"a": "feature/a"}, "git@gitlab.com:group/repo.git", client)

			result, err := mgr.MergeTaskMRs(t.Context(), "TASK-1")
			if err != nil {
				t.Fatalf("MergeTaskMRs() err = %v", err)
			}
			if result.Errs["a"] == nil || !strings.Contains(result.Errs["a"].Error(), tc.wantErr) {
				t.Fatalf("Errs[a] = %v, want %q conflict", result.Errs["a"], tc.wantErr)
			}
			if slices.Contains(result.Merged, "a") {
				t.Fatalf("result = %#v, want a not merged", result)
			}
		})
	}
}

func newMergedReconcileFixture(t *testing.T, byNumber forge.MRReadiness, history []forge.MRInfo) (*manager, *mergeForgeClient, *mockGitClient) {
	t.Helper()
	client := &mergeForgeClient{
		readiness: map[string]forge.MRReadiness{
			"feature/a": {SourceBranch: "feature/a", Blockers: []string{"merge request not found"}},
		},
		history:           map[string][]forge.MRInfo{"feature/a": history},
		readinessByNumber: map[int]forge.MRReadiness{byNumber.Number: byNumber},
	}
	mgr, gitMock := newMRMergeTestManager(t, map[string]string{"a": "feature/a"}, "git@gitlab.com:group/repo.git", client)
	gitMock.resolveRefFn = func(_, ref string) (string, error) {
		if ref == "feature/a" {
			return "a-head", nil
		}
		return "origin/develop-sha", nil
	}
	gitMock.remoteRefSHAFn = func(_, ref string) (string, error) {
		if ref == "refs/heads/feature/a" {
			return "a-head", nil
		}
		return "", nil
	}
	gitMock.isAncestorFn = func(_, ancestor, descendant string) (bool, error) {
		return ancestor == "squash-sha" && descendant == "origin/develop-sha", nil
	}
	return mgr, client, gitMock
}

var mergedReconcileMR = forge.MRReadiness{Number: 7, State: "merged", SourceBranch: "feature/a", TargetBranch: "develop", HeadSHA: "a-head", MergedSHA: "squash-sha", URL: "url-7"}

var mergedReconcileHistory = []forge.MRInfo{{Number: 7, State: "merged", SourceBranch: "feature/a", TargetBranch: "develop", URL: "url-7"}}

func TestInspectTaskMerge_ExternallyMergedMRIsReconciled(t *testing.T) {
	mgr, _, _ := newMergedReconcileFixture(t, mergedReconcileMR, mergedReconcileHistory)

	inspection, err := mgr.InspectTaskMerge(t.Context(), "TASK-1")
	if err != nil {
		t.Fatalf("InspectTaskMerge() err = %v", err)
	}
	if len(inspection.Services) != 1 {
		t.Fatalf("services = %d, want 1", len(inspection.Services))
	}
	item := inspection.Services[0]
	if item.Status != "merged" {
		t.Fatalf("status = %q, want merged: %#v", item.Status, item)
	}
	if item.MR.Number != 7 || item.MR.MergedSHA != "squash-sha" {
		t.Fatalf("MR = %#v, want exact merged MR with authoritative squash SHA", item.MR)
	}
}

func TestMergeTaskMRs_ReconcilesExternallyMergedWithoutMergeCall(t *testing.T) {
	mgr, client, _ := newMergedReconcileFixture(t, mergedReconcileMR, mergedReconcileHistory)

	result, err := mgr.MergeTaskMRs(t.Context(), "TASK-1")
	if err != nil {
		t.Fatalf("MergeTaskMRs() err = %v", err)
	}
	if !slices.Equal(result.Merged, []string{"a"}) {
		t.Fatalf("Merged = %v, want [a]", result.Merged)
	}
	if len(client.merges) != 0 {
		t.Fatalf("merges = %#v, want no MergeMR call for externally merged MR", client.merges)
	}
	if !slices.Contains(result.Steps, "a: already merged") {
		t.Fatalf("Steps = %v, want already-merged step", result.Steps)
	}
}

func TestInspectTaskMerge_MergedReconciliationFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mutateMR  func(*forge.MRReadiness)
		history   []forge.MRInfo
		wantBlock string
	}{
		{
			name:      "ambiguous history",
			history:   append(slices.Clone(mergedReconcileHistory), forge.MRInfo{Number: 9, State: "merged", SourceBranch: "feature/a", TargetBranch: "develop"}),
			wantBlock: "ambiguous",
		},
		{
			name:      "closed without merge stays no_mr",
			history:   []forge.MRInfo{{Number: 5, State: "closed", SourceBranch: "feature/a", TargetBranch: "develop"}},
			wantBlock: "",
		},
		{
			name:      "state not merged",
			mutateMR:  func(r *forge.MRReadiness) { r.State = "open" },
			wantBlock: "not merged",
		},
		{
			name:      "source drift",
			mutateMR:  func(r *forge.MRReadiness) { r.SourceBranch = "feature/other" },
			wantBlock: "source",
		},
		{
			name:      "target drift",
			mutateMR:  func(r *forge.MRReadiness) { r.TargetBranch = "master" },
			wantBlock: "target",
		},
		{
			name:      "stale head",
			mutateMR:  func(r *forge.MRReadiness) { r.HeadSHA = "old-head" },
			wantBlock: "head",
		},
		{
			name:      "empty head",
			mutateMR:  func(r *forge.MRReadiness) { r.HeadSHA = "" },
			wantBlock: "head",
		},
		{
			name:      "missing merged SHA",
			mutateMR:  func(r *forge.MRReadiness) { r.MergedSHA = "" },
			wantBlock: "merged SHA",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mr := mergedReconcileMR
			if tc.mutateMR != nil {
				tc.mutateMR(&mr)
			}
			history := tc.history
			if history == nil {
				history = mergedReconcileHistory
			}
			mgr, _, _ := newMergedReconcileFixture(t, mr, history)

			inspection, err := mgr.InspectTaskMerge(t.Context(), "TASK-1")
			if err != nil {
				t.Fatalf("InspectTaskMerge() err = %v", err)
			}
			item := inspection.Services[0]
			if tc.wantBlock == "" {
				if item.Status != "no_mr" {
					t.Fatalf("status = %q, want no_mr: %#v", item.Status, item)
				}
				return
			}
			if item.Status != "failed" {
				t.Fatalf("status = %q, want failed: %#v", item.Status, item)
			}
			if !slices.ContainsFunc(item.Blockers, func(b string) bool { return strings.Contains(b, tc.wantBlock) }) {
				t.Fatalf("blockers = %v, want one containing %q", item.Blockers, tc.wantBlock)
			}
		})
	}

	t.Run("merged SHA not contained in target", func(t *testing.T) {
		mgr, _, gitMock := newMergedReconcileFixture(t, mergedReconcileMR, mergedReconcileHistory)
		gitMock.isAncestorFn = func(_, _, _ string) (bool, error) { return false, nil }

		inspection, err := mgr.InspectTaskMerge(t.Context(), "TASK-1")
		if err != nil {
			t.Fatalf("InspectTaskMerge() err = %v", err)
		}
		item := inspection.Services[0]
		if item.Status != "failed" {
			t.Fatalf("status = %q, want failed: %#v", item.Status, item)
		}
		if !slices.ContainsFunc(item.Blockers, func(b string) bool { return strings.Contains(b, "not contained") }) {
			t.Fatalf("blockers = %v, want containment failure", item.Blockers)
		}
	})

	t.Run("source without remote ref fails closed", func(t *testing.T) {
		mgr, _, gitMock := newMergedReconcileFixture(t, mergedReconcileMR, mergedReconcileHistory)
		gitMock.remoteRefSHAFn = func(_, _ string) (string, error) { return "", nil }

		inspection, err := mgr.InspectTaskMerge(t.Context(), "TASK-1")
		if err != nil {
			t.Fatalf("InspectTaskMerge() err = %v", err)
		}
		if item := inspection.Services[0]; item.Status != "failed" {
			t.Fatalf("status = %q, want failed: %#v", item.Status, item)
		}
	})
}

func newMRMergeTestManager(t *testing.T, services map[string]string, remoteURL string, client forge.ForgeClient) (*manager, *mockGitClient) {
	t.Helper()
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskDir := filepath.Join(tasksRoot, "TASK-1")
	worktrees := make(map[string]git.WorktreeEntry, len(services))
	for name, branch := range services {
		path := filepath.Join(taskDir, name)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		worktrees[filepath.Join(rootDir, "repos", name)] = git.WorktreeEntry{Path: path, Branch: "refs/heads/" + branch}
	}
	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			return filepath.Join(rootDir, "repos", filepath.Base(path), ".git"), nil
		},
		listWorktreesFn: func(repoPath string) ([]git.WorktreeEntry, error) {
			return []git.WorktreeEntry{worktrees[repoPath]}, nil
		},
		remoteURLRes: remoteURL,
	}
	cfg := newCloseTestConfig(rootDir, tasksRoot)
	mgr := newTestManagerWithDeps(t, cfg, gitMock, nil, map[forge.ForgeProvider]forge.ForgeClient{
		forge.ForgeProviderGitLab: client,
	})
	return mgr.(*manager), gitMock
}
