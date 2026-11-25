package git

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
)

// Git rewrites even explicit URL operands. Present a fresh prefix, then strip
// it in Git's single rewrite pass so configured aliases never match the URL.
func pinnedRemoteArgs(repo, url string) (args []string, operand string) {
	prefix := "wtui-" + rand.Text() + ":"
	return []string{"-C", repo, "-c", "url..insteadOf=" + prefix, "-c", "url..pushInsteadOf=" + prefix}, prefix + url
}

// RemoteRefSHAAt reads the exact ref from the captured push destination,
// regardless of subsequent changes to the repository's remote configuration.
func (c *CommandClient) RemoteRefSHAAt(ctx context.Context, repoPath, capturedRemoteURL, ref string) (string, error) {
	if !validPushRemoteURL(capturedRemoteURL) {
		return "", fmt.Errorf("invalid remote URL %q", capturedRemoteURL)
	}
	if !validRemoteRef(ref) || !validFullRef(strings.TrimSuffix(ref, "^{}")) {
		return "", fmt.Errorf("invalid full remote ref %q", ref)
	}
	args, operand := pinnedRemoteArgs(repoPath, capturedRemoteURL)
	out, err := c.execGit(ctx, append(args, "ls-remote", "--", operand, ref)...)
	if err != nil {
		return "", err
	}
	return parseRemoteRefSHA(out, ref)
}
