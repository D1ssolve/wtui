//go:build integration

package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
)

func TestPrepareAccepted_Integration_DefaultCleanup(t *testing.T) {
	// Given: a real repository with an accepted integration commit and default cleanup.
	env := newReleaseIntegrationEnv(t)
	enableReleasePrepareTaskMerge(t, env.manager)
	if *env.manager.cfg.Release.KeepIntegrationWorktrees {
		t.Fatal("expected default keep_integration_worktrees=false")
	}
	accepted := gitOutput(t, env.repoPath, "rev-parse", "origin/develop")
	release := domain.Release{ID: "rel-cleanup", Dir: t.TempDir(), Services: []domain.ReleaseService{{
		Name: "svc-api", RepoPath: env.repoPath, IntegrationBranch: "develop",
		ReleaseBranch: "release/1.2.3", PostIntegrationSHA: accepted,
	}}}
	// When
	err := env.manager.executePrepareService(t.Context(), &release, &release.Services[0], nil)
	// Then
	if err != nil {
		t.Fatal(err)
	}
	svc := release.Services[0]
	if svc.Status != domain.ReleaseStatusPrepared || svc.IntegrationWorktreePath != "" || svc.PostIntegrationSHA != accepted || svc.ReleaseSHA != accepted {
		t.Fatalf("prepared service = %+v", svc)
	}
	path := filepath.Join(release.Dir, ".work", "svc-api-integration")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("integration worktree remains: %v", err)
	}
	entries, err := env.manager.git.ListWorktrees(t.Context(), env.repoPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if samePath(entry.Path, path) {
			t.Fatal("integration worktree remains registered")
		}
	}
}

func TestPlanReleaseTaskMerges_Integration_RejectsRepositoryAliases(t *testing.T) {
	// Given: two named services that are linked worktrees of one repository.
	env := newReleaseIntegrationEnv(t)
	env.addFeatureTask(t, "APP-1", nil)
	aliasPath := filepath.Join(env.tasksRoot, "APP-2", "svc-alias")
	if err := os.MkdirAll(filepath.Dir(aliasPath), 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, env.repoPath, "worktree", "add", "-b", "feature/APP-2", aliasPath, "develop")
	enableReleasePrepareTaskMerge(t, env.manager)
	before := gitOutput(t, env.repoPath, "ls-remote", "origin")
	// When
	_, err := env.manager.PlanReleaseTaskMerges(t.Context(), CreateReleaseParams{
		TaskIDs: []string{"APP-1", "APP-2"}, ServiceVersions: map[string]string{"svc-api": "1.2.3", "svc-alias": "2.3.4"},
	})
	// Then
	if !errors.Is(err, ErrReleaseServiceRepoConflict) {
		t.Fatalf("preview error = %v, want repository conflict", err)
	}
	if got := gitOutput(t, env.repoPath, "ls-remote", "origin"); got != before {
		t.Fatal("preview mutated remote refs")
	}
	if got := gitOutput(t, env.repoPath, "branch", "--list", "release/*"); got != "" {
		t.Fatalf("preview created release branches: %s", got)
	}
}

func prepareReleaseForCleanupTest(t *testing.T, env releaseIntegrationEnv, taskID string) domain.Release {
	t.Helper()
	env.addFeatureTask(t, taskID, func(worktreePath string) {
		writeFile(t, filepath.Join(worktreePath, "cleanup-"+taskID+".txt"), "cleanup case\n")
		mustGit(t, worktreePath, "add", "cleanup-"+taskID+".txt")
		mustGit(t, worktreePath, "commit", "-m", "feat("+taskID+"): cleanup case")
	})

	release, err := env.manager.CreateRelease(context.Background(), CreateReleaseParams{
		TaskIDs:          []string{taskID},
		ServiceVersions:  map[string]string{"svc-api": "1.2.3"},
		StartImmediately: true,
	})
	if err != nil {
		t.Fatalf("CreateRelease() error: %v", err)
	}
	if release.Status != domain.ReleaseStatusPrepared {
		t.Fatalf("release status = %q, want %q", release.Status, domain.ReleaseStatusPrepared)
	}
	return release
}

