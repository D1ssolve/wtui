package git

import (
	"context"
	"fmt"
	"strings"
)

// RemoteRefSHAAt reads the exact ref from the captured push destination,
// regardless of subsequent changes to the repository's remote configuration.
func (c *CommandClient) RemoteRefSHAAt(ctx context.Context, repoPath, capturedRemoteURL, ref string) (string, error) {
	if !validPushRemoteURL(capturedRemoteURL) {
		return "", fmt.Errorf("invalid remote URL %q", capturedRemoteURL)
	}
	if !validRemoteRef(ref) || !validFullRef(strings.TrimSuffix(ref, "^{}")) {
		return "", fmt.Errorf("invalid full remote ref %q", ref)
	}
	out, err := c.execGit(ctx, "-C", repoPath, "ls-remote", "--", capturedRemoteURL, ref)
	if err != nil {
		return "", err
	}
	return parseRemoteRefSHA(out, ref)
}
