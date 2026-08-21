//go:build integration

package task

import (
	"context"
	"errors"
	"fmt"
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

func TestIntegration_TwoStageRelease_FullCycle(t *testing.T) {
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

	if release.Status != domain.ReleaseStatusPrepared {
		t.Fatalf("release status = %q, want %q", release.Status, domain.ReleaseStatusPrepared)
	}
	if release.PreparedAt == nil {
		t.Fatalf("PreparedAt = nil, want non-nil")
	}
	if release.CompletedAt != nil {
		t.Fatalf("CompletedAt = %v, want nil after prepare", release.CompletedAt)
	}
	if gitOutput(t, env.repoPath, "tag", "-l", "v1.2.3") != "" {
		t.Fatalf("tag v1.2.3 already exists after prepare")
	}

	release = env.markReleaseMasterMerged(t, release)
	finished, err := env.manager.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if err != nil {
		t.Fatalf("FinalizeRelease() error: %v", err)
	}
	if finished.Status != domain.ReleaseStatusReleased {
		t.Fatalf("release status = %q, want %q", finished.Status, domain.ReleaseStatusReleased)
	}
	if finished.CompletedAt == nil {
		t.Fatalf("CompletedAt = nil, want non-nil after finish")
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
	if tagTarget != release.Services[0].AcceptedMergeSHA {
		t.Fatalf("tag target = %s, accepted merge = %s, want equal", tagTarget, release.Services[0].AcceptedMergeSHA)
	}
}

func TestCreateRelease_Integration_DevelopCheckedOutInMainWorktree(t *testing.T) {
	env := newReleaseIntegrationEnv(t)
	env.addFeatureTask(t, "APP-3", func(worktreePath string) {
		writeFile(t, filepath.Join(worktreePath, "feature-three.txt"), "feature three\n")
		mustGit(t, worktreePath, "add", "feature-three.txt")
		mustGit(t, worktreePath, "commit", "-m", "feat(APP-3): add feature three")
	})
	mustGit(t, env.repoPath, "checkout", "develop")

	release, err := env.manager.CreateRelease(context.Background(), CreateReleaseParams{
		TaskIDs:          []string{"APP-3"},
		ServiceVersions:  map[string]string{"svc-api": "1.2.3"},
		StartImmediately: true,
	})
	if err != nil {
		t.Fatalf("CreateRelease() with develop checked out: %v", err)
	}
	if release.Status != domain.ReleaseStatusPrepared {
		t.Fatalf("release status = %q, want %q", release.Status, domain.ReleaseStatusPrepared)
	}
}

func TestConvertHotfixToFeature_Integration_StagedTaskSwap(t *testing.T) {
	for _, targetID := range []string{"APP-1", "APP-2"} {
		t.Run(targetID, func(t *testing.T) {
			env := newReleaseIntegrationEnv(t)
			const sourceID = "APP-1"
			sourcePath := filepath.Join(env.tasksRoot, sourceID, "svc-api")
			if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
				t.Fatal(err)
			}
			mustGit(t, env.repoPath, "worktree", "add", "-b", "hotfix/"+sourceID, sourcePath, "master")
			writeFile(t, filepath.Join(sourcePath, "fix.txt"), "fix\n")
			mustGit(t, sourcePath, "add", "fix.txt")
			mustGit(t, sourcePath, "commit", "-m", "fix: staged conversion")
			mustGit(t, sourcePath, "push", "-u", "origin", "hotfix/"+sourceID)

			if err := env.manager.ConvertHotfixToFeature(context.Background(), ConvertHotfixParams{
				SourceTaskID: sourceID,
				TargetTaskID: targetID,
			}); err != nil {
				t.Fatalf("ConvertHotfixToFeature() error: %v", err)
			}

			targetPath := filepath.Join(env.tasksRoot, targetID, "svc-api")
			if got := gitOutput(t, targetPath, "branch", "--show-current"); got != "feature/"+targetID {
				t.Fatalf("target branch = %q", got)
			}
			if _, err := os.Stat(filepath.Join(targetPath, "fix.txt")); err != nil {
				t.Fatalf("converted file missing: %v", err)
			}
			if targetID != sourceID {
				if _, err := os.Stat(filepath.Join(env.tasksRoot, sourceID)); !os.IsNotExist(err) {
					t.Fatalf("source task still exists: %v", err)
				}
			}
			if got := gitOutput(t, env.repoPath, "ls-remote", "--heads", "origin", "hotfix/"+sourceID); got != "" {
				t.Fatalf("remote hotfix still exists: %q", got)
			}
			if got := gitOutput(t, env.repoPath, "ls-remote", "--heads", "origin", "feature/"+targetID); got == "" {
				t.Fatal("remote feature branch missing")
			}
		})
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
	manager   *manager
	repoPath  string
	tasksRoot string
}

func newReleaseIntegrationEnv(t *testing.T) releaseIntegrationEnv {
	t.Helper()
	return newReleaseIntegrationEnvWithGitClient(t, &integrationGitClient{Client: git.NewCommandClient(newIntegrationLogger())})
}

func newReleaseIntegrationEnvWithGitClient(t *testing.T, client git.Client) releaseIntegrationEnv {
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
	mustGit(t, repoPath, "checkout", "-b", "master")

	writeFile(t, filepath.Join(repoPath, "README.md"), "# svc-api\n")
	mustGit(t, repoPath, "add", "README.md")
	mustGit(t, repoPath, "commit", "-m", "chore: initial commit")
	mustGit(t, repoPath, "push", "-u", "origin", "master")
	mustGit(t, repoPath, "branch", "develop", "master")
	mustGit(t, repoPath, "push", "-u", "origin", "develop")

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
	disc := discovery.New(cfg, client, logger)
	slnMgr := sln.NewManager(dotnet.NewCommandClient(logger), logger)
	validator := validation.NewTaskValidator(client)
	flow := &gitflow.ResolvedGitFlow{
		DefaultBranchType: gitflow.BranchTypeFeature,
		ProductionBranch:  "master",
		IntegrationBranch: "develop",
		BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
			gitflow.BranchTypeFeature: {Prefixes: []string{"feature/"}},
			gitflow.BranchTypeRelease: {Prefixes: []string{"release/"}},
			gitflow.BranchTypeHotfix:  {Prefixes: []string{"hotfix/"}},
		},
	}

	mgr, ok := New(cfg, client, disc, slnMgr, validator, flow, nil, logger).(*manager)
	if !ok {
		t.Fatal("manager type assertion failed")
	}

	return releaseIntegrationEnv{
		manager:   mgr,
		repoPath:  repoPath,
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

	mustGit(t, e.repoPath, "checkout", "develop")
	mustGit(t, e.repoPath, "merge", "--no-ff", branch, "-m", "merge "+branch)
	mustGit(t, e.repoPath, "push", "origin", "develop")
	mustGit(t, e.repoPath, "checkout", "master")
}

func (e releaseIntegrationEnv) markReleaseMasterMerged(t *testing.T, release domain.Release) domain.Release {
	t.Helper()

	mustGit(t, e.repoPath, "checkout", "master")
	mustGit(t, e.repoPath, "merge", "--no-ff", release.Services[0].ReleaseBranch, "-m", "merge "+release.Services[0].ReleaseBranch)
	mustGit(t, e.repoPath, "push", "origin", "master")
	acceptedSHA := gitOutput(t, e.repoPath, "rev-parse", "master")

	release.Status = domain.ReleaseStatusMasterMerged
	release.Checkpoint = "master_merged"
	for i := range release.Services {
		release.Services[i].Status = domain.ReleaseStatusMasterMerged
		release.Services[i].AcceptedMergeSHA = acceptedSHA
	}
	var err error
	release, err = e.manager.writeReleaseManifest(release)
	if err != nil {
		t.Fatalf("writeReleaseManifest() error: %v", err)
	}
	return release
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

type failingPushTagClient struct {
	git.Client
	failCount int
	onFail    func(worktreePath, tag string) error
}

func (c *failingPushTagClient) PushTag(ctx context.Context, worktreePath, tag string) error {
	if c.failCount > 0 {
		c.failCount--
		if c.onFail != nil {
			if err := c.onFail(worktreePath, tag); err != nil {
				return err
			}
		}
		return fmt.Errorf("%w: simulated tag push failure", ErrReleaseTagPushFailed)
	}
	return c.Client.PushTag(ctx, worktreePath, tag)
}

type failingCreateBranchClient struct {
	git.Client
	failCount int
}

func (c *failingCreateBranchClient) CreateBranchFromBranch(ctx context.Context, repoPath, newBranch, fromBranch string) error {
	if c.failCount > 0 {
		c.failCount--
		return fmt.Errorf("simulated release branch create failure")
	}
	return c.Client.CreateBranchFromBranch(ctx, repoPath, newBranch, fromBranch)
}

type failingFetchClient struct {
	git.Client
	failCount int
}

func (c *failingFetchClient) Fetch(ctx context.Context, worktreePath string) error {
	if c.failCount > 0 {
		c.failCount--
		return fmt.Errorf("simulated fetch failure")
	}
	return c.Client.Fetch(ctx, worktreePath)
}

type failingCreateTagClient struct {
	git.Client
	failCount int
	onFail    func(repoPath, tag, target, message string) error
}

type failingCleanupBranchClient struct {
	git.Client
	failCount int
}

func (c *failingCleanupBranchClient) DeleteBranchIfUnchanged(ctx context.Context, repoPath, branch, expectedSHA string) error {
	if c.failCount > 0 && strings.HasPrefix(branch, "feature/") {
		c.failCount--
		return errors.New("simulated cleanup branch failure")
	}
	return c.Client.DeleteBranchIfUnchanged(ctx, repoPath, branch, expectedSHA)
}

func (c *failingCreateTagClient) CreateTag(ctx context.Context, repoPath, tag, target, message string) error {
	if c.failCount > 0 {
		c.failCount--
		if c.onFail != nil {
			if err := c.onFail(repoPath, tag, target, message); err != nil {
				return err
			}
		}
		return fmt.Errorf("%w: simulated tag create failure", ErrReleaseTagCreateFailed)
	}
	return c.Client.CreateTag(ctx, repoPath, tag, target, message)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writeFile(%q): %v", path, err)
	}
}

func TestIntegration_TwoStageRelease_FailAndRetryStage1(t *testing.T) {
	env := newReleaseIntegrationEnvWithGitClient(t, &failingCreateBranchClient{
		Client:    &integrationGitClient{Client: git.NewCommandClient(newIntegrationLogger())},
		failCount: 1,
	})

	env.addFeatureTask(t, "APP-30", func(worktreePath string) {
		writeFile(t, filepath.Join(worktreePath, "stage-one.txt"), "stage one retry case\n")
		mustGit(t, worktreePath, "add", "stage-one.txt")
		mustGit(t, worktreePath, "commit", "-m", "feat(APP-30): stage one retry case")
	})

	release, err := env.manager.CreateRelease(context.Background(), CreateReleaseParams{
		TaskIDs:          []string{"APP-30"},
		ServiceVersions:  map[string]string{"svc-api": "1.2.3"},
		StartImmediately: true,
	})
	if err == nil {
		t.Fatalf("CreateRelease() error=nil, want non-nil")
	}
	if release.Status != domain.ReleaseStatusFailed {
		t.Fatalf("release status = %q, want %q", release.Status, domain.ReleaseStatusFailed)
	}
	if release.PreparedAt != nil {
		t.Fatalf("PreparedAt = %v, want nil after stage-1 failure", release.PreparedAt)
	}

	retried, err := env.manager.RetryRelease(context.Background(), release.ID)
	if err != nil {
		t.Fatalf("RetryRelease() error: %v", err)
	}
	if retried.Status != domain.ReleaseStatusPrepared {
		t.Fatalf("release status = %q, want %q", retried.Status, domain.ReleaseStatusPrepared)
	}
	if retried.PreparedAt == nil {
		t.Fatalf("PreparedAt = nil, want non-nil after retry")
	}

	retried = env.markReleaseMasterMerged(t, retried)
	finished, err := env.manager.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: retried.ID})
	if err != nil {
		t.Fatalf("FinalizeRelease() error: %v", err)
	}
	if finished.Status != domain.ReleaseStatusReleased {
		t.Fatalf("release status = %q, want %q", finished.Status, domain.ReleaseStatusReleased)
	}

	if typ := gitOutput(t, env.repoPath, "cat-file", "-t", "v1.2.3"); typ != "tag" {
		t.Fatalf("tag object type = %q, want tag (annotated)", typ)
	}
	tagTarget := gitOutput(t, env.repoPath, "rev-parse", "v1.2.3^{}")
	if tagTarget != retried.Services[0].AcceptedMergeSHA {
		t.Fatalf("tag target = %s, accepted merge = %s, want equal", tagTarget, retried.Services[0].AcceptedMergeSHA)
	}
}

