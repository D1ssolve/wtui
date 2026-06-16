//go:build integration

package task

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/discovery"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/dotnet"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/sln"
	"github.com/D1ssolve/wtui/internal/validation"
)

func TestCreateRelease_Integration_HappyPath(t *testing.T) {
	env := newReleaseIntegrationEnv(t)

	env.addFeatureTask(t, "APP-1", func(worktreePath string) {
		writeFile(t, filepath.Join(worktreePath, "feature-one.txt"), "feature one\n")
		mustGit(t, worktreePath, "add", "feature-one.txt")
		mustGit(t, worktreePath, "commit", "-m", "feat(APP-1): add feature one")
	})
	env.addFeatureTask(t, "APP-2", func(worktreePath string) {
		writeFile(t, filepath.Join(worktreePath, "feature-two.txt"), "feature two\n")
		mustGit(t, worktreePath, "add", "feature-two.txt")
		mustGit(t, worktreePath, "commit", "-m", "feat(APP-2): add feature two")
	})

	release, err := env.manager.CreateRelease(context.Background(), CreateReleaseParams{
		TaskIDs:          []string{"APP-1", "APP-2"},
		ServiceVersions:  map[string]string{"svc-api": "1.2.3"},
		StartImmediately: true,
	})
	if err != nil {
		t.Fatalf("CreateRelease() error: %v", err)
	}

	if release.Status != "released" {
		t.Fatalf("release status = %q, want released", release.Status)
	}

	for _, branch := range []string{"feature/APP-1", "feature/APP-2"} {
		ok, err := gitIsAncestor(env.repoPath, branch, "develop")
		if err != nil {
			t.Fatalf("gitIsAncestor(%s -> develop) error: %v", branch, err)
		}
		if !ok {
			t.Fatalf("expected branch %q commits to be in develop", branch)
		}
	}

	developTip := gitOutput(t, env.repoPath, "rev-parse", "develop")
	releaseTip := gitOutput(t, env.repoPath, "rev-parse", "release/1.2.3")
	if developTip != releaseTip {
		t.Fatalf("release branch tip = %s, develop tip = %s, want equal", releaseTip, developTip)
	}

	if typ := gitOutput(t, env.repoPath, "cat-file", "-t", "v1.2.3"); typ != "tag" {
		t.Fatalf("tag object type = %q, want tag (annotated)", typ)
	}
	tagTarget := gitOutput(t, env.repoPath, "rev-parse", "v1.2.3^{}")
	if tagTarget != releaseTip {
		t.Fatalf("tag target = %s, release tip = %s, want equal", tagTarget, releaseTip)
	}
}

