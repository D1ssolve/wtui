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
	createMRFn       func(ctx context.Context, params forge.CreateMRParams) (forge.MRInfo, error)
	mrStatusFn       func(ctx context.Context, sourceBranch, repo string) ([]forge.MRInfo, error)
	pipelineStatusFn func(ctx context.Context, branch, repo string) ([]forge.PipelineStatus, error)
}

func (m *mockForgeClient) Provider() forge.ForgeProvider { return forge.ForgeProviderGitLab }
func (m *mockForgeClient) IsAvailable(_ context.Context) bool {
	return true
}

func (m *mockForgeClient) CreateMR(ctx context.Context, params forge.CreateMRParams) (forge.MRInfo, error) {
	if m.createMRFn != nil {
		return m.createMRFn(ctx, params)
	}
	return forge.MRInfo{}, nil
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
func (m *mockForgeClient) TriggerPipeline(_ context.Context, _ forge.TriggerPipelineParams) error {
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

	if _, err := mgr.ForgeCreateMissingMRs(t.Context(), "IN-FORGE-MRS", ""); err != nil {
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

	result, err := mgr.ForgeCreateMissingMRs(t.Context(), "IN-FORGE-MRS", "Shared title")
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

	result, err := mgr.ForgeCreateMissingMRs(t.Context(), "IN-FORGE-MRS", "")
	if err != nil {
		t.Fatalf("ForgeCreateMissingMRs() err = %v", err)
	}
	if len(result.Services) != 2 || result.Services[0].Status != "failed" || result.Services[0].Err == nil || result.Services[1].Status != "created" {
		t.Fatalf("result = %#v", result)
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
