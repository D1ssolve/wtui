package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/git"
)

type mockForgeClient struct {
	createMRFn            func(ctx context.Context, params forge.CreateMRParams) (forge.MRInfo, error)
	mrStatusFn            func(ctx context.Context, sourceBranch, repo string) ([]forge.MRInfo, error)
	mrHistoryFn           func(ctx context.Context, sourceBranch, repo string) ([]forge.MRInfo, error)
	mrReadinessByNumberFn func(ctx context.Context, number int, repo, worktreePath string) (forge.MRReadiness, error)
	pipelineStatusFn      func(ctx context.Context, branch, repo string) ([]forge.PipelineStatus, error)
	triggerPipelineFn     func(ctx context.Context, params forge.TriggerPipelineParams) error

	createdCount *int
}

func (m *mockForgeClient) Provider() forge.ForgeProvider { return forge.ForgeProviderGitLab }
func (m *mockForgeClient) IsAvailable(_ context.Context) bool {
	return true
}

func (m *mockForgeClient) CreateMR(ctx context.Context, params forge.CreateMRParams) (forge.MRInfo, error) {
	if m.createdCount != nil {
		*m.createdCount++
	}
	if m.createMRFn != nil {
		return m.createMRFn(ctx, params)
	}
	return forge.MRInfo{}, nil
}

func (m *mockForgeClient) MRHistory(ctx context.Context, sourceBranch, repo string) ([]forge.MRInfo, error) {
	if m.mrHistoryFn != nil {
		return m.mrHistoryFn(ctx, sourceBranch, repo)
	}
	return nil, nil
}

func (m *mockForgeClient) MRStatus(ctx context.Context, sourceBranch, repo string) ([]forge.MRInfo, error) {
	if m.mrStatusFn != nil {
		return m.mrStatusFn(ctx, sourceBranch, repo)
	}
	return nil, nil
}
func (m *mockForgeClient) PipelineStatus(ctx context.Context, branch, repo string) ([]forge.PipelineStatus, error) {
	if m.pipelineStatusFn != nil {
		return m.pipelineStatusFn(ctx, branch, repo)
	}
	return nil, nil
}
func (m *mockForgeClient) TriggerPipeline(ctx context.Context, params forge.TriggerPipelineParams) error {
	if m.triggerPipelineFn != nil {
		return m.triggerPipelineFn(ctx, params)
	}
	return nil
}
func (m *mockForgeClient) ListIssues(_ context.Context, _ forge.ListIssuesParams) ([]forge.IssueInfo, error) {
	return nil, nil
}

func TestForgePipelineStatus_UsesRepoExtractedFromServiceRemote(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskID := "IN-FORGE-PIPELINE"
	servicePath := filepath.Join(tasksRoot, taskID, "svc")
	if err := os.MkdirAll(servicePath, 0o755); err != nil {
		t.Fatal(err)
	}

	fakeCommonDir := filepath.Join(rootDir, "repos", "svc", ".git")
	if err := os.MkdirAll(fakeCommonDir, 0o755); err != nil {
		t.Fatal(err)
	}

	gitMock := &mockGitClient{
		commonDirFn:      func(path string) (string, error) { return fakeCommonDir, nil },
		listWorktreesRes: []git.WorktreeEntry{{Path: servicePath, Branch: "refs/heads/feature/IN-FORGE-PIPELINE"}},
		repoStatusFn:     func(string) (git.RawStatus, error) { return git.RawStatus{Branch: "feature/IN-FORGE-PIPELINE"}, nil },
		remoteURLRes:     "git@gitlab.com:group/svc.git",
	}

	var gotBranch string
	var gotRepo string
	forgeClient := &mockForgeClient{
		pipelineStatusFn: func(_ context.Context, branch, repo string) ([]forge.PipelineStatus, error) {
			gotBranch = branch
			gotRepo = repo
			return []forge.PipelineStatus{{Status: "success", Branch: branch}}, nil
		},
	}

	mgr := newTestManagerWithDeps(t, newCloseTestConfig(rootDir, tasksRoot), gitMock, nil, map[forge.ForgeProvider]forge.ForgeClient{
		forge.ForgeProviderGitLab: forgeClient,
	})

	_, err := mgr.ForgePipelineStatus(context.Background(), taskID, "svc", "")
	if err != nil {
		t.Fatalf("ForgePipelineStatus error: %v", err)
	}

	if gotBranch != "feature/IN-FORGE-PIPELINE" {
		t.Fatalf("branch = %q, want %q", gotBranch, "feature/IN-FORGE-PIPELINE")
	}
	if gotRepo != "gitlab.com/group/svc" {
		t.Fatalf("repo = %q, want %q", gotRepo, "gitlab.com/group/svc")
	}
}

