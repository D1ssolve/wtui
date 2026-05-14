//go:build integration

package git

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
