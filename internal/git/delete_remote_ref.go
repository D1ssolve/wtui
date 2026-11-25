package git

import (
	"context"
	"fmt"
)

func (c *CommandClient) DeleteRemoteRefIfUnchanged(ctx context.Context, repoPath, capturedRemoteURL, ref, expectedOID string) error {
	if !validPushRemoteURL(capturedRemoteURL) {
		return fmt.Errorf("invalid push remote URL %q", capturedRemoteURL)
	}
	if !validRemoteRef(ref) || !validFullRef(ref) {
		return fmt.Errorf("invalid full remote ref %q", ref)
	}
	if !validObjectID(expectedOID) {
		return fmt.Errorf("invalid expected OID %q", expectedOID)
	}
	args, operand := pinnedRemoteArgs(repoPath, capturedRemoteURL)
	_, err := c.execGit(ctx, append(args,
		"push",
		"--force-with-lease="+ref+":"+expectedOID,
		"--", operand,
		":"+ref,
	)...)
	return err
}