func TestForgeCreateMissingMRs_BlankTitleDefaultsToTaskID(t *testing.T) {
	var titles []string
	client := &mockForgeClient{createMRFn: func(_ context.Context, params forge.CreateMRParams) (forge.MRInfo, error) {
		titles = append(titles, params.Title)
		return forge.MRInfo{}, nil
	}}
	mgr := newForgeTaskTestManager(t, client)

	if _, err := mgr.ForgeCreateMissingMRs(t.Context(), "IN-FORGE-MRS", "", false); err != nil {
		t.Fatalf("ForgeCreateMissingMRs() err = %v", err)
	}
	if len(titles) != 2 || titles[0] != "IN-FORGE-MRS" || titles[1] != "IN-FORGE-MRS" {
		t.Fatalf("titles = %#v", titles)
	}
}

func TestForgeCreateMissingMRs_CreatesOnlyMissing(t *testing.T) {
	var created []forge.CreateMRParams
	client := &mockForgeClient{
		mrStatusFn: func(_ context.Context, sourceBranch, _ string) ([]forge.MRInfo, error) {
			if strings.HasSuffix(sourceBranch, "/api") {
				return []forge.MRInfo{{Number: 7, URL: "https://gitlab.example/api/7"}}, nil
			}
			return nil, nil
		},
		createMRFn: func(_ context.Context, params forge.CreateMRParams) (forge.MRInfo, error) {
			created = append(created, params)
			return forge.MRInfo{Number: 8, URL: "https://gitlab.example/worker/8"}, nil
		},
	}
	mgr := newForgeTaskTestManager(t, client)

	result, err := mgr.ForgeCreateMissingMRs(t.Context(), "IN-FORGE-MRS", "Shared title", false)
	if err != nil {
		t.Fatalf("ForgeCreateMissingMRs() err = %v", err)
	}
	if len(created) != 1 || !strings.HasSuffix(created[0].SourceBranch, "/worker") {
		t.Fatalf("created = %#v, want only worker", created)
	}
	if created[0].Title != "Shared title" || created[0].TargetBranch != "develop" {
		t.Fatalf("create params = %#v", created[0])
	}
	if len(result.Services) != 2 || result.Services[0].ServiceName != "api" || result.Services[0].Status != "existing" || result.Services[1].ServiceName != "worker" || result.Services[1].Status != "created" {
		t.Fatalf("result = %#v", result)
	}
}

func TestForgeCreateMissingMRs_ServiceFailureDoesNotStopRemainingServices(t *testing.T) {
	client := &mockForgeClient{
		mrStatusFn: func(_ context.Context, sourceBranch, _ string) ([]forge.MRInfo, error) {
			if strings.HasSuffix(sourceBranch, "/api") {
				return nil, errors.New("status unavailable")
			}
			return nil, nil
		},
		createMRFn: func(_ context.Context, params forge.CreateMRParams) (forge.MRInfo, error) {
			return forge.MRInfo{URL: "https://gitlab.example/worker/8"}, nil
		},
	}
	mgr := newForgeTaskTestManager(t, client)

	result, err := mgr.ForgeCreateMissingMRs(t.Context(), "IN-FORGE-MRS", "", false)
	if err != nil {
		t.Fatalf("ForgeCreateMissingMRs() err = %v", err)
	}
	if len(result.Services) != 2 || result.Services[0].Status != "failed" || result.Services[0].Err == nil || result.Services[1].Status != "created" {
		t.Fatalf("result = %#v", result)
	}
}

type historyForgeClient struct {
	*mockForgeClient
	mrHistoryFn func(ctx context.Context, branch, repo string) ([]forge.MRInfo, error)
}

func (m *historyForgeClient) MRHistory(ctx context.Context, branch, repo string) ([]forge.MRInfo, error) {
	return m.mrHistoryFn(ctx, branch, repo)
}

