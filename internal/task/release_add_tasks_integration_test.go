//go:build integration

package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// unmergedFeatureTask creates a pushed task branch off develop that develop does not contain.
func (e releaseIntegrationEnv) unmergedFeatureTask(t *testing.T, taskID, file, content string) string {
	t.Helper()
	branch := "feature/" + taskID
	wt := filepath.Join(e.tasksRoot, taskID, "svc-api")
	if err := os.MkdirAll(filepath.Dir(wt), 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, e.repoPath, "worktree", "add", "-b", branch, wt, "origin/develop")
	writeFile(t, filepath.Join(wt, file), content)
	mustGit(t, wt, "add", file)
	mustGit(t, wt, "commit", "-m", "feat("+taskID+"): "+file)
	mustGit(t, wt, "push", "-u", "origin", branch)
	return gitOutput(t, wt, "rev-parse", "HEAD")
}

func TestAddReleaseTasks_Integration_MergesIntoPreparedRelease(t *testing.T) {
	ctx := context.Background()
	env := newReleaseIntegrationEnv(t)
	env.addFeatureTask(t, "APP-1", func(wt string) {
		writeFile(t, filepath.Join(wt, "one.txt"), "one\n")
		mustGit(t, wt, "add", "one.txt")
		mustGit(t, wt, "commit", "-m", "feat(APP-1): one")
	})
	release, err := env.manager.CreateRelease(ctx, CreateReleaseParams{TaskIDs: []string{"APP-1"}, ServiceVersions: map[string]string{"svc-api": "1.2.3"}, StartImmediately: true})
	if err != nil {
		t.Fatalf("CreateRelease: %v", err)
	}
	head := env.unmergedFeatureTask(t, "APP-2", "two.txt", "two\n")
	tipBefore := gitOutput(t, env.repoPath, "rev-parse", "origin/release/1.2.3")

	plan, err := env.manager.PlanReleaseTaskAdd(ctx, release.ID, []string{"APP-2"})
	if err != nil {
		t.Fatalf("PlanReleaseTaskAdd: %v", err)
	}
	if !plan.Ready() || len(plan.Services) != 1 || plan.Services[0].ReleaseTipSHA != tipBefore || plan.Services[0].Branches[0].HeadSHA != head {
		t.Fatalf("plan = %+v, want one ready service pinned to tip %s and head %s", plan, tipBefore, head)
	}
	if got := gitOutput(t, env.repoPath, "rev-parse", "origin/release/1.2.3"); got != tipBefore {
		t.Fatalf("planning moved the release branch to %s", got)
	}

	updated, err := env.manager.AddReleaseTasks(ctx, plan, nil)
	if err != nil {
		t.Fatalf("AddReleaseTasks: %v", err)
	}
	mustGit(t, env.repoPath, "fetch", "origin")
	tip := gitOutput(t, env.repoPath, "rev-parse", "origin/release/1.2.3")
	if ok, err := gitIsAncestor(env.repoPath, head, tip); err != nil || !ok {
		t.Fatalf("release branch %s does not contain APP-2 head %s (%v)", tip, head, err)
	}
	if parents := gitOutput(t, env.repoPath, "rev-list", "--parents", "-n", "1", tip); parents != tip+" "+tipBefore+" "+head {
		t.Fatalf("release tip parents = %q, want a merge of %s and %s", parents, tipBefore, head)
	}
	svc := updated.Services[0]
	fb := svc.FeatureBranches[len(svc.FeatureBranches)-1]
	if !slices.Equal(updated.TaskIDs, []string{"APP-1", "APP-2"}) || fb.TaskID != "APP-2" || !fb.Merged || fb.MergeRef != tip || svc.ReleaseSHA != tip {
		t.Fatalf("manifest tasks=%v fb=%+v release_sha=%s, want APP-2 recorded at %s", updated.TaskIDs, fb, svc.ReleaseSHA, tip)
	}
	if stored, err := env.manager.GetRelease(ctx, release.ID); err != nil || !slices.Equal(stored.TaskIDs, updated.TaskIDs) {
		t.Fatalf("stored manifest tasks = %v (%v), want %v", stored.TaskIDs, err, updated.TaskIDs)
	}
	if svc.ReleaseWorktreePath != "" {
		if got := gitOutput(t, svc.ReleaseWorktreePath, "rev-parse", "HEAD"); got != tip {
			t.Fatalf("release worktree HEAD = %s, want fast-forwarded to %s", got, tip)
		}
	}

	if _, err := env.manager.PlanReleaseTaskAdd(ctx, release.ID, []string{"APP-2"}); !errors.Is(err, ErrReleaseDuplicateTasks) {
		t.Fatalf("re-adding APP-2: err = %v, want ErrReleaseDuplicateTasks", err)
	}
}

