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

func TestIntegration_ReleaseDeletion_PreservesTasksAndHistory(t *testing.T) {
	for _, stage := range []string{"draft", "prepared", "released"} {
		t.Run(stage, func(t *testing.T) {
			env := newReleaseIntegrationEnv(t)
			env.addFeatureTask(t, "APP-1", nil)
			taskPath := filepath.Join(env.tasksRoot, "APP-1", "svc-api")
			mustGit(t, env.repoPath, "push", "origin", "feature/APP-1")
			release, err := env.manager.CreateRelease(t.Context(), CreateReleaseParams{
				TaskIDs: []string{"APP-1"}, ServiceVersions: map[string]string{"svc-api": "1.2.3"},
				StartImmediately: stage != "draft",
			})
			if err != nil {
				t.Fatal(err)
			}
			if stage == "released" {
				release = env.markReleaseMasterMerged(t, release)
				release, err = env.manager.FinalizeRelease(t.Context(), FinishReleaseParams{ReleaseID: release.ID})
				if err != nil {
					t.Fatal(err)
				}
			}
			if stage == "prepared" {
				// Committed release fixes are included in explicit deletion.
				mustGit(t, release.Services[0].ReleaseWorktreePath, "commit", "--allow-empty", "-m", "release fix")
				mustGit(t, release.Services[0].ReleaseWorktreePath, "push", "origin", release.Services[0].ReleaseBranch)
			}
			mustGit(t, env.repoPath, "tag", "keep-tag")
			mustGit(t, env.repoPath, "push", "origin", "refs/tags/keep-tag")
			protectedBefore := gitOutput(t, env.repoPath, "show-ref", "refs/heads/master", "refs/heads/develop", "refs/heads/feature/APP-1", "refs/tags/keep-tag")
			remoteBefore := gitOutput(t, env.repoPath, "ls-remote", "origin", "refs/heads/master", "refs/heads/develop", "refs/heads/feature/APP-1", "refs/tags/keep-tag")

			plan, err := env.manager.PlanReleaseCleanup(t.Context(), release.ID, ReleaseDeletionSelection())
			if err != nil || len(plan.Preview().Blockers) != 0 {
				t.Fatalf("deletion preview = %+v, %v", plan.Preview(), err)
			}
			if _, err := env.manager.ExecuteReleaseCleanup(t.Context(), plan, nil); err != nil {
				t.Fatal(err)
			}
			if got := gitOutput(t, env.repoPath, "branch", "--list", "release/1.2.3"); got != "" {
				t.Fatalf("local release branch remains: %s", got)
			}
			if got := gitOutput(t, env.repoPath, "branch", "--remotes", "--list", "origin/release/1.2.3"); got != "" {
				t.Fatalf("remote-tracking release branch remains: %s", got)
			}
			if got := gitOutput(t, env.repoPath, "tag", "--list", "v1.2.3"); got != "" {
				t.Fatalf("local release tag remains: %s", got)
			}
			if got := gitOutput(t, env.repoPath, "ls-remote", "origin", "refs/heads/release/1.2.3", "refs/tags/v1.2.3*"); got != "" {
				t.Fatalf("remote release refs remain: %s", got)
			}
			if got := gitOutput(t, env.repoPath, "show-ref", "refs/heads/master", "refs/heads/develop", "refs/heads/feature/APP-1", "refs/tags/keep-tag"); got != protectedBefore {
				t.Fatalf("unrelated local refs changed:\n%s", got)
			}
			if got := gitOutput(t, env.repoPath, "ls-remote", "origin", "refs/heads/master", "refs/heads/develop", "refs/heads/feature/APP-1", "refs/tags/keep-tag"); got != remoteBefore {
				t.Fatalf("unrelated remote refs changed:\n%s", got)
			}
			if _, err := os.Stat(taskPath); err != nil {
				t.Fatalf("task worktree lost: %v", err)
			}
			if _, err := os.Stat(release.Dir); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("release directory remains: %v", err)
			}
			if got := gitOutput(t, env.repoPath, "worktree", "list", "--porcelain"); strings.Contains(got, release.Dir) {
				t.Fatalf("release worktree registration remains:\n%s", got)
			}
		})
	}
}

func TestIntegration_ReleaseDeletion_ChangedPlanPreservesResources(t *testing.T) {
	env := newReleaseIntegrationEnv(t)
	release := prepareReleaseForCleanupTest(t, env, "APP-61")
	plan, err := env.manager.PlanReleaseCleanup(t.Context(), release.ID, ReleaseDeletionSelection())
	if err != nil || len(plan.Preview().Blockers) != 0 {
		t.Fatalf("deletion preview = %+v, %v", plan.Preview(), err)
	}
	mustGit(t, release.Services[0].ReleaseWorktreePath, "commit", "--allow-empty", "-m", "changed after preview")
	if _, err := env.manager.ExecuteReleaseCleanup(t.Context(), plan, nil); !errors.Is(err, ErrReleaseCleanupBlocked) {
		t.Fatalf("stale plan error = %v", err)
	}
	if _, err := os.Stat(env.manager.releaseManifestPath(release.ID)); err != nil {
		t.Fatalf("manifest lost: %v", err)
	}
	if _, err := os.Stat(release.Services[0].ReleaseWorktreePath); err != nil {
		t.Fatalf("worktree removed by stale plan: %v", err)
	}
}