func TestIntegration_PrepareCleanup_DirtyWorktreeFailsClosedAndRetains(t *testing.T) {
	env := newReleaseIntegrationEnv(t)
	release := prepareReleaseForCleanupTest(t, env, "APP-41")

	workPath := filepath.Join(release.Dir, ".work", "svc-api-integration")
	mustGit(t, env.repoPath, "worktree", "add", "--detach", workPath, "origin/develop")
	writeFile(t, filepath.Join(workPath, "local-notes.txt"), "do not destroy\n")

	svc := release.Services[0]
	err := env.manager.removeOwnedIntegrationWorktree(context.Background(), &release, &svc, workPath, "")
	if err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("removeOwnedIntegrationWorktree() error = %v, want dirty block", err)
	}
	if _, statErr := os.Stat(filepath.Join(workPath, "local-notes.txt")); statErr != nil {
		t.Fatalf("dirty worktree content lost: %v", statErr)
	}
}

func TestIntegration_PrepareCleanup_UnregisteredReplacementPathFailsClosed(t *testing.T) {
	env := newReleaseIntegrationEnv(t)
	release := prepareReleaseForCleanupTest(t, env, "APP-42")

	workPath := filepath.Join(release.Dir, ".work", "svc-api-integration")
	if err := os.MkdirAll(workPath, 0o755); err != nil {
		t.Fatalf("mkdir replacement path: %v", err)
	}
	writeFile(t, filepath.Join(workPath, "replacement.txt"), "replacement content\n")

	svc := release.Services[0]
	err := env.manager.removeOwnedIntegrationWorktree(context.Background(), &release, &svc, workPath, "")
	if err == nil || !strings.Contains(err.Error(), "exactly one registered worktree") {
		t.Fatalf("removeOwnedIntegrationWorktree() error = %v, want registration block", err)
	}
	if _, statErr := os.Stat(filepath.Join(workPath, "replacement.txt")); statErr != nil {
		t.Fatalf("replacement content lost: %v", statErr)
	}
}

func TestIntegration_PrepareCleanup_TamperedPathOutsideWorkFailsClosed(t *testing.T) {
	env := newReleaseIntegrationEnv(t)
	release := prepareReleaseForCleanupTest(t, env, "APP-43")

	tampered := filepath.Join(t.TempDir(), "svc-api-integration")
	mustGit(t, env.repoPath, "worktree", "add", "--detach", tampered, "origin/develop")

	svc := release.Services[0]
	err := env.manager.removeOwnedIntegrationWorktree(context.Background(), &release, &svc, tampered, "")
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("removeOwnedIntegrationWorktree() error = %v, want outside-work block", err)
	}
	if listed := gitOutput(t, env.repoPath, "worktree", "list", "--porcelain"); !strings.Contains(listed, tampered) {
		t.Fatalf("tampered worktree was removed:\n%s", listed)
	}
}

func TestIntegration_PrepareCleanup_RetryReusesAndTruthfullyCleansWorktree(t *testing.T) {
	env := newReleaseIntegrationEnvWithGitClient(t, &failingCleanupWorktreeClient{
		Client: &failingCreateBranchClient{
			Client:    &integrationGitClient{Client: git.NewCommandClient(newIntegrationLogger())},
			failCount: 1,
		},
		failCount: 1,
	})

	env.addFeatureTask(t, "APP-44", func(worktreePath string) {
		writeFile(t, filepath.Join(worktreePath, "retry-cleanup.txt"), "retry cleanup case\n")
		mustGit(t, worktreePath, "add", "retry-cleanup.txt")
		mustGit(t, worktreePath, "commit", "-m", "feat(APP-44): retry cleanup case")
	})

	release, err := env.manager.CreateRelease(context.Background(), CreateReleaseParams{
		TaskIDs:          []string{"APP-44"},
		ServiceVersions:  map[string]string{"svc-api": "1.2.3"},
		StartImmediately: true,
	})
	if err == nil {
		t.Fatal("CreateRelease() error = nil, want branch-create failure")
	}
	if release.Status != domain.ReleaseStatusFailed {
		t.Fatalf("release status = %q, want %q", release.Status, domain.ReleaseStatusFailed)
	}
	failed, err := env.manager.GetRelease(context.Background(), release.ID)
	if err != nil {
		t.Fatalf("GetRelease() error: %v", err)
	}
	workPath := failed.Services[0].IntegrationWorktreePath
	if workPath == "" {
		t.Fatal("IntegrationWorktreePath empty after failed prepare, want retained for retry")
	}

	retried, err := env.manager.RetryRelease(context.Background(), release.ID)
	if err != nil {
		t.Fatalf("RetryRelease() error: %v", err)
	}
	if retried.Status != domain.ReleaseStatusPrepared {
		t.Fatalf("release status = %q, want %q", retried.Status, domain.ReleaseStatusPrepared)
	}
	if retried.Services[0].IntegrationWorktreePath != "" {
		t.Fatalf("IntegrationWorktreePath = %q after retry, want cleaned", retried.Services[0].IntegrationWorktreePath)
	}
	if _, statErr := os.Stat(workPath); !os.IsNotExist(statErr) {
		t.Fatalf("integration worktree still present after retry: %v", statErr)
	}

	persisted, err := env.manager.GetRelease(context.Background(), release.ID)
	if err != nil {
		t.Fatalf("GetRelease() after retry error: %v", err)
	}
	if persisted.Checkpoint != "prepared" {
		t.Fatalf("checkpoint = %q, want prepared", persisted.Checkpoint)
	}
	if persisted.Error != nil {
		t.Fatalf("Error = %#v, want nil after successful retry", persisted.Error)
	}
}

