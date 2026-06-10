//go:build integration

package git

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
)

func realPath(t *testing.T, p string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {

		return p
	}
	return resolved
}

func TestCommandClient_Integration(t *testing.T) {

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH, skipping integration tests")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	client := NewCommandClient(logger)
	ctx := context.Background()

	repoDir := t.TempDir()
	mustGit(t, repoDir, "init")
	mustGit(t, repoDir, "config", "user.email", "test@example.com")
	mustGit(t, repoDir, "config", "user.name", "Test User")

	writeFile(t, filepath.Join(repoDir, "README.md"), "# test")
	mustGit(t, repoDir, "add", ".")
	mustGit(t, repoDir, "commit", "-m", "initial commit")

	t.Run("IsValidRepo", func(t *testing.T) {
		if err := client.IsValidRepo(ctx, repoDir); err != nil {
			t.Errorf("IsValidRepo(%q) error: %v", repoDir, err)
		}
	})

	t.Run("IsValidRepo_invalid", func(t *testing.T) {
		notARepo := t.TempDir()
		if err := client.IsValidRepo(ctx, notARepo); err == nil {
			t.Error("expected error for non-git directory, got nil")
		}
	})

	t.Run("Version", func(t *testing.T) {
		major, minor, err := client.Version(ctx)
		if err != nil {
			t.Fatalf("Version() error: %v", err)
		}
		if major < 2 {
			t.Errorf("expected major version >= 2, got %d.%d", major, minor)
		}
		t.Logf("git version: %d.%d", major, minor)
	})

	t.Run("BaseBranch_fallback", func(t *testing.T) {

		branch, err := client.BaseBranch(ctx, repoDir)
		if err != nil {
			t.Fatalf("BaseBranch() error: %v", err)
		}
		if branch == "" {
			t.Error("expected non-empty branch name")
		}
		t.Logf("base branch: %s", branch)
	})

	t.Run("BranchExists_false", func(t *testing.T) {
		exists, err := client.BranchExists(ctx, repoDir, "nonexistent-branch-xyz")
		if err != nil {
			t.Fatalf("BranchExists() error: %v", err)
		}
		if exists {
			t.Error("expected exists=false for nonexistent branch")
		}
	})

	t.Run("BranchExists_true", func(t *testing.T) {

		branch, err := client.BaseBranch(ctx, repoDir)
		if err != nil {
			t.Fatalf("BaseBranch() error: %v", err)
		}
		exists, err := client.BranchExists(ctx, repoDir, branch)
		if err != nil {
			t.Fatalf("BranchExists() error: %v", err)
		}
		if !exists {
			t.Errorf("expected branch %q to exist", branch)
		}
	})

	t.Run("RemoteBranchExists_false", func(t *testing.T) {
		remoteDir := t.TempDir()
		mustGit(t, remoteDir, "init", "--bare")
		removeRemoteIfExists(t, repoDir, "origin")
		mustGit(t, repoDir, "remote", "add", "origin", remoteDir)

		exists, err := client.RemoteBranchExists(ctx, repoDir, "nonexistent-remote-branch-xyz")
		if err != nil {
			t.Fatalf("RemoteBranchExists() error: %v", err)
		}
		if exists {
			t.Error("expected exists=false for nonexistent remote branch")
		}
	})

	t.Run("RemoteBranchExists_true", func(t *testing.T) {

		remoteDir := t.TempDir()
		mustGit(t, remoteDir, "init", "--bare")

		removeRemoteIfExists(t, repoDir, "origin")
		mustGit(t, repoDir, "remote", "add", "origin", remoteDir)

		branch, err := client.BaseBranch(ctx, repoDir)
		if err != nil {
			t.Fatalf("BaseBranch() error: %v", err)
		}

		mustGit(t, repoDir, "push", "-u", "origin", branch)

		exists, err := client.RemoteBranchExists(ctx, repoDir, branch)
		if err != nil {
			t.Fatalf("RemoteBranchExists() error: %v", err)
		}
		if !exists {
			t.Errorf("expected remote branch %q to exist", branch)
		}
	})

	t.Run("RemoteBranchExists_invalid_repo", func(t *testing.T) {
		notARepo := t.TempDir()
		_, err := client.RemoteBranchExists(ctx, notARepo, "some-branch")
		if err == nil {
			t.Error("expected error for non-git directory, got nil")
		}
	})

	t.Run("ListWorktrees", func(t *testing.T) {
		entries, err := client.ListWorktrees(ctx, repoDir)
		if err != nil {
			t.Fatalf("ListWorktrees() error: %v", err)
		}
		if len(entries) == 0 {
			t.Fatal("expected at least one worktree entry")
		}
		if entries[0].Path == "" {
			t.Error("expected non-empty worktree path")
		}
		t.Logf("worktrees: %+v", entries)
	})

	t.Run("CommonDir", func(t *testing.T) {
		dir, err := client.CommonDir(ctx, repoDir)
		if err != nil {
			t.Fatalf("CommonDir() error: %v", err)
		}
		if dir == "" {
			t.Error("expected non-empty common dir")
		}
		t.Logf("common dir: %s", dir)
	})

	t.Run("IsDirty_clean", func(t *testing.T) {
		dirty, err := client.IsDirty(ctx, repoDir)
		if err != nil {
			t.Fatalf("IsDirty() error: %v", err)
		}
		if dirty {
			t.Error("expected clean repo to not be dirty")
		}
	})

	t.Run("IsDirty_dirty", func(t *testing.T) {

		dirtyFile := filepath.Join(repoDir, "dirty.txt")
		writeFile(t, dirtyFile, "dirty content")
		t.Cleanup(func() { os.Remove(dirtyFile) })

		dirty, err := client.IsDirty(ctx, repoDir)
		if err != nil {
			t.Fatalf("IsDirty() error: %v", err)
		}
		if !dirty {
			t.Error("expected repo with untracked file to be dirty")
		}
	})

	t.Run("AddWorktree_and_RemoveWorktree", func(t *testing.T) {

		currentBranch, err := client.BaseBranch(ctx, repoDir)
		if err != nil {
			t.Fatalf("BaseBranch() error: %v", err)
		}

		wtDir := filepath.Join(t.TempDir(), "linked-worktree")
		newBranch := "feature/integration-test"
		if err := client.AddWorktree(ctx, repoDir, wtDir, newBranch, true, currentBranch); err != nil {
			t.Fatalf("AddWorktree() error: %v", err)
		}

		if _, statErr := os.Stat(wtDir); statErr != nil {
			t.Errorf("worktree directory not created: %v", statErr)
		}

		wtDirReal := realPath(t, wtDir)

		entries, err := client.ListWorktrees(ctx, repoDir)
		if err != nil {
			t.Fatalf("ListWorktrees() after AddWorktree error: %v", err)
		}
		found := false
		for _, e := range entries {
			if realPath(t, e.Path) == wtDirReal {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("new worktree %q not found in list: %+v", wtDir, entries)
		}

		commonDir, err := client.CommonDir(ctx, repoDir)
		if err != nil {
			t.Fatalf("CommonDir() error: %v", err)
		}

		if err := client.RemoveWorktree(ctx, commonDir, wtDir, false); err != nil {
			t.Fatalf("RemoveWorktree() error: %v", err)
		}
	})

	t.Run("RepoStatus_parses_conflicts", func(t *testing.T) {
		mustGit(t, repoDir, "checkout", "-b", "status-conflict")
		writeFile(t, filepath.Join(repoDir, "conflict.txt"), "base\n")
		mustGit(t, repoDir, "add", "conflict.txt")
		mustGit(t, repoDir, "commit", "-m", "add conflict file")

		mustGit(t, repoDir, "checkout", "-b", "status-side")
		writeFile(t, filepath.Join(repoDir, "conflict.txt"), "side\n")
		mustGit(t, repoDir, "add", "conflict.txt")
		mustGit(t, repoDir, "commit", "-m", "side change")

		mustGit(t, repoDir, "checkout", "status-conflict")
		writeFile(t, filepath.Join(repoDir, "conflict.txt"), "main\n")
		mustGit(t, repoDir, "add", "conflict.txt")
		mustGit(t, repoDir, "commit", "-m", "main change")

		cmd := exec.Command("git", "merge", "status-side")
		cmd.Dir = repoDir
		_ = cmd.Run()

		writeFile(t, filepath.Join(repoDir, "status-untracked.txt"), "u\n")

		status, err := client.RepoStatus(ctx, repoDir)
		if err != nil {
			t.Fatalf("RepoStatus() error: %v", err)
		}

		if len(status.ConflictPaths) == 0 {
			t.Fatalf("expected conflict paths, got none: %+v", status)
		}
		if !slices.Contains(status.ConflictPaths, "conflict.txt") {
			t.Fatalf("conflict paths = %+v, want conflict.txt", status.ConflictPaths)
		}
		if !slices.Contains(status.UntrackedPaths, "status-untracked.txt") {
			t.Fatalf("untracked paths = %+v, want status-untracked.txt", status.UntrackedPaths)
		}
		foundConflictXY := false
		for _, entry := range status.ChangedEntries {
			if entry.Path == "conflict.txt" && len(entry.XY) == 2 && entry.XY != "  " {
				foundConflictXY = true
				break
			}
		}
		if !foundConflictXY {
			t.Fatalf("expected conflict entry for conflict.txt in %+v", status.ChangedEntries)
		}

		mustGit(t, repoDir, "merge", "--abort")
		_ = os.Remove(filepath.Join(repoDir, "status-untracked.txt"))
	})

	t.Run("OperationState_detects_all_interrupted_operations", func(t *testing.T) {
		commonDir, err := client.CommonDir(ctx, repoDir)
		if err != nil {
			t.Fatalf("CommonDir() error: %v", err)
		}

		for _, name := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "BISECT_LOG"} {
			if err := os.WriteFile(filepath.Join(commonDir, name), []byte("x"), 0o600); err != nil {
				t.Fatalf("write %s: %v", name, err)
			}
			t.Cleanup(func() { _ = os.Remove(filepath.Join(commonDir, name)) })
		}
		for _, name := range []string{"rebase-merge", "rebase-apply"} {
			dir := filepath.Join(commonDir, name)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", name, err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
		}

		states, err := client.OperationState(ctx, repoDir)
		if err != nil {
			t.Fatalf("OperationState() error: %v", err)
		}

		for _, want := range []domain.RepoState{
			domain.RepoStateMerging,
			domain.RepoStateRebasing,
			domain.RepoStateCherryPick,
			domain.RepoStateReverting,
			domain.RepoStateBisect,
		} {
			if !slices.Contains(states, want) {
				t.Fatalf("states = %+v, missing %v", states, want)
			}
		}
	})

	t.Run("IsAncestor_maps_exit_codes", func(t *testing.T) {
		mustGit(t, repoDir, "checkout", "-B", "ancestor-main")
		writeFile(t, filepath.Join(repoDir, "ancestor.txt"), "1\n")
		mustGit(t, repoDir, "add", "ancestor.txt")
		mustGit(t, repoDir, "commit", "-m", "ancestor base")

		mustGit(t, repoDir, "checkout", "-b", "ancestor-feature")
		writeFile(t, filepath.Join(repoDir, "ancestor.txt"), "2\n")
		mustGit(t, repoDir, "add", "ancestor.txt")
		mustGit(t, repoDir, "commit", "-m", "feature commit")

		ok, err := client.IsAncestor(ctx, repoDir, "ancestor-main", "ancestor-feature")
		if err != nil {
			t.Fatalf("IsAncestor(true case) error: %v", err)
		}
		if !ok {
			t.Fatal("IsAncestor(true case) = false, want true")
		}

		notAncestor, err := client.IsAncestor(ctx, repoDir, "ancestor-feature", "ancestor-main")
		if err != nil {
			t.Fatalf("IsAncestor(false case) error: %v", err)
		}
		if notAncestor {
			t.Fatal("IsAncestor(false case) = true, want false")
		}
	})

	t.Run("Tag_operations", func(t *testing.T) {
		remoteDir := t.TempDir()
		mustGit(t, remoteDir, "init", "--bare")
		removeRemoteIfExists(t, repoDir, "origin")
		mustGit(t, repoDir, "remote", "add", "origin", remoteDir)

		baseBranch, err := client.BaseBranch(ctx, repoDir)
		if err != nil {
			t.Fatalf("BaseBranch() error: %v", err)
		}
		mustGit(t, repoDir, "checkout", baseBranch)
		mustGit(t, repoDir, "push", "-u", "origin", baseBranch)

		if err := client.CreateTag(ctx, repoDir, "v1.2.0", baseBranch, "Release v1.2.0"); err != nil {
			t.Fatalf("CreateTag() error: %v", err)
		}

		objTypeOut, err := exec.Command("git", "-C", repoDir, "cat-file", "-t", "v1.2.0").CombinedOutput()
		if err != nil {
			t.Fatalf("cat-file -t v1.2.0: %v\n%s", err, objTypeOut)
		}
		if strings.TrimSpace(string(objTypeOut)) != "tag" {
			t.Fatalf("tag object type = %q, want tag", strings.TrimSpace(string(objTypeOut)))
		}

		if err := client.PushTag(ctx, repoDir, "v1.2.0"); err != nil {
			t.Fatalf("PushTag() error: %v", err)
		}
		remoteTagOut, err := exec.Command("git", "-C", remoteDir, "show-ref", "--tags", "v1.2.0").CombinedOutput()
		if err != nil {
			t.Fatalf("remote show-ref for tag failed: %v\n%s", err, remoteTagOut)
		}

		if err := client.CreateTag(ctx, repoDir, "v1.3.0", baseBranch, "Release v1.3.0"); err != nil {
			t.Fatalf("CreateTag v1.3.0 error: %v", err)
		}
		mustGit(t, repoDir, "tag", "not-semver")

		tags, err := client.ListTags(ctx, repoDir)
		if err != nil {
			t.Fatalf("ListTags() error: %v", err)
		}
		if len(tags) < 3 {
			t.Fatalf("expected at least 3 tags, got %d", len(tags))
		}
		if tags[0].Name != "v1.3.0" || tags[1].Name != "v1.2.0" {
			t.Fatalf("unexpected semver sort order: %+v", tags)
		}

		latest, err := client.LatestSemverTag(ctx, repoDir, baseBranch)
		if err != nil {
			t.Fatalf("LatestSemverTag() error: %v", err)
		}
		if latest != "v1.3.0" {
			t.Fatalf("LatestSemverTag = %q, want v1.3.0", latest)
		}

		firstCommitOut, err := exec.Command("git", "-C", repoDir, "rev-list", "--max-parents=0", "HEAD").CombinedOutput()
		if err != nil {
			t.Fatalf("rev-list first commit failed: %v\n%s", err, firstCommitOut)
		}
		firstCommit := strings.TrimSpace(string(firstCommitOut))
		mustGit(t, repoDir, "checkout", "-b", "no-semver-branch", firstCommit)
		noSemver, err := client.LatestSemverTag(ctx, repoDir, "no-semver-branch")
		if err != nil {
			t.Fatalf("LatestSemverTag(no semver) error: %v", err)
		}
		if noSemver != "" {
			t.Fatalf("LatestSemverTag(no semver) = %q, want empty", noSemver)
		}
	})
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writeFile(%q): %v", path, err)
	}
}

func removeRemoteIfExists(t *testing.T, dir, name string) {
	t.Helper()
	cmd := exec.Command("git", "remote", "get-url", name)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		return
	}
	mustGit(t, dir, "remote", "remove", name)
}