func TestIntegration_FinalizeRelease_FetchFailure_PersistsFailedManifest(t *testing.T) {
	env := newReleaseIntegrationEnv(t)

	env.addFeatureTask(t, "APP-50", func(worktreePath string) {
		writeFile(t, filepath.Join(worktreePath, "fetch-fail.txt"), "fetch failure case\n")
		mustGit(t, worktreePath, "add", "fetch-fail.txt")
		mustGit(t, worktreePath, "commit", "-m", "feat(APP-50): fetch failure case")
	})

	release, err := env.manager.CreateRelease(context.Background(), CreateReleaseParams{
		TaskIDs:          []string{"APP-50"},
		ServiceVersions:  map[string]string{"svc-api": "1.2.3"},
		StartImmediately: true,
	})
	if err != nil {
		t.Fatalf("CreateRelease() error: %v", err)
	}
	if release.Status != domain.ReleaseStatusPrepared {
		t.Fatalf("release status = %q, want %q", release.Status, domain.ReleaseStatusPrepared)
	}
	release = env.markReleaseMasterMerged(t, release)

	finishMgr := &manager{
		cfg:          env.manager.cfg,
		git:          &failingFetchClient{Client: env.manager.git, failCount: 1},
		discoverer:   env.manager.discoverer,
		slnMgr:       env.manager.slnMgr,
		validator:    env.manager.validator,
		flow:         env.manager.flow,
		forgeClients: env.manager.forgeClients,
		logger:       env.manager.logger,
	}

	result, err := finishMgr.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if err == nil {
		t.Fatalf("FinalizeRelease() error=nil, want non-nil")
	}
	if result.ID != release.ID {
		t.Fatalf("result.ID = %q, want %q", result.ID, release.ID)
	}
	if result.Status != domain.ReleaseStatusFailed {
		t.Fatalf("release status = %q, want %q", result.Status, domain.ReleaseStatusFailed)
	}
	if result.Error == nil {
		t.Fatalf("release error=nil, want non-nil")
	}
	if !result.Error.Recoverable {
		t.Fatalf("release error Recoverable = %v, want true", result.Error.Recoverable)
	}

	loaded, loadErr := finishMgr.loadReleaseManifest(release.ID)
	if loadErr != nil {
		t.Fatalf("loadReleaseManifest(%q) error: %v", release.ID, loadErr)
	}
	if loaded.Status != domain.ReleaseStatusFailed {
		t.Fatalf("loaded release status = %q, want %q", loaded.Status, domain.ReleaseStatusFailed)
	}
	if loaded.Error == nil {
		t.Fatalf("loaded release error=nil, want non-nil")
	}
	if !loaded.Error.Recoverable {
		t.Fatalf("loaded release error Recoverable = %v, want true", loaded.Error.Recoverable)
	}
}

