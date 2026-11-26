package task

import (
	"context"
	"fmt"
)

// Check every selected ff-only target before the first service publishes. Execution
// still uses git merge --ff-only and a lease: preflight cannot exclude a racing push.
func (m *manager) preflightFastForward(ctx context.Context, sp ServiceClosePlan) error {
	if err := m.git.Fetch(ctx, sp.RepoPath); err != nil {
		return err
	}
	for _, target := range sp.TargetBranches {
		remote := m.cfg.ServiceRemote().Tracking(target)
		done, err := m.git.IsAncestor(ctx, sp.RepoPath, sp.SourceBranch, remote)
		if err != nil {
			return err
		}
		if done {
			continue
		}
		for _, base := range []string{remote, "refs/heads/" + target} {
			ok, err := m.git.IsAncestor(ctx, sp.RepoPath, base, sp.SourceBranch)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("%s: cannot fast-forward %s to %s", sp.ServiceName, base, sp.SourceBranch)
			}
		}
	}
	return nil
}
