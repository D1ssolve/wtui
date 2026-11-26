package task

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

// FinalizeRelease fires the release's F event; in git-flow it back-merges and tags.
func (m *manager) FinalizeRelease(ctx context.Context, params FinishReleaseParams) (domain.Release, error) {
	mach := m.releaseMachine()
	tags := func(ev gitflow.Event) bool {
		target, ok := mach.State(ev.To)
		return ev.Key == "F" && ok && slices.ContainsFunc(target.Run, func(s gitflow.Step) bool { return s.Kind == gitflow.StepTag })
	}
	release, fired, err := m.advanceRelease(ctx, params.ReleaseID, tags, "", &releaseRun{statusCh: params.StatusCh})
	if fired == nil && err == nil {
		err = fmt.Errorf("%w: release %s cannot be finalized from state %s", ErrReleaseInvalidStatusTransition, params.ReleaseID, release.State)
	}
	return release, err
}

// acceptReleaseBranchTip records the remote release branch tip as the release SHA on the
// first finalize; a retry keeps the recorded SHA as long as the branch still contains it.
func (m *manager) acceptReleaseBranchTip(ctx context.Context, svc *domain.ReleaseService) error {
	remoteRelease := m.cfg.ServiceRemote().Tracking(svc.ReleaseBranch)
	tip, err := m.resolveReleaseRefSHA(ctx, svc.RepoPath, remoteRelease)
	if err != nil {
		return err
	}
	// Review merges also set AcceptedMergeSHA; only a recorded release-branch tip may pin a retry.
	accepted, recorded := svc.Accepted[svc.ReleaseBranch]
	if !recorded && len(svc.Accepted) == 0 {
		accepted = svc.AcceptedMergeSHA // manifests written before Accepted existed
	}
	if accepted != "" && accepted != tip {
		contained, err := m.git.IsAncestor(ctx, svc.RepoPath, accepted, remoteRelease)
		if err != nil {
			return err
		}
		if !contained {
			return fmt.Errorf("%w: service=%s release branch %s no longer contains accepted %s (tip %s)", ErrReleaseMasterMoved, svc.Name, svc.ReleaseBranch, accepted, tip)
		}
		tip = accepted
	}
	if svc.Accepted == nil {
		svc.Accepted = map[string]string{}
	}
	svc.Accepted[svc.ReleaseBranch] = tip
	svc.AcceptedMergeSHA = tip
	return nil
}

// syncFinalizeService merges fromRef into the into branch through a temporary worktree
// when into does not already contain it, then publishes into.
func (m *manager) syncFinalizeService(ctx context.Context, release *domain.Release, svc *domain.ReleaseService, fromRef, into string, statusCh chan<- string) (err error) {
	integrationPath := filepath.Join(release.Dir, ".work", svc.Name+"-finalize-integration")
	if err := os.MkdirAll(filepath.Dir(integrationPath), 0o755); err != nil {
		return fmt.Errorf("%w: %v", ErrReleaseManifestInvalid, err)
	}
	if svc.IntegrationWorktreePath != "" {
		if err := m.removeOwnedIntegrationWorktree(ctx, release, svc, svc.IntegrationWorktreePath, svc.PostIntegrationSHA); err != nil {
			return fmt.Errorf("release finalize: remove retained worktree: %w", err)
		}
		svc.IntegrationWorktreePath = ""
	}
	remoteIntegration := m.cfg.ServiceRemote().Tracking(into)
	if err := m.git.AddDetachedWorktree(ctx, svc.RepoPath, integrationPath, remoteIntegration); err != nil {
		return err
	}
	svc.IntegrationWorktreePath = integrationPath
	svc.Status = domain.ReleaseStatusSyncingDevelop
	pushIntegration := m.cfg.Release != nil && m.cfg.Release.PushIntegration != nil && *m.cfg.Release.PushIntegration
	keep := !pushIntegration || m.cfg.Release.KeepIntegrationWorktrees != nil && *m.cfg.Release.KeepIntegrationWorktrees
	defer func() {
		if keep {
			return
		}
		if err := m.removeOwnedIntegrationWorktree(ctx, release, svc, integrationPath, svc.PostIntegrationSHA); err != nil {
			sendStatus(statusCh, fmt.Sprintf("[%s][finalize] integration worktree preserved: %v", svc.Name, err))
			return
		}
		svc.IntegrationWorktreePath = ""
	}()
	if err := m.persistCheckpoint(release, "integration_worktree", nil); err != nil {
		return err
	}

	if err := m.git.MergeFFOnly(ctx, integrationPath, remoteIntegration); err != nil {
		return fmt.Errorf("release finalize: fast-forward service=%s integration=%s: %w", svc.Name, into, err)
	}
	alreadyMerged, err := m.git.IsAncestor(ctx, integrationPath, fromRef, "HEAD")
	if err != nil {
		return err
	}
	if alreadyMerged {
		sendStatus(statusCh, fmt.Sprintf("[%s][sync] %s already contains %s", svc.Name, into, fromRef))
	} else {
		sendStatus(statusCh, fmt.Sprintf("[%s][sync] merging %s into %s", svc.Name, fromRef, into))
		if err := m.git.Merge(ctx, integrationPath, fromRef); err != nil {
			states, stateErr := m.git.OperationState(ctx, integrationPath)
			if stateErr == nil && containsMergeConflictState(states) {
				_ = m.git.MergeAbort(ctx, integrationPath)
				return fmt.Errorf("%w: service=%s branch=%s", ErrReleaseMergeConflict, svc.Name, fromRef)
			}
			return err
		}
	}

	if pushIntegration {
		// Prove the push destination before mutating the remote: an
		// integration push to a fetch-vs-push mismatched remote would ship
		// nothing the release identity covers.
		if _, err := m.verifiedRepoPushURL(ctx, svc.Name, svc.RepoPath); err != nil {
			return fmt.Errorf("release finalize: verify push destination for service=%s: %w", svc.Name, err)
		}
		if err := m.git.PushRef(ctx, integrationPath, "HEAD", into); err != nil {
			return fmt.Errorf("release finalize: push service=%s integration=%s: %w", svc.Name, into, err)
		}
		svc.PushedIntegration = true
	}
	if into != svc.IntegrationBranch {
		return m.persistCheckpoint(release, "back_merge", nil)
	}
	sha, err := m.resolveReleaseRefSHA(ctx, integrationPath, "HEAD")
	if err != nil {
		return err
	}
	svc.PostIntegrationSHA = sha
	if pushIntegration {
		svc.PostIntegrationRef = remoteIntegration
	} else {
		svc.PostIntegrationRef = sha
	}
	return m.persistCheckpoint(release, "sync_develop", nil)
}

