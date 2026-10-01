package git

import (
	"context"
	"fmt"
)

// EnsureCommit guarantees commit sha is readable from repoPath, fetching
// only that object from origin when missing. No local or remote refs are
// moved; a fetch failure is returned so callers fail closed.
func (c *CommandClient) EnsureCommit(ctx context.Context, repoPath, sha string) error {
	if !validObjectID(sha) {
		return fmt.Errorf("invalid commit SHA %q", sha)
	}
	if _, err := c.execGit(ctx, "-C", repoPath, "cat-file", "-e", sha+"^{commit}"); err == nil {
		return nil
	} else if ctx.Err() != nil {
		return ctx.Err()
	}
	_, err := c.execGit(ctx, "-C", repoPath, "fetch", "--no-tags", "origin", sha)
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
