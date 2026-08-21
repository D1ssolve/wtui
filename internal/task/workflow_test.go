package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

type workflowForgeClient struct {
	readiness map[string]forge.MRReadiness
	readErrs  map[string]error
	reads     atomic.Int32
}

func (f *workflowForgeClient) Provider() forge.ForgeProvider    { return forge.ForgeProviderGitLab }
func (f *workflowForgeClient) IsAvailable(context.Context) bool { return true }
func (f *workflowForgeClient) CreateMR(context.Context, forge.CreateMRParams) (forge.MRInfo, error) {
	return forge.MRInfo{}, nil
}
func (f *workflowForgeClient) MRStatus(context.Context, string, string) ([]forge.MRInfo, error) {
	return nil, nil
}
func (f *workflowForgeClient) MRReadiness(_ context.Context, branch, _, _ string) (forge.MRReadiness, error) {
	f.reads.Add(1)
	return f.readiness[branch], f.readErrs[branch]
}
func (f *workflowForgeClient) MRReadinessByNumber(context.Context, int, string, string) (forge.MRReadiness, error) {
	return forge.MRReadiness{}, nil
}
func (f *workflowForgeClient) MergeMR(context.Context, forge.MergeMRParams) (forge.MRMergeResult, error) {
	return forge.MRMergeResult{}, nil
}
func (f *workflowForgeClient) PipelineStatus(context.Context, string, string) ([]forge.PipelineStatus, error) {
	return nil, nil
}
func (f *workflowForgeClient) TriggerPipeline(context.Context, forge.TriggerPipelineParams) error {
	return nil
}
func (f *workflowForgeClient) ListIssues(context.Context, forge.ListIssuesParams) ([]forge.IssueInfo, error) {
	return nil, nil
}

func TestTaskWorkflow_PhaseTransitions(t *testing.T) {
	tests := []struct {
		name       string
		readiness  map[string]forge.MRReadiness
		merged     bool
		pushed     bool
		want       domain.WorkflowPhase
		wantNext   string
		wantBlock  string
		wantMRRead int
	}{
		{
			name:       "code before any merge request exists",
			readiness:  map[string]forge.MRReadiness{},
			want:       domain.TaskWorkflowCode,
			wantNext:   "press C to create MRs",
			wantMRRead: 2,
		},
		{
			name:       "mr when clean branches are pushed but merge requests are missing",
			readiness:  map[string]forge.MRReadiness{},
			pushed:     true,
			want:       domain.TaskWorkflowMR,
			wantNext:   "press C to create MRs",
			wantMRRead: 2,
		},
		{
			name: "review and ci while no merge request is ready",
			readiness: map[string]forge.MRReadiness{
				"feature/a": {Number: 1, State: "open", Blockers: []string{"not approved"}},
				"feature/b": {Number: 2, State: "open", Blockers: []string{"checks pending"}},
			},
			want:       domain.TaskWorkflowReviewCI,
			wantNext:   "wait for review/CI, then M",
			wantBlock:  "2 services waiting for approval/CI",
			wantMRRead: 2,
		},
		{
			name: "merge when at least one merge request is ready",
			readiness: map[string]forge.MRReadiness{
				"feature/a": {Number: 1, State: "open", Ready: true},
				"feature/b": {Number: 2, State: "open", Blockers: []string{"checks pending"}},
			},
			want:       domain.TaskWorkflowMerge,
			wantNext:   "press M to merge ready MRs",
			wantMRRead: 2,
		},
		{
			name:       "release eligible when every branch is merged",
			merged:     true,
			want:       domain.TaskWorkflowReleaseEligible,
			wantNext:   "select in release (N)",
			wantMRRead: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, client := newWorkflowTestManager(t, tt.readiness, tt.merged, tt.pushed)
			summary, err := mgr.TaskWorkflow(t.Context(), "TASK-1")
			if err != nil {
				t.Fatalf("TaskWorkflow() err = %v", err)
			}
			if summary.Current != tt.want || summary.NextAction != tt.wantNext {
				t.Fatalf("summary = %#v, want current %q and next %q", summary, tt.want, tt.wantNext)
			}
			if summary.Blocker != tt.wantBlock {
				t.Fatalf("Blocker = %q, want %q", summary.Blocker, tt.wantBlock)
			}
			if reads := int(client.reads.Load()); reads != tt.wantMRRead {
				t.Fatalf("forge reads = %d, want %d", reads, tt.wantMRRead)
			}
			assertWorkflowStates(t, summary)
		})
	}
}