func TestIntegration_ReleaseDeletion_DraftPreservesUncreatedRefs(t *testing.T) {
	env := newReleaseIntegrationEnv(t)
	env.addFeatureTask(t, "APP-63", nil)
	release, err := env.manager.CreateRelease(t.Context(), CreateReleaseParams{
		TaskIDs: []string{"APP-63"}, ServiceVersions: map[string]string{"svc-api": "1.2.3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, env.repoPath, "branch", "release/1.2.3")
	mustGit(t, env.repoPath, "tag", "-a", "-m", "another release", "v1.2.3")
	mustGit(t, env.repoPath, "push", "origin", "release/1.2.3", "refs/tags/v1.2.3")
	before := gitOutput(t, env.repoPath, "show-ref")
	remoteBefore := gitOutput(t, env.repoPath, "ls-remote", "origin")
	plan, err := env.manager.PlanReleaseCleanup(t.Context(), release.ID, ReleaseDeletionSelection())
	if err != nil || len(plan.Preview().Blockers) != 0 {
		t.Fatalf("draft deletion preview = %+v, %v", plan.Preview(), err)
	}
	if _, err := env.manager.ExecuteReleaseCleanup(t.Context(), plan, nil); err != nil {
		t.Fatal(err)
	}
	if got := gitOutput(t, env.repoPath, "show-ref"); got != before {
		t.Fatalf("draft deleted refs it never created:\n%s", got)
	}
	if got := gitOutput(t, env.repoPath, "ls-remote", "origin"); got != remoteBefore {
		t.Fatalf("draft deleted remote refs it never created:\n%s", got)
	}
}

func TestIntegration_ReleaseDeletion_MissingCheckpointPreservesManifest(t *testing.T) {
	for _, checkpoint := range []string{"branch", "tag"} {
		t.Run(checkpoint, func(t *testing.T) {
			env := newReleaseIntegrationEnv(t)
			release := prepareReleaseForCleanupTest(t, env, "APP-64")
			if checkpoint == "tag" {
				release = env.markReleaseMasterMerged(t, release)
				var err error
				release, err = env.manager.FinalizeRelease(t.Context(), FinishReleaseParams{ReleaseID: release.ID})
				if err != nil {
					t.Fatal(err)
				}
				release.Services[0].TagRef = ""
				release.Services[0].TagSHA = ""
				release.Services[0].PushedTag = false
			} else {
				release.Services[0].ReleaseRef = ""
				release.Services[0].ReleaseSHA = ""
				release.Services[0].PushedReleaseBranch = false
			}
			release.Status = domain.ReleaseStatusFailed
			if _, err := env.manager.writeReleaseManifest(release); err != nil {
				t.Fatal(err)
			}
			plan, err := env.manager.PlanReleaseCleanup(t.Context(), release.ID, ReleaseDeletionSelection())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := env.manager.ExecuteReleaseCleanup(t.Context(), plan, nil); !errors.Is(err, ErrReleaseCleanupBlocked) {
				t.Fatalf("ambiguous %s ownership should block deletion: %v", checkpoint, err)
			}
			if _, err := os.Stat(env.manager.releaseManifestPath(release.ID)); err != nil {
				t.Fatalf("manifest lost with ambiguous %s ownership: %v", checkpoint, err)
			}
		})
	}
}

type failReleaseTagDeletionClient struct {
	git.Client
	fail bool
}

func (c *failReleaseTagDeletionClient) DeleteRemoteRefIfUnchanged(ctx context.Context, repo, url, ref, oid string) error {
	if c.fail && strings.HasPrefix(ref, "refs/tags/") {
		c.fail = false
		return errors.New("remote tag deletion denied")
	}
	return c.Client.DeleteRemoteRefIfUnchanged(ctx, repo, url, ref, oid)
}

func TestIntegration_ReleaseDeletion_PartialFailureCanRetry(t *testing.T) {
	g := &failReleaseTagDeletionClient{Client: &integrationGitClient{Client: git.NewCommandClient(newIntegrationLogger())}, fail: true}
	env := newReleaseIntegrationEnvWithGitClient(t, g)
	release := prepareReleaseForCleanupTest(t, env, "APP-62")
	release = env.markReleaseMasterMerged(t, release)
	release, err := env.manager.FinalizeRelease(t.Context(), FinishReleaseParams{ReleaseID: release.ID})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := env.manager.PlanReleaseCleanup(t.Context(), release.ID, ReleaseDeletionSelection())
	if err != nil || len(plan.Preview().Blockers) != 0 {
		t.Fatalf("deletion preview = %+v, %v", plan.Preview(), err)
	}
	if _, err := env.manager.ExecuteReleaseCleanup(t.Context(), plan, nil); err == nil {
		t.Fatal("expected remote deletion failure")
	}
	stored, err := env.manager.loadReleaseManifest(release.ID)
	if err != nil || stored.Status != domain.ReleaseStatusReleased {
		t.Fatalf("manifest unavailable for retry: %+v, %v", stored, err)
	}
	retry, err := env.manager.PlanReleaseCleanup(t.Context(), release.ID, ReleaseDeletionSelection())
	if err != nil || len(retry.Preview().Blockers) != 0 {
		t.Fatalf("retry preview = %+v, %v", retry.Preview(), err)
	}
	if _, err := env.manager.ExecuteReleaseCleanup(t.Context(), retry, nil); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if _, err := os.Stat(release.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("release remains after retry: %v", err)
	}
}