func TestCreateRelease_Integration_MergeConflict_FailsAndPreservesIntegrationWorktree(t *testing.T) {
	env := newReleaseIntegrationEnv(t)

	baseConflictPath := filepath.Join(env.repoPath, "conflict.txt")
	writeFile(t, baseConflictPath, "base\n")
	mustGit(t, env.repoPath, "add", "conflict.txt")
	mustGit(t, env.repoPath, "commit", "-m", "chore: add conflict base")
	mustGit(t, env.repoPath, "push", "origin", "develop")

	env.addFeatureTask(t, "APP-10", func(worktreePath string) {
		writeFile(t, filepath.Join(worktreePath, "conflict.txt"), "from app-10\n")
		mustGit(t, worktreePath, "add", "conflict.txt")
		mustGit(t, worktreePath, "commit", "-m", "feat(APP-10): conflict change")
	})
	env.addFeatureTask(t, "APP-11", func(worktreePath string) {
		writeFile(t, filepath.Join(worktreePath, "conflict.txt"), "from app-11\n")
		mustGit(t, worktreePath, "add", "conflict.txt")
		mustGit(t, worktreePath, "commit", "-m", "feat(APP-11): conflicting change")
	})

	release, err := env.manager.CreateRelease(context.Background(), CreateReleaseParams{
		TaskIDs:          []string{"APP-10", "APP-11"},
		ServiceVersions:  map[string]string{"svc-api": "2.0.0"},
		StartImmediately: true,
	})
	if !errors.Is(err, ErrReleaseMergeConflict) {
		t.Fatalf("CreateRelease() error = %v, want ErrReleaseMergeConflict", err)
	}

	persisted, loadErr := env.manager.loadReleaseManifest(release.ID)
	if loadErr != nil {
		t.Fatalf("loadReleaseManifest(%q) error: %v", release.ID, loadErr)
	}
	if persisted.Status != "failed" {
		t.Fatalf("persisted status = %q, want failed", persisted.Status)
	}
	if persisted.Error == nil || persisted.Error.Code != "ERR_RELEASE_MERGE_CONFLICT" {
		t.Fatalf("persisted error = %#v, want ERR_RELEASE_MERGE_CONFLICT", persisted.Error)
	}

	if len(persisted.Services) != 1 {
		t.Fatalf("len(services) = %d, want 1", len(persisted.Services))
	}
	integrationPath := persisted.Services[0].IntegrationWorktreePath
	if strings.TrimSpace(integrationPath) == "" {
		t.Fatalf("integration worktree path empty, want preserved path")
	}
	if _, statErr := os.Stat(integrationPath); statErr != nil {
		t.Fatalf("integration worktree missing: %v", statErr)
	}

	if mergeHeadExists(integrationPath) {
		t.Fatalf("MERGE_HEAD still exists in preserved integration worktree, want merge aborted")
	}
}

func TestCreateRelease_Integration_ExistingBranchAndTag(t *testing.T) {
	t.Run("existing release branch", func(t *testing.T) {
		env := newReleaseIntegrationEnv(t)
		env.addFeatureTask(t, "APP-20", func(worktreePath string) {
			writeFile(t, filepath.Join(worktreePath, "branch-case.txt"), "branch exists case\n")
			mustGit(t, worktreePath, "add", "branch-case.txt")
			mustGit(t, worktreePath, "commit", "-m", "feat(APP-20): branch exists case")
		})

		mustGit(t, env.repoPath, "branch", "release/1.2.3", "develop")

		_, err := env.manager.CreateRelease(context.Background(), CreateReleaseParams{
			TaskIDs:          []string{"APP-20"},
			ServiceVersions:  map[string]string{"svc-api": "1.2.3"},
			StartImmediately: true,
		})
		if !errors.Is(err, ErrReleaseBranchExists) {
			t.Fatalf("CreateRelease() error = %v, want ErrReleaseBranchExists", err)
		}
})

	t.Run("existing tag", func(t *testing.T) {
		env := newReleaseIntegrationEnv(t)
		env.addFeatureTask(t, "APP-21", func(worktreePath string) {
			writeFile(t, filepath.Join(worktreePath, "tag-case.txt"), "tag exists case\n")
			mustGit(t, worktreePath, "add", "tag-case.txt")
			mustGit(t, worktreePath, "commit", "-m", "feat(APP-21): tag exists case")
		})

		mustGit(t, env.repoPath, "tag", "-a", "v1.2.3", "develop", "-m", "existing tag")

		_, err := env.manager.CreateRelease(context.Background(), CreateReleaseParams{
			TaskIDs:          []string{"APP-21"},
			ServiceVersions:  map[string]string{"svc-api": "1.2.3"},
			StartImmediately: true,
		})
		if !errors.Is(err, ErrReleaseTagExists) {
			t.Fatalf("CreateRelease() error = %v, want ErrReleaseTagExists", err)
		}
	})
}

type releaseIntegrationEnv struct {
	manager  *manager
	repoPath string
	tasksRoot string
}

