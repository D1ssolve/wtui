package task

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

type pendingPublish struct {
	index     int
	publisher forge.ReleasePublisher
	params    forge.PublishParams
}

// publishReleases creates the forge Release object of every service's existing tag. Every
// service is verified before the first forge write, and a recorded URL is never redone.
func (m *manager) publishReleases(ctx context.Context, release *domain.Release, step gitflow.Step, statusCh chan<- string) error {
	var pending []pendingPublish
	for i := range release.Services {
		svc := &release.Services[i]
		if svc.ReleaseURL != "" {
			sendStatus(statusCh, fmt.Sprintf("[%s][publish] already published: %s", svc.Name, svc.ReleaseURL))
			continue
		}
		p, err := m.preparePublish(ctx, release.ID, *svc, step)
		if err != nil {
			return serviceFailure(svc, err)
		}
		p.index = i
		pending = append(pending, p)
	}
	for _, p := range pending {
		svc := &release.Services[p.index]
		sendStatus(statusCh, fmt.Sprintf("[%s][publish] publishing %s on %s", svc.Name, p.params.Tag, p.params.Repo))
		url, err := p.publisher.PublishRelease(ctx, p.params)
		if err != nil {
			return serviceFailure(svc, fmt.Errorf("publish release %s for service=%s: %w", p.params.Tag, svc.Name, err))
		}
		svc.ReleaseURL = url
		if err := m.persistCheckpoint(release, "publish_release", nil); err != nil {
			return err
		}
	}
	return nil
}

func (m *manager) preparePublish(ctx context.Context, releaseID string, svc domain.ReleaseService, step gitflow.Step) (pendingPublish, error) {
	client, repo, worktreePath, err := m.releaseForgeTarget(ctx, svc)
	if err != nil {
		return pendingPublish{}, err
	}
	publisher, ok := client.(forge.ReleasePublisher)
	if !ok {
		return pendingPublish{}, fmt.Errorf("the %s forge cannot publish releases; remove the publish step", client.Provider())
	}
	pushURL, err := m.verifiedRepoPushURL(ctx, svc.Name, svc.RepoPath)
	if err != nil {
		return pendingPublish{}, err
	}
	if got := forge.ExtractRepoPath(pushURL); got != repo {
		return pendingPublish{}, fmt.Errorf("%w: service=%s pushes to %q, not the project %s", ErrReleaseRetryUnsafe, svc.Name, got, repo)
	}
	if mr := svc.ProductionMR; mr != nil && mr.Repo != "" && !strings.EqualFold(mr.Repo, repo) {
		return pendingPublish{}, fmt.Errorf("%w: service=%s was reviewed in %s, not %s", ErrReleaseRetryUnsafe, svc.Name, mr.Repo, repo)
	}
	object, err := m.verifyRemoteReleaseTag(ctx, svc, pushURL)
	if err != nil {
		return pendingPublish{}, err
	}

	notes := releaseTagMessage(releaseID, svc.TagDescription)
	if step.Notes == gitflow.PublishNotesChangelog {
		if object == svc.AcceptedMergeSHA {
			return pendingPublish{}, fmt.Errorf("publish notes: changelog needs an annotated tag; %s is lightweight (use notes: description)", svc.Tag)
		}
		if notes, err = m.git.TagMessage(ctx, svc.RepoPath, object); err != nil {
			return pendingPublish{}, fmt.Errorf("read tag %s message: %w", svc.Tag, err)
		}
	}
	title, err := gitflow.PublishTitle(step.Title, map[string]string{"Version": svc.Version, "Tag": svc.Tag, "Service": svc.Name, "ReleaseID": releaseID})
	if err != nil || strings.TrimSpace(title) == "" {
		return pendingPublish{}, errors.Join(fmt.Errorf("publish title for %s is empty or invalid", svc.Name), err)
	}
	return pendingPublish{publisher: publisher, params: forge.PublishParams{
		WorktreePath: worktreePath, Repo: repo, Tag: svc.Tag, Title: title, Notes: notes, CommitSHA: svc.AcceptedMergeSHA,
	}}, nil
}

// verifyRemoteReleaseTag proves refs/tags/<tag> at the push destination is exactly the local
// tag object, which peels to the accepted commit, and returns that object. ls-remote prints
// a peeled ^{} line only for an annotated tag: the line must be present and name the
// accepted commit for a tag object, and absent for a lightweight tag naming the commit.
func (m *manager) verifyRemoteReleaseTag(ctx context.Context, svc domain.ReleaseService, pushURL string) (string, error) {
	if svc.AcceptedMergeSHA == "" {
		return "", fmt.Errorf("%w: service=%s has no accepted commit to publish", ErrReleaseRetryUnsafe, svc.Name)
	}
	local, err := m.resolveTagObjectSHA(ctx, svc.RepoPath, svc.Tag, svc.AcceptedMergeSHA)
	if err != nil {
		return "", fmt.Errorf("%w: service=%s: %v", ErrReleaseRetryUnsafe, svc.Name, err)
	}
	ref := "refs/tags/" + svc.Tag
	remote, err := m.git.RemoteRefSHAAt(ctx, svc.RepoPath, pushURL, ref)
	if err != nil {
		return "", fmt.Errorf("read %s at %s: %w", ref, pushURL, err)
	}
	if remote == "" {
		return "", fmt.Errorf("tag %s of service=%s is not on %s; push it (release.push_tags) and retry: publish never creates or pushes tags", svc.Tag, svc.Name, pushURL)
	}
	if remote != local {
		return "", fmt.Errorf("%w: service=%s tag %s at %s is %s, local tag object is %s", ErrReleaseRetryUnsafe, svc.Name, svc.Tag, pushURL, remote, local)
	}
	peeled, err := m.git.RemoteRefSHAAt(ctx, svc.RepoPath, pushURL, ref+"^{}")
	if err != nil {
		return "", fmt.Errorf("read %s^{} at %s: %w", ref, pushURL, err)
	}
	annotated := local != svc.AcceptedMergeSHA
	if annotated && peeled != svc.AcceptedMergeSHA || !annotated && peeled != "" {
		return "", fmt.Errorf("%w: service=%s tag %s at %s peels to %q, want accepted %s", ErrReleaseRetryUnsafe, svc.Name, svc.Tag, pushURL, peeled, svc.AcceptedMergeSHA)
	}
	return local, nil
}