func TestIntegration_FinalizeRelease_TagCreateFailure_ReachedTaggingCheckpoint(t *testing.T) {
	env := newReleaseIntegrationEnv(t)

	env.addFeatureTask(t, "APP-51", func(worktreePath string) {
		writeFile(t, filepath.Join(worktreePath, "tag-create-fail.txt"), "tag create failure case\n")
		mustGit(t, worktreePath, "add", "tag-create-fail.txt")
		mustGit(t, worktreePath, "commit", "-m", "feat(APP-51): tag create failure case")
	})

	release, err := env.manager.CreateRelease(context.Background(), CreateReleaseParams{
		TaskIDs:          []string{"APP-51"},
		ServiceVersions:  map[string]string{"svc-api": "1.2.3"},
		StartImmediately: true,
	})
	if err != nil {
		t.Fatalf("CreateRelease() error: %v", err)
	}
	if release.Status != domain.ReleaseStatusPrepared {
		t.Fatalf("release status = %q, want %q", release.Status, domain.ReleaseStatusPrepared)
	}
	release = env.markReleaseMasterMerged(t, release)

	failingGit := &failingCreateTagClient{
		Client:    &integrationGitClient{Client: git.NewCommandClient(newIntegrationLogger())},
		failCount: 1,
	}
	finishMgr := &manager{
		cfg:          env.manager.cfg,
		git:          failingGit,
		discoverer:   env.manager.discoverer,
		slnMgr:       env.manager.slnMgr,
		validator:    env.manager.validator,
		flow:         env.manager.flow,
		forgeClients: env.manager.forgeClients,
		logger:       env.manager.logger,
	}
	failingGit.onFail = func(repoPath, tag, target, message string) error {
		loaded, loadErr := finishMgr.loadReleaseManifest(release.ID)
		if loadErr != nil {
			return fmt.Errorf("load manifest during CreateTag: %w", loadErr)
		}
		if loaded.Status != domain.ReleaseStatusTagging {
			return fmt.Errorf("manifest status during CreateTag = %q, want %q", loaded.Status, domain.ReleaseStatusTagging)
		}
		if loaded.Checkpoint != "tagging" {
			return fmt.Errorf("manifest checkpoint during CreateTag = %q, want %q", loaded.Checkpoint, "tagging")
		}
		return nil
	}

	result, err := finishMgr.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if err == nil {
		t.Fatalf("FinalizeRelease() error=nil, want non-nil")
	}
	if !errors.Is(err, ErrReleaseTagCreateFailed) {
		t.Fatalf("FinalizeRelease() error = %v, want ErrReleaseTagCreateFailed", err)
	}
	if result.Status != domain.ReleaseStatusFailed {
		t.Fatalf("release status = %q, want %q", result.Status, domain.ReleaseStatusFailed)
	}
	if result.Error == nil || result.Error.Stage != "tag" {
		t.Fatalf("release error stage = %q, want tag", result.Error.Stage)
	}
}