func TestIntegration_PrepareCleanup_RetryDirtyWorktreeFailsAndKeepsCheckpointTruthful(t *testing.T) {
	env := newReleaseIntegrationEnvWithGitClient(t, &failingCleanupWorktreeClient{
		Client: &failingCreateBranchClient{
			Client:    &integrationGitClient{Client: git.NewCommandClient(newIntegrationLogger())},
			failCount: 1,
		},
		failCount: 1,
	})

	env.addFeatureTask(t, "APP-45", func(worktreePath string) {
		writeFile(t, filepath.Join(worktreePath, "retry-dirty.txt"), "retry dirty case\n")
		mustGit(t, worktreePath, "add", "retry-dirty.txt")
		mustGit(t, worktreePath, "commit", "-m", "feat(APP-45): retry dirty case")
	})

	release, err := env.manager.CreateRelease(context.Background(), CreateReleaseParams{
		TaskIDs:          []string{"APP-45"},
		ServiceVersions:  map[string]string{"svc-api": "1.2.3"},
		StartImmediately: true,
	})
	if err == nil {
		t.Fatal("CreateRelease() error = nil, want branch-create failure")
	}

	failed, err := env.manager.GetRelease(context.Background(), release.ID)
	if err != nil {
		t.Fatalf("GetRelease() error: %v", err)
	}
	workPath := failed.Services[0].IntegrationWorktreePath
	if workPath == "" {
		t.Fatal("IntegrationWorktreePath empty after failed prepare")
	}
	writeFile(t, filepath.Join(workPath, "local-dirty.txt"), "keep me\n")

	retried, err := env.manager.RetryRelease(context.Background(), release.ID)
	if err == nil {
		t.Fatal("RetryRelease() error = nil, want dirty-worktree cleanup block")
	}
	if retried.Status != domain.ReleaseStatusFailed {
		t.Fatalf("release status = %q, want %q", retried.Status, domain.ReleaseStatusFailed)
	}
	if _, statErr := os.Stat(filepath.Join(workPath, "local-dirty.txt")); statErr != nil {
		t.Fatalf("dirty content lost after blocked retry: %v", statErr)
	}

	persisted, err := env.manager.GetRelease(context.Background(), release.ID)
	if err != nil {
		t.Fatalf("GetRelease() after blocked retry error: %v", err)
	}
	if persisted.Services[0].IntegrationWorktreePath != workPath {
		t.Fatalf("IntegrationWorktreePath = %q, want retained %q", persisted.Services[0].IntegrationWorktreePath, workPath)
	}
	if persisted.Checkpoint == "cleanup_prepare" {
		t.Fatal("checkpoint = cleanup_prepare despite failed cleanup, want truthful earlier checkpoint")
	}
	if persisted.Error == nil {
		t.Fatal("Error = nil, want cleanup failure recorded")
	}
}
