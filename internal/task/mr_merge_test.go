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
func (f *mergeForgeClient) MRReadiness(_ context.Context, branch, _, _ string) (forge.MRReadiness, error) {
	return f.readiness[branch], f.readErrs[branch]
}
func (f *mergeForgeClient) MRReadinessByNumber(_ context.Context, number int, _, _ string) (forge.MRReadiness, error) {
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
		"feature/ready":   {Number: 1, State: "open", HeadSHA: "ready-sha", Ready: true, SupportsSHAPin: true},
		"feature/blocked": {Number: 2, State: "open", Blockers: []string{"checks failing"}, SupportsSHAPin: true},
	}}
	mgr := newMRMergeTestManager(t, map[string]string{"ready": "feature/ready", "blocked": "feature/blocked"}, "git@gitlab.com:group/repo.git", client)

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
}

func TestMergeServiceMR_MergesOnlySelectedService(t *testing.T) {
	client := &mergeForgeClient{readiness: map[string]forge.MRReadiness{
		"feature/api":    {Number: 1, State: "open", HeadSHA: "api-sha", Ready: true, SupportsSHAPin: true},
		"feature/worker": {Number: 2, State: "open", HeadSHA: "worker-sha", Ready: true, SupportsSHAPin: true},
	}}
	mgr := newMRMergeTestManager(t, map[string]string{"api": "feature/api", "worker": "feature/worker"}, "git@gitlab.com:group/repo.git", client)

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
			"feature/a": {Number: 1, State: "open", HeadSHA: "old-sha", Ready: true, SupportsSHAPin: true},
			"feature/b": {Number: 2, State: "open", HeadSHA: "b-sha", Ready: true, SupportsSHAPin: true},
		},
		mergeErrs: map[int]error{1: headDrift},
	}
	mgr := newMRMergeTestManager(t, map[string]string{"a": "feature/a", "b": "feature/b"}, "git@gitlab.com:group/repo.git", client)

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

func TestMergeTaskMRs_OmitsHeadSHAWhenForgeCannotPin(t *testing.T) {
	client := &mergeForgeClient{
		readiness: map[string]forge.MRReadiness{
			"feature/a": {Number: 1, State: "open", HeadSHA: "observed-sha", Ready: true, SupportsSHAPin: false},
		},
		readinessByNumber: map[int]forge.MRReadiness{1: {Number: 1, HeadSHA: "observed-sha"}},
	}
	mgr := newMRMergeTestManager(t, map[string]string{"a": "feature/a"}, "git@gitlab.com:group/repo.git", client)

	if _, err := mgr.MergeTaskMRs(t.Context(), "TASK-1"); err != nil {
		t.Fatalf("MergeTaskMRs() err = %v", err)
	}
	if len(client.merges) != 1 || client.merges[0].ExpectedHeadSHA != "" {
		t.Fatalf("merges = %#v, want unpinned merge", client.merges)
	}
}

func TestMergeTaskMRs_UnpinnedHeadDriftSkipsMerge(t *testing.T) {
	client := &mergeForgeClient{
		readiness: map[string]forge.MRReadiness{
			"feature/a": {Number: 17, State: "open", HeadSHA: "inspected-sha", Ready: true},
		},
		readinessByNumber: map[int]forge.MRReadiness{17: {Number: 17, HeadSHA: "changed-sha"}},
	}
	mgr := newMRMergeTestManager(t, map[string]string{"a": "feature/a"}, "git@gitlab.com:group/repo.git", client)

	result, err := mgr.MergeTaskMRs(t.Context(), "TASK-1")
	if err != nil {
		t.Fatalf("MergeTaskMRs() err = %v", err)
	}
	if len(client.merges) != 0 || result.Errs["a"] == nil || !strings.Contains(result.Errs["a"].Error(), "head SHA drift") {
		t.Fatalf("merges = %#v, result = %#v", client.merges, result)
	}
}

func TestInspectTaskMerge_NoMRIsSkippedByMerge(t *testing.T) {
	client := &mergeForgeClient{readiness: map[string]forge.MRReadiness{
		"feature/a": {SourceBranch: "feature/a", Blockers: []string{"merge request not found"}},
	}}
	mgr := newMRMergeTestManager(t, map[string]string{"a": "feature/a"}, "git@gitlab.com:group/repo.git", client)

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
	mgr := newMRMergeTestManager(t, map[string]string{"a": "feature/a"}, "git@gitlab.com:", client)

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

func newMRMergeTestManager(t *testing.T, services map[string]string, remoteURL string, client forge.ForgeClient) Manager {
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
	return newTestManagerWithDeps(t, cfg, gitMock, nil, map[forge.ForgeProvider]forge.ForgeClient{
		forge.ForgeProviderGitLab: client,
	})
}
