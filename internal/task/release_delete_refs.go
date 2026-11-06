package task

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
)

func (m *manager) planReleaseDeletionRefs(ctx context.Context, plan *ReleaseCleanupPlan, status domain.ReleaseStatus, svc domain.ReleaseService, entries []git.WorktreeEntry, preview *ReleaseCleanupServicePreview) error {
	ownsBranch := svc.ReleaseSHA != "" || svc.ReleaseRef != "" || svc.PushedReleaseBranch
	ownsTag := svc.TagSHA != "" || svc.TagRef != "" || svc.PushedTag
	if !ownsBranch && !ownsTag && status == domain.ReleaseStatusDraft {
		return nil
	}
	url, err := m.verifiedRepoPushURL(ctx, svc.Name, svc.RepoPath)
	if err != nil {
		return err
	}
	if status != domain.ReleaseStatusDraft {
		for _, check := range []struct {
			ref   string
			owned bool
			local func(context.Context, string, string) (bool, error)
			name  string
		}{
			{"refs/heads/" + svc.ReleaseBranch, ownsBranch, m.git.BranchExists, svc.ReleaseBranch},
			{"refs/tags/" + svc.Tag, ownsTag, m.git.TagExists, svc.Tag},
		} {
			if check.owned {
				continue
			}
			exists, err := check.local(ctx, svc.RepoPath, check.name)
			if err != nil {
				return err
			}
			remote, err := m.git.RemoteRefSHAAt(ctx, svc.RepoPath, url, check.ref)
			if err != nil {
				return err
			}
			if exists || remote != "" {
				plan.block("service %s ref %s exists without a creation checkpoint; ownership is ambiguous", svc.Name, check.ref)
			}
		}
	}
	if ownsBranch {
		if svc.Version == "" || svc.ReleaseBranch != releaseBranchName(m.flow, svc.Version) || m.cleanupReleaseBranchProtected(svc.ReleaseBranch) {
			plan.block("service %s release branch is outside release ownership", svc.Name)
			return nil
		}
		preview.ReleaseBranch = svc.ReleaseBranch
		exists, err := m.git.BranchExists(ctx, svc.RepoPath, svc.ReleaseBranch)
		if err != nil {
			return err
		}
		localSHA := ""
		if exists {
			localSHA, err = m.git.ResolveRef(ctx, svc.RepoPath, "refs/heads/"+svc.ReleaseBranch)
			if err != nil {
				return err
			}
			for _, entry := range entries {
				if entry.Branch == "refs/heads/"+svc.ReleaseBranch && !samePath(entry.Path, svc.ReleaseWorktreePath) &&
					!samePath(entry.Path, filepath.Join(m.releasesRootDir(), plan.preview.ReleaseID, "services", svc.Name)) {
					plan.block("release branch %s is checked out outside its release", svc.ReleaseBranch)
				}
			}
		}
		plan.steps = append(plan.steps, releaseCleanupStep{
			kind: deleteReleaseLocalBranch, description: "delete local release branch " + svc.ReleaseBranch,
			repoPath: svc.RepoPath, branch: svc.ReleaseBranch, expectedSHA: localSHA, noop: !exists,
		})
		if err := m.planReleaseRemoteRefDeletion(ctx, plan, svc.RepoPath, url, "refs/heads/"+svc.ReleaseBranch); err != nil {
			return err
		}
	}
	if ownsTag {
		if svc.Version == "" || svc.Tag == "" || svc.Tag != formatReleaseTag(m.cfg, svc.Version) || svc.AcceptedMergeSHA == "" {
			plan.block("service %s tag is outside release ownership", svc.Name)
			return nil
		}
		preview.Tag = svc.Tag
		exists, err := m.git.TagExists(ctx, svc.RepoPath, svc.Tag)
		if err != nil {
			return err
		}
		oid := ""
		if exists {
			oid, err = m.resolveTagObjectSHA(ctx, svc.RepoPath, svc.Tag, svc.AcceptedMergeSHA)
			if err != nil {
				plan.block("service %s tag identity mismatch: %v", svc.Name, err)
			}
		}
		plan.steps = append(plan.steps, releaseCleanupStep{
			kind: deleteReleaseLocalTag, description: "delete local release tag " + svc.Tag,
			repoPath: svc.RepoPath, branch: svc.Tag, expectedSHA: oid, noop: !exists,
		})
		ref := "refs/tags/" + svc.Tag
		remoteOID, err := m.git.RemoteRefSHAAt(ctx, svc.RepoPath, url, ref)
		if err != nil {
			return err
		}
		if remoteOID != "" {
			if err := m.git.EnsureCommit(ctx, svc.RepoPath, remoteOID); err != nil {
				return err
			}
			peeled, err := m.git.ResolveRef(ctx, svc.RepoPath, remoteOID+"^{commit}")
			if err != nil {
				return err
			}
			if peeled != svc.AcceptedMergeSHA {
				plan.block("service %s remote tag identity mismatch", svc.Name)
			}
		}
		plan.steps = append(plan.steps, releaseCleanupStep{
			kind: deleteReleaseRemoteRef, description: "delete remote release tag " + svc.Tag,
			repoPath: svc.RepoPath, branch: ref, remoteURL: url, expectedSHA: remoteOID, noop: remoteOID == "",
		})
	}
	return nil
}

func (m *manager) planReleaseRemoteRefDeletion(ctx context.Context, plan *ReleaseCleanupPlan, repo, url, ref string) error {
	oid, err := m.git.RemoteRefSHAAt(ctx, repo, url, ref)
	if err != nil {
		return err
	}
	plan.steps = append(plan.steps, releaseCleanupStep{
		kind: deleteReleaseRemoteRef, description: "delete remote release branch " + strings.TrimPrefix(ref, "refs/heads/"),
		repoPath: repo, branch: ref, remoteURL: url, expectedSHA: oid, noop: oid == "",
	})
	return nil
}

func (m *manager) executeReleaseRefDeletion(ctx context.Context, step releaseCleanupStep) error {
	switch step.kind {
	case deleteReleaseRemoteRef:
		return m.git.DeleteRemoteRefIfUnchanged(ctx, step.repoPath, step.remoteURL, step.branch, step.expectedSHA)
	case deleteReleaseLocalTag:
		return m.git.DeleteTagIfUnchanged(ctx, step.repoPath, step.branch, step.expectedSHA)
	case deleteReleaseLocalBranch:
		entries, err := m.git.ListWorktrees(ctx, step.repoPath)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.Branch == "refs/heads/"+step.branch {
				return fmt.Errorf("release branch %s is still checked out at %s", step.branch, entry.Path)
			}
		}
		return m.git.DeleteBranchIfUnchanged(ctx, step.repoPath, step.branch, step.expectedSHA)
	default:
		return fmt.Errorf("unknown release deletion step %d", step.kind)
	}
}