func TestForgeCreateMissingMRs_ClosedMRHistoryRequiresConfirmation(t *testing.T) {
	created := 0
	client := &historyForgeClient{
		mockForgeClient: &mockForgeClient{createMRFn: func(_ context.Context, params forge.CreateMRParams) (forge.MRInfo, error) {
			created++
			return forge.MRInfo{Number: 9, URL: "https://gitlab.example/api/9"}, nil
		}},
		mrHistoryFn: func(_ context.Context, branch, _ string) ([]forge.MRInfo, error) {
			if strings.HasSuffix(branch, "/api") {
				return []forge.MRInfo{{Number: 3, State: "closed", SourceBranch: branch, TargetBranch: "develop"}}, nil
			}
			return nil, nil
		},
	}
	mgr := newForgeTaskTestManager(t, client)

	result, err := mgr.ForgeCreateMissingMRs(t.Context(), "IN-FORGE-MRS", "", false)
	if err != nil {
		t.Fatalf("ForgeCreateMissingMRs() err = %v", err)
	}
	if created != 1 || result.Services[0].Status != "confirm" || !strings.Contains(result.Services[0].Reason, "#3") || result.Services[1].Status != "created" {
		t.Fatalf("result = %#v, created = %d", result, created)
	}

	result, err = mgr.ForgeCreateMissingMRs(t.Context(), "IN-FORGE-MRS", "", true)
	if err != nil {
		t.Fatalf("ForgeCreateMissingMRs(force) err = %v", err)
	}
	if created != 3 || result.Services[0].Status != "created" {
		t.Fatalf("force result = %#v, created = %d", result, created)
	}
}

func TestForgeCreateMissingMRs_NoDiffRequiresConfirmation(t *testing.T) {
	created := 0
	client := &mockForgeClient{createMRFn: func(_ context.Context, params forge.CreateMRParams) (forge.MRInfo, error) {
		created++
		return forge.MRInfo{Number: 9, URL: "https://gitlab.example/api/9"}, nil
	}}
	mgr := newForgeTaskTestManager(t, client)
	mgr.(*manager).git.(*mockGitClient).revListCountFn = func(_, _, _ string) (int, error) { return 0, nil }

	result, err := mgr.ForgeCreateMissingMRs(t.Context(), "IN-FORGE-MRS", "", false)
	if err != nil {
		t.Fatalf("ForgeCreateMissingMRs() err = %v", err)
	}
	if created != 0 || result.Services[0].Status != "confirm" || !strings.Contains(result.Services[0].Reason, "no changes") {
		t.Fatalf("result = %#v, created = %d", result, created)
	}
}

func newForgeTaskTestManager(t *testing.T, client forge.ForgeClient) Manager {
	t.Helper()
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskID := "IN-FORGE-MRS"
	for _, serviceName := range []string{"api", "worker"} {
		if err := os.MkdirAll(filepath.Join(tasksRoot, taskID, serviceName), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			return filepath.Join(rootDir, "repos", filepath.Base(path), ".git"), nil
		},
		listWorktreesFn: func(repoPath string) ([]git.WorktreeEntry, error) {
			serviceName := filepath.Base(repoPath)
			return []git.WorktreeEntry{{
				Path:   filepath.Join(tasksRoot, taskID, serviceName),
				Branch: "refs/heads/feature/" + taskID + "/" + serviceName,
			}}, nil
		},
		remoteURLRes: "git@gitlab.com:group/project.git",
		revListCountFn: func(_, _, _ string) (int, error) {
			return 1, nil
		},
	}
	return newTestManagerWithDeps(t, newCloseTestConfig(rootDir, tasksRoot), gitMock, nil, map[forge.ForgeProvider]forge.ForgeClient{
		forge.ForgeProviderGitLab: client,
	})
}

func TestForgePipelineStatus_ReturnsErrorWhenServiceRemoteUnparseable(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskID := "IN-FORGE-PIPELINE-ERR"
	servicePath := filepath.Join(tasksRoot, taskID, "svc")
	if err := os.MkdirAll(servicePath, 0o755); err != nil {
		t.Fatal(err)
	}

	fakeCommonDir := filepath.Join(rootDir, "repos", "svc", ".git")
	if err := os.MkdirAll(fakeCommonDir, 0o755); err != nil {
		t.Fatal(err)
	}

	gitMock := &mockGitClient{
		commonDirFn:      func(path string) (string, error) { return fakeCommonDir, nil },
		listWorktreesRes: []git.WorktreeEntry{{Path: servicePath, Branch: "refs/heads/feature/IN-FORGE-PIPELINE-ERR"}},
		repoStatusFn: func(string) (git.RawStatus, error) {
			return git.RawStatus{Branch: "feature/IN-FORGE-PIPELINE-ERR"}, nil
		},
		remoteURLRes: "git@gitlab.com:",
	}

	forgeClient := &mockForgeClient{}
	mgr := newTestManagerWithDeps(t, newCloseTestConfig(rootDir, tasksRoot), gitMock, nil, map[forge.ForgeProvider]forge.ForgeClient{
		forge.ForgeProviderGitLab: forgeClient,
	})

	_, err := mgr.ForgePipelineStatus(context.Background(), taskID, "svc", "")
	if err == nil {
		t.Fatal("ForgePipelineStatus error = nil, want parse error")
	}
	if got := err.Error(); !strings.Contains(got, "not parseable") {
		t.Fatalf("error = %q, want parseable hint", got)
	}
}
