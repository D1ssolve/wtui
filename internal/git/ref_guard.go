package git

import (
	"context"
	"fmt"
)

// VerifyLocalRefGuards preflights the same direct refs and OIDs as guarded
// deletion. It does not replace verification inside the delete transaction.
func (c *CommandClient) VerifyLocalRefGuards(ctx context.Context, repo string, guards []RefGuard) error {
	if err := c.rejectSymbolicGuardRefs(ctx, repo, guards); err != nil {
		return err
	}
	for _, guard := range guards {
		oid, err := c.execGit(ctx, "-C", repo, "show-ref", "--verify", "--hash", guard.Ref)
		if err != nil {
			return err
		}
		if oid != guard.OID {
			return fmt.Errorf("%s is at %s, expected %s; refresh the tracking ref and retry", guard.Ref, oid, guard.OID)
		}
	}
	return nil
}
