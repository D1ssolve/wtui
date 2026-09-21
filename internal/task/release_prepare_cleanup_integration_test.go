//go:build integration

package task

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
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
