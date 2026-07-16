package task

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/git"
)

type mockForgeClient struct {
	pipelineStatusFn func(ctx context.Context, branch, repo string) ([]forge.PipelineStatus, error)
}

func (m *mockForgeClient) Provider() forge.ForgeProvider { return forge.ForgeProviderGitLab }
func (m *mockForgeClient) IsAvailable(_ context.Context) bool {
	return true
}
func (m *mockForgeClient) CreateMR(_ context.Context, _ forge.CreateMRParams) (forge.MRInfo, error) {
	return forge.MRInfo{}, nil
}
func (m *mockForgeClient) MRStatus(_ context.Context, _, _ string) ([]forge.MRInfo, error) {
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
		repoStatusFn:     func(string) (git.RawStatus, error) { return git.RawStatus{Branch: "feature/IN-FORGE-PIPELINE-ERR"}, nil },
		remoteURLRes:     "git@gitlab.com:",
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
