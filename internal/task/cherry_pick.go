package task

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
)

// cherryPickIntoOpenReleases applies the accepted review merge to the release branch of every
// prepared, unfinished release of this repository, publishing each with a lease on the tip it
// was picked onto. Releases that already contain the change or its pick are skipped.
func (m *manager) cherryPickIntoOpenReleases(ctx context.Context, taskID string, svc domain.Service, mergeSHA string, step func(name string, status StepStatus, text string)) error {
	if mergeSHA == "" {
		return errors.New("cherry-pick: accepted merge SHA is unknown")
	}
	actionSHA, err := m.git.ResolveRef(ctx, svc.RepoPath, "refs/heads/"+svc.Branch)
	if err != nil {
		return fmt.Errorf("cherry-pick: resolve close action source %s: %w", svc.Branch, err)
	}
	releases, err := m.listReleaseManifestsWithContext(ctx)
	if err != nil {
		return fmt.Errorf("cherry-pick: list releases: %w", err)
	}
	if err := m.git.Fetch(ctx, svc.RepoPath); err != nil {
		return fmt.Errorf("cherry-pick: fetch %s: %w", svc.Name, err)
	}
	for _, release := range releases {
		if release.PreparedAt == nil || release.Status == domain.ReleaseStatusReleased || release.Status == domain.ReleaseStatusRejected {
			continue
		}
		for _, rs := range release.Services {
			if filepath.Clean(rs.RepoPath) != filepath.Clean(svc.RepoPath) || rs.ReleaseBranch == "" {
				continue
			}
			name := svc.Name + ":cherry-pick:" + rs.ReleaseBranch
			if rs.AcceptedMergeSHA != "" {
				// The tag SHA is already fixed; a pick could never reach this release.
				step(name, StepStatusSkipped, fmt.Sprintf("NOT picked: release %s already accepted %s; ship %s in a new release", release.ID, rs.AcceptedMergeSHA, mergeSHA))
				continue
			}
			picked, err := m.cherryPickIntoBranch(ctx, svc, rs.ReleaseBranch, mergeSHA)
			if err != nil {
				return fmt.Errorf("release %s: %w", release.ID, err)
			}
			if picked {
				step(name, StepStatusOK, "picked "+mergeSHA)
			} else {
				step(name, StepStatusSkipped, "already contains "+mergeSHA)
			}
		}
	}
	return m.proveClosePostAction(ctx, taskID, svc, closePostActionCherryPick, actionSHA)
}

func (m *manager) cherryPickIntoBranch(ctx context.Context, svc domain.Service, branch, mergeSHA string) (bool, error) {
	remoteRef := m.cfg.ServiceRemote().Tracking(branch)
	tip, err := m.git.ResolveRef(ctx, svc.RepoPath, remoteRef)
	if err != nil {
		return false, fmt.Errorf("resolve %s: %w", remoteRef, err)
	}
	if contained, err := m.git.IsAncestor(ctx, svc.RepoPath, mergeSHA, remoteRef); err != nil {
		return false, err
	} else if contained {
		return false, nil
	}
	picks, err := m.git.CommitsMentioning(ctx, svc.RepoPath, remoteRef, "(cherry picked from commit "+mergeSHA+")")
	if err != nil {
		return false, err
	}
	if len(picks) > 0 {
		return false, nil
	}

	dir, err := os.MkdirTemp("", "wtui-cherry-pick-*")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(dir)
	worktree := filepath.Join(dir, "wt")
	if err := m.git.AddDetachedWorktree(ctx, svc.RepoPath, worktree, tip); err != nil {
		return false, fmt.Errorf("prepare %s: %w", branch, err)
	}
	defer func() {
		if err := m.git.RemoveWorktree(context.WithoutCancel(ctx), svc.RepoPath, worktree, true); err != nil && m.logger != nil {
			m.logger.WarnContext(ctx, "cherry-pick: remove temporary worktree", "path", worktree, "error", err)
		}
	}()
	if err := m.git.CherryPick(ctx, worktree, mergeSHA); errors.Is(err, git.ErrCherryPickEmpty) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("cherry-pick %s onto %s failed; pick it manually with `git cherry-pick -x %s` and push %s: %w", mergeSHA, branch, mergeSHA, branch, err)
	}
	picked, err := m.git.ResolveRef(ctx, worktree, "HEAD")
	if err != nil {
		return false, err
	}
	pushURL, err := m.verifiedPushURL(ctx, svc)
	if err != nil {
		return false, err
	}
	ref := "refs/heads/" + branch
	if err := m.git.PushRefWithLease(ctx, svc.RepoPath, pushURL, ref, picked, ref, tip); err != nil {
		return false, fmt.Errorf("push %s: %w", branch, err)
	}
	return true, nil
}
