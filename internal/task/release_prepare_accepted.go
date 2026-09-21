package task

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
)

func (m *manager) executeAcceptedPrepareService(ctx context.Context, release *domain.Release, svc *domain.ReleaseService, statusCh chan<- string) error {
	if strings.TrimSpace(svc.PostIntegrationSHA) == "" {
		return fmt.Errorf("%w: service=%s missing accepted integration SHA", ErrReleaseRetryUnsafe, svc.Name)
	}
	svc.Status = domain.ReleaseStatusMerging
	if err := m.persistCheckpoint(release, "fetch", nil); err != nil {
		return err
	}
	sendStatus(statusCh, fmt.Sprintf("[%s][fetch] verifying accepted integration SHA", svc.Name))
	if err := m.checkAcceptedIntegration(ctx, svc); err != nil {
		return err
	}
	integrationPath := filepath.Join(release.Dir, ".work", svc.Name+"-integration")
	if svc.IntegrationWorktreePath != "" && svc.IntegrationWorktreePath != integrationPath {
		return fmt.Errorf("%w: unexpected integration worktree path", ErrReleaseRetryUnsafe)
	}
	if err := m.ensureAcceptedWorktree(ctx, svc, git.WorktreeEntry{Path: integrationPath, HEAD: svc.PostIntegrationSHA, Branch: "(detached)"}); err != nil {
		return err
	}
	svc.IntegrationWorktreePath = integrationPath
	if err := m.persistCheckpoint(release, "integration_worktree", nil); err != nil {
		return err
	}
	svc.Status = domain.ReleaseStatusBranching
	exists, err := m.git.BranchExists(ctx, svc.RepoPath, svc.ReleaseBranch)
	if err != nil {
		return err
	}
	if exists {
		if err := m.checkAcceptedReleaseRef(ctx, svc, svc.ReleaseBranch); err != nil {
			return err
		}
	}
	remoteSHA, err := m.git.RemoteRefSHA(ctx, svc.RepoPath, "refs/heads/"+svc.ReleaseBranch)
	if err != nil {
		return err
	}
	if remoteSHA != "" && remoteSHA != svc.PostIntegrationSHA {
		return fmt.Errorf("%w: service=%s remote release SHA=%s want %s", ErrReleaseRetryUnsafe, svc.Name, remoteSHA, svc.PostIntegrationSHA)
	}
	if err := m.checkAcceptedIntegration(ctx, svc); err != nil {
		return err
	}
	if !exists {
		sendStatus(statusCh, fmt.Sprintf("[%s][branch] creating %s", svc.Name, svc.ReleaseBranch))
		if err := m.git.CreateBranchFromBranch(ctx, svc.RepoPath, svc.ReleaseBranch, svc.PostIntegrationSHA); err != nil {
			return err
		}
	}
	if err := m.checkAcceptedReleaseRef(ctx, svc, svc.ReleaseBranch); err != nil {
		return err
	}
	svc.ReleaseRef, svc.ReleaseSHA = svc.ReleaseBranch, svc.PostIntegrationSHA
	if err := m.persistCheckpoint(release, "branch", nil); err != nil {
		return err
	}
	if m.cfg.Release != nil && m.cfg.Release.CreateReleaseWorktrees != nil && *m.cfg.Release.CreateReleaseWorktrees {
		path := filepath.Join(release.Dir, "services", svc.Name)
		if svc.ReleaseWorktreePath != "" && svc.ReleaseWorktreePath != path {
			return fmt.Errorf("%w: unexpected release worktree path", ErrReleaseRetryUnsafe)
		}
		if err := m.ensureAcceptedWorktree(ctx, svc, git.WorktreeEntry{Path: path, HEAD: svc.PostIntegrationSHA, Branch: "refs/heads/" + svc.ReleaseBranch}); err != nil {
			return err
		}
		svc.ReleaseWorktreePath = path
	}
	svc.Status = domain.ReleaseStatusPushing
	if m.cfg.Release != nil && m.cfg.Release.PushReleaseBranches != nil && *m.cfg.Release.PushReleaseBranches {
		if err := m.checkAcceptedReleaseRef(ctx, svc, svc.ReleaseBranch); err != nil {
			return err
		}
		remoteSHA, err = m.git.RemoteRefSHA(ctx, svc.RepoPath, "refs/heads/"+svc.ReleaseBranch)
		if err != nil {
			return err
		}
		if remoteSHA != "" && remoteSHA != svc.PostIntegrationSHA {
			return fmt.Errorf("%w: remote release branch moved", ErrReleaseRetryUnsafe)
		}
		if remoteSHA == "" {
			sendStatus(statusCh, fmt.Sprintf("[%s][push] pushing release branch %s", svc.Name, svc.ReleaseBranch))
			if err := m.git.PushBranchExplicit(ctx, svc.RepoPath, svc.ReleaseBranch); err != nil {
				return err
			}
		}
		if err := m.checkAcceptedIntegration(ctx, svc); err != nil {
			return err
		}
		if err := m.checkAcceptedReleaseRef(ctx, svc, "origin/"+svc.ReleaseBranch); err != nil {
			return err
		}
		svc.PushedReleaseBranch = true
		if err := m.persistCheckpoint(release, "push_branch", nil); err != nil {
			return err
		}
	}
	if m.cfg.Release == nil || m.cfg.Release.KeepIntegrationWorktrees == nil || !*m.cfg.Release.KeepIntegrationWorktrees {
		commonDir, err := m.git.CommonDir(ctx, integrationPath)
		if err != nil {
			return err
		}
		if err := m.git.RemoveWorktree(ctx, commonDir, integrationPath, false); err != nil {
			return err
		}
		svc.IntegrationWorktreePath = ""
	}
	svc.Status = domain.ReleaseStatusPrepared
	if err := m.persistCheckpoint(release, "service_prepared", nil); err != nil {
		return err
	}
	sendStatus(statusCh, fmt.Sprintf("[%s][done] prepared", svc.Name))
	return nil
}

