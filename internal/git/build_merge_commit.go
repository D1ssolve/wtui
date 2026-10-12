package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func (c *CommandClient) BuildMergeCommit(ctx context.Context, repoPath, targetSHA, headSHA, method string) (sha string, err error) {
	switch method {
	case "", "merge", "squash", "rebase":
	default:
		return "", fmt.Errorf("invalid merge method %q", method)
	}
	for _, oid := range []string{targetSHA, headSHA} {
		if !validObjectID(oid) {
			return "", fmt.Errorf("invalid commit SHA %q", oid)
		}
	}
	for _, oid := range []string{targetSHA, headSHA} {
		kind, err := c.execGit(ctx, "-C", repoPath, "cat-file", "-t", oid)
		if err != nil {
			return "", fmt.Errorf("validate commit %s: %w", oid, err)
		}
		if kind != "commit" {
			return "", fmt.Errorf("object %s is %s, not a commit", oid, kind)
		}
	}

	root, err := os.MkdirTemp("", "wtui-merge-")
	if err != nil {
		return "", fmt.Errorf("create merge worktree directory: %w", err)
	}
	worktree := filepath.Join(root, "worktree")
	// Register cleanup before add: cancellation can leave a partially added worktree.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), subprocessTimeout)
		defer cancel()
		if _, cleanupErr := c.execGit(cleanupCtx, "-C", repoPath, "worktree", "remove", "--force", worktree); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("remove merge worktree %s: %w", worktree, cleanupErr))
		}
		if cleanupErr := os.RemoveAll(root); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("remove merge directory %s: %w", root, cleanupErr))
		}
		if err != nil {
			sha = ""
		}
	}()

	start := targetSHA
	if method == "rebase" {
		start = headSHA
	}
	if err := c.AddDetachedWorktree(ctx, repoPath, worktree, start); err != nil {
		return "", fmt.Errorf("add merge worktree: %w", err)
	}
	switch method {
	case "", "merge":
		err = c.MergeNoFF(ctx, worktree, headSHA)
	case "squash":
		_, err = c.execGit(ctx, "-C", worktree, "merge", "--squash", headSHA)
		if err == nil {
			_, err = c.execGit(ctx, "-C", worktree, "commit", "--no-edit", "-m", "Squash "+headSHA)
		}
	case "rebase":
		// Preserve merge topology, and never let rebase.updateRefs move user branches.
		_, err = c.execGit(ctx, "-C", worktree, "rebase", "--rebase-merges", "--no-update-refs", "--no-autostash", targetSHA)
	}
	if err != nil {
		return "", fmt.Errorf("build %s commit: %w", method, err)
	}
	sha, err = c.ResolveRef(ctx, worktree, "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve built commit: %w", err)
	}
	if !validObjectID(sha) {
		return "", fmt.Errorf("invalid built commit SHA %q", sha)
	}
	return sha, nil
}
