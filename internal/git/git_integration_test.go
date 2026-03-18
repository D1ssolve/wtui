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

// realPath resolves any OS-level symlinks in a path (necessary on macOS where
// t.TempDir() returns /var/folders/... which is a symlink to /private/var/folders/...,
// while git resolves symlinks and returns the canonical path).
func realPath(t *testing.T, p string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		// If the path doesn't exist yet (pre-creation), return as-is.
		return p
	}
	return resolved
}

// TestCommandClient_Integration exercises CommandClient against a real git repository
// created in a temporary directory. These tests require:
//   - git to be present in $PATH
//   - the "integration" build tag: go test -tags integration ./internal/git/...
func TestCommandClient_Integration(t *testing.T) {
	// Skip if git is not available in $PATH.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH, skipping integration tests")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	client := NewCommandClient(logger)
	ctx := context.Background()

	// Create a temporary directory and initialise a git repo inside it.
	repoDir := t.TempDir()
	mustGit(t, repoDir, "init")
	mustGit(t, repoDir, "config", "user.email", "test@example.com")
	mustGit(t, repoDir, "config", "user.name", "Test User")

	// Create an initial commit so the repo has a HEAD.
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
		// No remote set, so should fall back to the current branch name.
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
		// The initial commit should have created a branch (master or main).
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
		// Create an untracked file to make the repo dirty.
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
		// Get the current branch name.
		currentBranch, err := client.BaseBranch(ctx, repoDir)
		if err != nil {
			t.Fatalf("BaseBranch() error: %v", err)
		}

		// Create a linked worktree with a new branch.
		wtDir := filepath.Join(t.TempDir(), "linked-worktree")
		newBranch := "feature/integration-test"
		if err := client.AddWorktree(ctx, repoDir, wtDir, newBranch, true, currentBranch); err != nil {
			t.Fatalf("AddWorktree() error: %v", err)
		}

		// Verify the worktree directory was created.
		if _, statErr := os.Stat(wtDir); statErr != nil {
			t.Errorf("worktree directory not created: %v", statErr)
		}

		// Resolve symlinks on both sides so that macOS /var -> /private/var doesn't
		// cause a spurious mismatch between t.TempDir() and git's reported paths.
		wtDirReal := realPath(t, wtDir)

		// Verify it appears in ListWorktrees.
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

		// Get the common dir for removal.
		commonDir, err := client.CommonDir(ctx, repoDir)
		if err != nil {
			t.Fatalf("CommonDir() error: %v", err)
		}

		// Remove the worktree (non-force, clean worktree).
		if err := client.RemoveWorktree(ctx, commonDir, wtDir, false); err != nil {
			t.Fatalf("RemoveWorktree() error: %v", err)
		}
	})
}

// ─── test helpers ─────────────────────────────────────────────────────────────

// mustGit runs a git command in dir and fatals the test on error.
func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
}

// writeFile writes content to path, fataling the test on error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writeFile(%q): %v", path, err)
	}
}