func (m *manager) checkAcceptedIntegration(ctx context.Context, svc *domain.ReleaseService) error {
	if err := m.git.Fetch(ctx, svc.RepoPath); err != nil {
		return err
	}
	return m.checkAcceptedReleaseRef(ctx, svc, "origin/"+svc.IntegrationBranch)
}

func (m *manager) checkAcceptedReleaseRef(ctx context.Context, svc *domain.ReleaseService, ref string) error {
	sha, err := m.resolveReleaseRefSHA(ctx, svc.RepoPath, ref)
	if err != nil {
		return err
	}
	if sha != svc.PostIntegrationSHA {
		return fmt.Errorf("%w: service=%s ref=%s SHA=%s want accepted %s", ErrReleaseRetryUnsafe, svc.Name, ref, sha, svc.PostIntegrationSHA)
	}
	return nil
}

func (m *manager) ensureAcceptedWorktree(ctx context.Context, svc *domain.ReleaseService, want git.WorktreeEntry) error {
	entries, err := m.git.ListWorktrees(ctx, svc.RepoPath)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if filepath.Clean(entry.Path) != filepath.Clean(want.Path) {
			continue
		}
		if entry.HEAD != want.HEAD || entry.Branch != want.Branch || entry.Locked {
			return fmt.Errorf("%w: mismatched worktree %s", ErrReleaseRetryUnsafe, want.Path)
		}
		dirty, err := m.git.IsDirty(ctx, want.Path)
		if err != nil {
			return err
		}
		states, err := m.git.OperationState(ctx, want.Path)
		if err != nil {
			return err
		}
		if dirty || len(states) != 0 {
			return fmt.Errorf("%w: dirty or active worktree %s", ErrReleaseRetryUnsafe, want.Path)
		}
		return nil
	}
	if _, err := os.Lstat(want.Path); err == nil {
		return fmt.Errorf("%w: unregistered worktree path %s", ErrReleaseRetryUnsafe, want.Path)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(want.Path), 0o755); err != nil {
		return err
	}
	if want.Branch == "(detached)" {
		return m.git.AddDetachedWorktree(ctx, svc.RepoPath, want.Path, want.HEAD)
	}
	return m.git.AddWorktree(ctx, svc.RepoPath, want.Path, svc.ReleaseBranch, false, "")
}