func newReleaseIntegrationEnv(t *testing.T) releaseIntegrationEnv {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH, skipping integration tests")
	}

	rootDir := resolvedPath(t, t.TempDir())
	tasksRoot := filepath.Join(rootDir, ".tasks")
	remotePath := filepath.Join(rootDir, "origin.git")
	repoPath := filepath.Join(rootDir, "svc-api")

	mustGit(t, rootDir, "init", "--bare", remotePath)
	mustGit(t, rootDir, "clone", remotePath, repoPath)
	mustGit(t, repoPath, "config", "user.email", "integration@example.com")
	mustGit(t, repoPath, "config", "user.name", "Integration Test")
	mustGit(t, repoPath, "checkout", "-b", "main")

	writeFile(t, filepath.Join(repoPath, "README.md"), "# svc-api\n")
	mustGit(t, repoPath, "add", "README.md")
	mustGit(t, repoPath, "commit", "-m", "chore: initial commit")
	mustGit(t, repoPath, "push", "-u", "origin", "main")
	mustGit(t, repoPath, "branch", "develop", "main")
	mustGit(t, repoPath, "push", "-u", "origin", "develop")

	gitClient := &integrationGitClient{Client: git.NewCommandClient(newIntegrationLogger())}
	cfg := &config.Config{
		TasksRoot:    tasksRoot,
		RootDir:      rootDir,
		BranchPrefix: "feature/",
		BaseBranch:   "develop",
		Editor:       "code",
	}
	if _, err := cfg.Effective(); err != nil {
		t.Fatalf("cfg.Effective(): %v", err)
	}

	logger := newIntegrationLogger()
	disc := discovery.New(cfg, gitClient, logger)
	slnMgr := sln.NewManager(dotnet.NewCommandClient(logger), logger)
	validator := validation.NewTaskValidator(gitClient)
	flow := &gitflow.ResolvedGitFlow{
		DefaultBranchType: gitflow.BranchTypeFeature,
		IntegrationBranch: "develop",
		BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
			gitflow.BranchTypeFeature: {Prefixes: []string{"feature/"}},
			gitflow.BranchTypeRelease: {Prefixes: []string{"release/"}},
		},
	}

	mgr, ok := New(cfg, gitClient, disc, slnMgr, validator, flow, nil, logger).(*manager)
	if !ok {
		t.Fatal("manager type assertion failed")
	}

	return releaseIntegrationEnv{
		manager:  mgr,
		repoPath: repoPath,
		tasksRoot: tasksRoot,
	}
}

func resolvedPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}

func (e releaseIntegrationEnv) addFeatureTask(t *testing.T, taskID string, mutate func(worktreePath string)) {
	t.Helper()

	branch := "feature/" + taskID
	worktreePath := filepath.Join(e.tasksRoot, taskID, "svc-api")
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		t.Fatalf("mkdir task dir: %v", err)
	}

	mustGit(t, e.repoPath, "worktree", "add", "-b", branch, worktreePath, "develop")

	if mutate != nil {
		mutate(worktreePath)
	}
}

func newIntegrationLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func gitIsAncestor(dir, ancestor, descendant string) (bool, error) {
	cmd := exec.Command("git", "merge-base", "--is-ancestor", ancestor, descendant)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func mergeHeadExists(worktreePath string) bool {
	cmd := exec.Command("git", "-C", worktreePath, "rev-parse", "--verify", "MERGE_HEAD")
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

type integrationGitClient struct {
	git.Client
}

func (c *integrationGitClient) OperationState(ctx context.Context, worktreePath string) ([]domain.RepoState, error) {
	states, err := c.Client.OperationState(ctx, worktreePath)
	if err != nil {
		return nil, err
	}
	if containsRepoState(states, domain.RepoStateMerging) || containsRepoState(states, domain.RepoStateConflicted) {
		return states, nil
	}
	if mergeHeadExists(worktreePath) {
		return append(states, domain.RepoStateMerging), nil
	}
	return states, nil
}

func containsRepoState(states []domain.RepoState, target domain.RepoState) bool {
	for _, state := range states {
		if state == target {
			return true
		}
	}
	return false
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writeFile(%q): %v", path, err)
	}
}