func TestTaskWorkflow_IncludesPerServiceInspectionRows(t *testing.T) {
	mgr, _ := newWorkflowTestManager(t, map[string]forge.MRReadiness{
		"feature/a": {Number: 1, State: "open", Ready: true},
		"feature/b": {Number: 2, State: "open", Blockers: []string{"checks pending"}},
	}, false, true)

	summary, err := mgr.TaskWorkflow(t.Context(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.ServiceWorkflow{
		{ServiceName: "a", Status: "ready"},
		{ServiceName: "b", Status: "waiting", Detail: "checks pending"},
	}
	if !reflect.DeepEqual(summary.Services, want) {
		t.Fatalf("Services = %#v, want %#v", summary.Services, want)
	}
}

func TestTaskWorkflow_ForgeFailureDoesNotReportMissingMR(t *testing.T) {
	mgr, client := newWorkflowTestManager(t, nil, false, true)
	client.readErrs = map[string]error{"feature/a": errors.New("forge unknown: ERROR")}

	summary, err := mgr.TaskWorkflow(t.Context(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Current != domain.TaskWorkflowReviewCI || summary.NextAction != "fix blockers, then M" {
		t.Fatalf("summary = %#v, want blocked review workflow", summary)
	}
	if !strings.Contains(summary.Blocker, "a: forge unknown: ERROR") {
		t.Fatalf("Blocker = %q, want forge error", summary.Blocker)
	}
	if strings.Contains(summary.NextAction, "create MRs") {
		t.Fatalf("NextAction = %q, must not report missing MR", summary.NextAction)
	}
}

func TestTaskWorkflow_FetchesBeforeCheckingOriginIntegrationBranch(t *testing.T) {
	mgr, _ := newWorkflowTestManager(t, nil, false, true)
	gitMock := mgr.(*manager).git.(*mockGitClient)
	var fetched atomic.Bool
	gitMock.fetchFn = func(string) error {
		fetched.Store(true)
		return nil
	}
	gitMock.isAncestorFn = func(_, _, _ string) (bool, error) {
		return fetched.Load(), nil
	}

	summary, err := mgr.TaskWorkflow(t.Context(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Current != domain.TaskWorkflowReleaseEligible {
		t.Fatalf("Current = %q, want %q after fresh origin/develop check", summary.Current, domain.TaskWorkflowReleaseEligible)
	}
	gitMock.mu.Lock()
	fetchCount := len(gitMock.fetchCalls)
	gitMock.mu.Unlock()
	if fetchCount != 2 {
		t.Fatalf("Fetch calls = %d, want one per service", fetchCount)
	}
}

func TestTaskWorkflow_BlockedService_ShowsBlockerDetail(t *testing.T) {
	mgr, _ := newWorkflowTestManager(t, map[string]forge.MRReadiness{
		"feature/a": {Number: 1, State: "open", Blockers: []string{"merge blocked: need rebase"}},
		"feature/b": {Number: 2, State: "open", Blockers: []string{"checks pending"}},
	}, false, true)

	summary, err := mgr.TaskWorkflow(t.Context(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Current != domain.TaskWorkflowReviewCI {
		t.Fatalf("Current = %q, want %q", summary.Current, domain.TaskWorkflowReviewCI)
	}
	if summary.NextAction != "fix blockers, then M" {
		t.Fatalf("NextAction = %q, want %q", summary.NextAction, "fix blockers, then M")
	}
	wantBlocker := "a: merge blocked: need rebase"
	if summary.Blocker != wantBlocker {
		t.Fatalf("Blocker = %q, want %q", summary.Blocker, wantBlocker)
	}
	for _, step := range summary.Steps {
		switch step.Phase {
		case domain.TaskWorkflowReviewCI:
			if step.State != "blocked" {
				t.Fatalf("review_ci state = %q, want blocked", step.State)
			}
		case domain.TaskWorkflowCode, domain.TaskWorkflowMR:
			if step.State != "done" {
				t.Fatalf("%s state = %q, want done", step.Phase, step.State)
			}
		default:
			if step.State != "next" {
				t.Fatalf("%s state = %q, want next", step.Phase, step.State)
			}
		}
	}
}

func TestReleaseWorkflow_StatusMapping(t *testing.T) {
	tests := []struct {
		status      domain.ReleaseStatus
		want        domain.WorkflowPhase
		wantNext    string
		wantBlocker string
	}{
		{domain.ReleaseStatusDraft, domain.ReleaseWorkflowDevelop, "prepare release", ""},
		{domain.ReleaseStatusValidating, domain.ReleaseWorkflowDevelop, "validating release", ""},
		{domain.ReleaseStatusMerging, domain.ReleaseWorkflowDevelop, "preparing release", ""},
		{domain.ReleaseStatusBranching, domain.ReleaseWorkflowReleaseBranch, "creating release branches", ""},
		{domain.ReleaseStatusPushing, domain.ReleaseWorkflowReleaseBranch, "pushing release branches", ""},
		{domain.ReleaseStatusPrepared, domain.ReleaseWorkflowRegression, "press F to create master MRs", ""},
		{domain.ReleaseStatusAwaitingMasterMerge, domain.ReleaseWorkflowMasterMR, "press M to merge ready MRs", ""},
		{domain.ReleaseStatusMasterMerged, domain.ReleaseWorkflowTag, "press F to sync develop and tag", ""},
		{domain.ReleaseStatusSyncingDevelop, domain.ReleaseWorkflowTag, "syncing develop", ""},
		{domain.ReleaseStatusTagging, domain.ReleaseWorkflowTag, "tagging release", ""},
		{domain.ReleaseStatusReleased, domain.ReleaseWorkflowTag, "release complete", ""},
		{domain.ReleaseStatusFailed, domain.ReleaseWorkflowDevelop, "release failed", "tag push failed"},
		{domain.ReleaseStatusRejected, domain.ReleaseWorkflowDevelop, "release rejected", ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			release := domain.Release{Status: tt.status}
			if tt.status == domain.ReleaseStatusFailed {
				release.Error = &domain.ReleaseError{Message: "tag push failed"}
			}
			summary := ReleaseWorkflow(release)
			if summary.Current != tt.want || summary.NextAction != tt.wantNext || summary.Blocker != tt.wantBlocker {
				t.Fatalf("ReleaseWorkflow() = %#v, want current=%q next=%q blocker=%q", summary, tt.want, tt.wantNext, tt.wantBlocker)
			}
			if len(summary.Steps) != 5 {
				t.Fatalf("steps = %d, want 5", len(summary.Steps))
			}
			if tt.status == domain.ReleaseStatusReleased {
				for _, step := range summary.Steps {
					if step.State != "done" {
						t.Fatalf("released step %q state = %q, want done", step.Phase, step.State)
					}
				}
			}
			if tt.status == domain.ReleaseStatusFailed && summary.Steps[0].State != "blocked" {
				t.Fatalf("failed current state = %q, want blocked", summary.Steps[0].State)
			}
		})
	}
}

func TestReleaseWorkflow_IncludesPerServiceRows(t *testing.T) {
	release := domain.Release{Services: []domain.ReleaseService{
		{Name: "api", Status: domain.ReleaseStatusAwaitingMasterMerge, ProductionMR: &domain.ProductionMRRef{Number: 42, State: "open"}},
		{Name: "worker", Status: domain.ReleaseStatusFailed, Error: &domain.ReleaseError{Message: "CI failed"}},
	}}

	summary := ReleaseWorkflow(release)
	want := []domain.ServiceWorkflow{
		{ServiceName: "api", Status: "awaiting_master_merge", Detail: "MR #42: open"},
		{ServiceName: "worker", Status: "failed", Detail: "CI failed"},
	}
	if !reflect.DeepEqual(summary.Services, want) {
		t.Fatalf("Services = %#v, want %#v", summary.Services, want)
	}
}

func newWorkflowTestManager(t *testing.T, readiness map[string]forge.MRReadiness, merged, pushed bool) (Manager, *workflowForgeClient) {
	t.Helper()
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskDir := filepath.Join(tasksRoot, "TASK-1")
	worktrees := make(map[string]git.WorktreeEntry, 2)
	for _, name := range []string{"a", "b"} {
		path := filepath.Join(taskDir, name)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		worktrees[filepath.Join(rootDir, "repos", name)] = git.WorktreeEntry{Path: path, Branch: "refs/heads/feature/" + name}
	}
	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			return filepath.Join(rootDir, "repos", filepath.Base(path), ".git"), nil
		},
		listWorktreesFn: func(repoPath string) ([]git.WorktreeEntry, error) {
			return []git.WorktreeEntry{worktrees[repoPath]}, nil
		},
		remoteURLRes:          "git@gitlab.com:group/repo.git",
		remoteBranchExistsRes: pushed,
		isAncestorFn: func(_, _, descendant string) (bool, error) {
			if descendant != "origin/develop" {
				t.Fatalf("descendant = %q, want origin/develop", descendant)
			}
			return merged, nil
		},
	}
	client := &workflowForgeClient{readiness: readiness}
	flow := &gitflow.ResolvedGitFlow{IntegrationBranch: "develop"}
	mgr := newTestManagerWithDeps(t, newCloseTestConfig(rootDir, tasksRoot), gitMock, flow, map[forge.ForgeProvider]forge.ForgeClient{
		forge.ForgeProviderGitLab: client,
	})
	return mgr, client
}

func assertWorkflowStates(t *testing.T, summary domain.WorkflowSummary) {
	t.Helper()
	foundNow := false
	for _, step := range summary.Steps {
		if step.Phase == summary.Current && step.State == "now" {
			foundNow = true
		}
		if !strings.Contains(" done now next blocked ", " "+step.State+" ") {
			t.Fatalf("invalid state %q", step.State)
		}
	}
	if summary.Current != domain.TaskWorkflowReleaseEligible && !foundNow {
		t.Fatalf("current phase %q has no now step: %#v", summary.Current, summary.Steps)
	}
}
