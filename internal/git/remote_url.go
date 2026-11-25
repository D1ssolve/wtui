package git

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

func (c *CommandClient) RemoteURL(ctx context.Context, repo, remote string) (string, error) {
	return c.remoteURL(ctx, repo, remote, "--all")
}

func (c *CommandClient) PushURL(ctx context.Context, repo, remote string) (string, error) {
	return c.remoteURL(ctx, repo, remote, "--push", "--all")
}

func (c *CommandClient) remoteURL(ctx context.Context, repo, remote string, flags ...string) (string, error) {
	if remote == "" {
		remote = "origin"
	}
	args := append([]string{"-C", repo, "remote", "get-url"}, flags...)
	out, err := c.execGitRaw(ctx, append(args, remote)...)
	if err != nil {
		return "", err
	}
	// Remove only Git's record delimiter. A second LF might be an empty
	// record or part of the address; neither is safe to authorize.
	endpoint, terminated := strings.CutSuffix(out, "\n")
	if !terminated || !validPushRemoteURL(endpoint) {
		return "", fmt.Errorf("remote %q did not resolve to one usable URL", remote)
	}
	return endpoint, nil
}

func validPushRemoteURL(endpoint string) bool {
	// Approval records may pass through JSON, which replaces invalid UTF-8.
	trimmed := strings.TrimSpace(endpoint)
	return trimmed != "" && !strings.HasPrefix(trimmed, "-") &&
		utf8.ValidString(endpoint) && !strings.ContainsAny(endpoint, "\x00\r\n")
}