func (m *manager) validateFinalizeTag(ctx context.Context, svc domain.ReleaseService) error {
	exists, err := m.git.TagExists(ctx, svc.RepoPath, svc.Tag)
	if err != nil || !exists {
		return err
	}
	sha, err := m.resolveReleaseRefSHA(ctx, svc.RepoPath, svc.Tag+"^{}")
	if err != nil {
		return err
	}
	if sha != svc.AcceptedMergeSHA {
		return fmt.Errorf("%w: service=%s tag=%s expected=%s actual=%s", ErrReleaseRetryUnsafe, svc.Name, svc.Tag, svc.AcceptedMergeSHA, sha)
	}
	return nil
}

func (m *manager) tagFinalizeService(ctx context.Context, release *domain.Release, svc *domain.ReleaseService, statusCh chan<- string) error {
	exists, err := m.git.TagExists(ctx, svc.RepoPath, svc.Tag)
	if err != nil {
		return err
	}
	if !exists {
		sendStatus(statusCh, fmt.Sprintf("[%s][tag] creating %s", svc.Name, svc.Tag))
		message, err := m.releaseTagMessageFor(ctx, release, svc)
		if err != nil {
			return err
		}
		if err := m.git.CreateTag(ctx, svc.RepoPath, svc.Tag, svc.AcceptedMergeSHA, message); err != nil {
			return fmt.Errorf("%w: service=%s tag=%s: %v", ErrReleaseTagCreateFailed, svc.Name, svc.Tag, err)
		}
	}
	svc.TagRef = svc.Tag
	svc.TagSHA = svc.AcceptedMergeSHA
	svc.Status = domain.ReleaseStatusTagging
	return m.persistCheckpoint(release, "tag", nil)
}

func (m *manager) failFinalization(release *domain.Release, svc *domain.ReleaseService, err error) (domain.Release, error) {
	releaseErr := classifyReleaseError(err, svc)
	_ = m.failRelease(release, releaseErr)
	return *release, err
}

// retryFinishRelease uses this for unfinished service checkpoints.
func (m *manager) runFinishService(ctx context.Context, release *domain.Release, svc *domain.ReleaseService, _ chan<- string) error {
	if svc.PushedTag {
		return nil
	}
	if svc.AcceptedMergeSHA == "" {
		return fmt.Errorf("%w: service=%s accepted merge SHA missing", ErrReleaseLegacyManifest, svc.Name)
	}
	exists, err := m.git.TagExists(ctx, svc.RepoPath, svc.Tag)
	if err != nil {
		return err
	}
	if !exists {
		message, err := m.releaseTagMessageFor(ctx, release, svc)
		if err != nil {
			return err
		}
		if err := m.git.CreateTag(ctx, svc.RepoPath, svc.Tag, svc.AcceptedMergeSHA, message); err != nil {
			return fmt.Errorf("%w: %v", ErrReleaseTagCreateFailed, err)
		}
	}
	tagObjectSHA, err := m.resolveTagObjectSHA(ctx, svc.RepoPath, svc.Tag, svc.AcceptedMergeSHA)
	if err != nil {
		return err
	}
	svc.TagRef, svc.TagSHA = svc.Tag, svc.AcceptedMergeSHA
	if m.cfg.Release != nil && m.cfg.Release.PushTags != nil && *m.cfg.Release.PushTags {
		pushURL, err := m.verifiedRepoPushURL(ctx, svc.Name, svc.RepoPath)
		if err != nil {
			return fmt.Errorf("release finish: verify push destination for service=%s: %w", svc.Name, err)
		}
		if err := m.git.PushTag(ctx, svc.RepoPath, pushURL, svc.Tag, tagObjectSHA); err != nil {
			return fmt.Errorf("%w: %v", ErrReleaseTagPushFailed, err)
		}
		svc.PushedTag = true
	}
	svc.Status = domain.ReleaseStatusReleased
	return nil
}

func releaseTagMessage(releaseID, description string) string {
	if description = strings.TrimSpace(description); description != "" {
		return description
	}
	return "wtui release " + releaseID
}