func TestIntegration_FinalizeRelease_TagPushFailure_ReachedPushingCheckpoint(t *testing.T) {
	env := newReleaseIntegrationEnv(t)

	env.addFeatureTask(t, "APP-52", func(worktreePath string) {
		writeFile(t, filepath.Join(worktreePath, "tag-push-fail.txt"), "tag push failure case\n")
		mustGit(t, worktreePath, "add", "tag-push-fail.txt")
		mustGit(t, worktreePath, "commit", "-m", "feat(APP-52): tag push failure case")
	})

	release, err := env.manager.CreateRelease(context.Background(), CreateReleaseParams{
		TaskIDs:          []string{"APP-52"},
		ServiceVersions:  map[string]string{"svc-api": "1.2.3"},
		StartImmediately: true,
	})
	if err != nil {
		t.Fatalf("CreateRelease() error: %v", err)
	}
	if release.Status != domain.ReleaseStatusPrepared {
		t.Fatalf("release status = %q, want %q", release.Status, domain.ReleaseStatusPrepared)
	}
	release = env.markReleaseMasterMerged(t, release)

	failingGit := &failingPushTagClient{
		Client:    &integrationGitClient{Client: git.NewCommandClient(newIntegrationLogger())},
		failCount: 1,
	}
	finishMgr := &manager{
		cfg:          env.manager.cfg,
		git:          failingGit,
		discoverer:   env.manager.discoverer,
		slnMgr:       env.manager.slnMgr,
		validator:    env.manager.validator,
		flow:         env.manager.flow,
		forgeClients: env.manager.forgeClients,
		logger:       env.manager.logger,
	}
	failingGit.onFail = func(worktreePath, tag string) error {
		loaded, loadErr := finishMgr.loadReleaseManifest(release.ID)
		if loadErr != nil {
			return fmt.Errorf("load manifest during PushTag: %w", loadErr)
		}
		if loaded.Status != domain.ReleaseStatusPushing {
			return fmt.Errorf("manifest status during PushTag = %q, want %q", loaded.Status, domain.ReleaseStatusPushing)
		}
		if loaded.Checkpoint != "pushing" {
			return fmt.Errorf("manifest checkpoint during PushTag = %q, want %q", loaded.Checkpoint, "pushing")
		}
		return nil
	}

	result, err := finishMgr.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if err == nil {
		t.Fatalf("FinalizeRelease() error=nil, want non-nil")
	}
	if !errors.Is(err, ErrReleaseTagPushFailed) {
		t.Fatalf("FinalizeRelease() error = %v, want ErrReleaseTagPushFailed", err)
	}
	if result.Status != domain.ReleaseStatusFailed {
		t.Fatalf("release status = %q, want %q", result.Status, domain.ReleaseStatusFailed)
	}
	if result.Error == nil || result.Error.Stage != "push_tag" {
		t.Fatalf("release error stage = %q, want push_tag", result.Error.Stage)
	}
}