func TestAddReleaseTasks_Integration_ConflictAndMovedTipPushNothing(t *testing.T) {
	ctx := context.Background()
	env := newReleaseIntegrationEnv(t)
	env.addFeatureTask(t, "APP-1", func(wt string) {
		writeFile(t, filepath.Join(wt, "shared.txt"), "release\n")
		mustGit(t, wt, "add", "shared.txt")
		mustGit(t, wt, "commit", "-m", "feat(APP-1): shared")
	})
	release, err := env.manager.CreateRelease(ctx, CreateReleaseParams{TaskIDs: []string{"APP-1"}, ServiceVersions: map[string]string{"svc-api": "1.2.3"}, StartImmediately: true})
	if err != nil {
		t.Fatalf("CreateRelease: %v", err)
	}

	// master predates shared.txt, so APP-3 adds it with content that conflicts with the release.
	mustGit(t, env.repoPath, "fetch", "origin")
	conflictWT := filepath.Join(env.tasksRoot, "APP-3", "svc-api")
	if err := os.MkdirAll(filepath.Dir(conflictWT), 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, env.repoPath, "worktree", "add", "-b", "feature/APP-3", conflictWT, "origin/master")
	writeFile(t, filepath.Join(conflictWT, "shared.txt"), "task\n")
	mustGit(t, conflictWT, "add", "shared.txt")
	mustGit(t, conflictWT, "commit", "-m", "feat(APP-3): shared differently")
	plan, err := env.manager.PlanReleaseTaskAdd(ctx, release.ID, []string{"APP-3"})
	if err != nil {
		t.Fatalf("PlanReleaseTaskAdd(conflict): %v", err)
	}
	if plan.Ready() || !slices.Equal(plan.Services[0].Branches[0].Conflicts, []string{"shared.txt"}) {
		t.Fatalf("conflict plan = %+v, want blocked on shared.txt", plan)
	}
	if _, err := env.manager.AddReleaseTasks(ctx, plan, nil); err == nil {
		t.Fatal("AddReleaseTasks ran a blocked plan")
	}

	env.unmergedFeatureTask(t, "APP-4", "four.txt", "four\n")
	plan, err = env.manager.PlanReleaseTaskAdd(ctx, release.ID, []string{"APP-4"})
	if err != nil || !plan.Ready() {
		t.Fatalf("PlanReleaseTaskAdd(APP-4) = %+v, %v", plan, err)
	}
	other := filepath.Join(t.TempDir(), "other")
	mustGit(t, env.repoPath, "worktree", "add", "--detach", other, "origin/release/1.2.3")
	writeFile(t, filepath.Join(other, "hotfix.txt"), "moved\n")
	mustGit(t, other, "add", "hotfix.txt")
	mustGit(t, other, "commit", "-m", "fix: release moved")
	mustGit(t, other, "push", "origin", "HEAD:refs/heads/release/1.2.3")
	moved := gitOutput(t, other, "rev-parse", "HEAD")

	if _, err := env.manager.AddReleaseTasks(ctx, plan, nil); !errors.Is(err, ErrReleaseRetryUnsafe) {
		t.Fatalf("AddReleaseTasks after the tip moved: err = %v, want ErrReleaseRetryUnsafe", err)
	}
	mustGit(t, env.repoPath, "fetch", "origin")
	if got := gitOutput(t, env.repoPath, "rev-parse", "origin/release/1.2.3"); got != moved {
		t.Fatalf("release branch = %s, want untouched %s", got, moved)
	}
	if stored, err := env.manager.GetRelease(ctx, release.ID); err != nil || slices.Contains(stored.TaskIDs, "APP-4") {
		t.Fatalf("stored tasks = %v (%v), want APP-4 not recorded", stored.TaskIDs, err)
	}
}