func TestReleaseCleanup_Integration_DevelopCheckedOutAndRetryAfterPartialFailure(t *testing.T) {
	env := newReleaseIntegrationEnv(t)
	env.addFeatureTask(t, "APP-60", func(worktreePath string) {
		writeFile(t, filepath.Join(worktreePath, "cleanup.txt"), "cleanup\n")
		mustGit(t, worktreePath, "add", "cleanup.txt")
		mustGit(t, worktreePath, "commit", "-m", "feat: cleanup")
	})
	release, err := env.manager.CreateRelease(context.Background(), CreateReleaseParams{TaskIDs: []string{"APP-60"}, ServiceVersions: map[string]string{"svc-api": "1.2.3"}, StartImmediately: true})
	if err != nil {
		t.Fatal(err)
	}
	release = env.markReleaseMasterMerged(t, release)
	if _, err = env.manager.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID}); err != nil {
		t.Fatal(err)
	}
	mustGit(t, env.repoPath, "checkout", "develop")

	failing := &failingCleanupBranchClient{Client: env.manager.git, failCount: 1}
	mgr := *env.manager
	mgr.git = failing
	plan, err := mgr.PlanReleaseCleanup(context.Background(), release.ID, DefaultReleaseCleanupSelection())
	if err != nil || len(plan.Preview().Blockers) != 0 {
		t.Fatalf("plan = %+v, %v", plan.Preview(), err)
	}
	if _, err = mgr.ExecuteReleaseCleanup(context.Background(), plan, nil); err == nil {
		t.Fatal("expected injected failure")
	}
	if _, err := os.Stat(filepath.Join(env.tasksRoot, "APP-60")); !os.IsNotExist(err) {
		t.Fatalf("task directory remains: %v", err)
	}
	if _, err := os.Stat(mgr.releaseManifestPath(release.ID)); err != nil {
		t.Fatalf("manifest removed after failure: %v", err)
	}

	retryPlan, err := mgr.PlanReleaseCleanup(context.Background(), release.ID, DefaultReleaseCleanupSelection())
	if err != nil || len(retryPlan.Preview().Blockers) != 0 {
		t.Fatalf("retry plan = %+v, %v", retryPlan.Preview(), err)
	}
	if _, err = mgr.ExecuteReleaseCleanup(context.Background(), retryPlan, nil); err != nil {
		t.Fatal(err)
	}
	if branch := gitOutput(t, env.repoPath, "branch", "--show-current"); branch != "develop" {
		t.Fatalf("current branch = %q", branch)
	}
	if _, err := os.Stat(mgr.releaseManifestPath(release.ID)); !os.IsNotExist(err) {
		t.Fatalf("release manifest remains: %v", err)
	}
}
