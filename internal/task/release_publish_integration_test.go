//go:build integration

package task

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/git"
)

const publishForgeURL = "git@github.com:org/svc-api.git"

// publishIntegrationGit routes the forge URL to the disposable bare origin for tag pushes too.
type publishIntegrationGit struct{ *forgeRemoteURLClient }

func (c *publishIntegrationGit) PushTag(ctx context.Context, repo, url, tag, object string) error {
	if url == c.url {
		url = filepath.Join(filepath.Dir(repo), "origin.git")
	}
	return c.forgeRemoteURLClient.PushTag(ctx, repo, url, tag, object)
}

func newPublishIntegrationEnv(t *testing.T) (releaseIntegrationEnv, *fakeReleasePublisher) {
	t.Helper()
	env := newReleaseIntegrationEnvWithGitClient(t, &publishIntegrationGit{&forgeRemoteURLClient{
		Client: &integrationGitClient{Client: git.NewCommandClient(newIntegrationLogger())}, url: publishForgeURL,
	}})
	useReleaseMachine(t, env.manager, publishSteps)
	pub := &fakeReleasePublisher{mockForgeClient: &mockForgeClient{}}
	env.manager.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitHub: pub}
	return env, pub
}

func preparePublishRelease(t *testing.T, env releaseIntegrationEnv) domain.Release {
	t.Helper()
	env.addFeatureTask(t, "APP-1", func(worktreePath string) {
		writeFile(t, filepath.Join(worktreePath, "feature.txt"), "feature\n")
		mustGit(t, worktreePath, "add", "feature.txt")
		mustGit(t, worktreePath, "commit", "-m", "feat(APP-1): add feature")
	})
	release, err := env.manager.CreateRelease(t.Context(), CreateReleaseParams{TaskIDs: []string{"APP-1"}, ServiceVersions: map[string]string{"svc-api": "1.2.3"}, StartImmediately: true})
	if err != nil {
		t.Fatalf("CreateRelease: %v", err)
	}
	return env.markReleaseMasterMerged(t, release)
}

func TestIntegration_ReleasePublish_TagPushedThenPublishedOnce(t *testing.T) {
	env, pub := newPublishIntegrationEnv(t)
	release := preparePublishRelease(t, env)

	got, err := env.manager.FinalizeRelease(t.Context(), FinishReleaseParams{ReleaseID: release.ID})
	if err != nil || got.Status != domain.ReleaseStatusReleased {
		t.Fatalf("FinalizeRelease = %s, %v", got.Status, err)
	}
	origin := filepath.Join(filepath.Dir(env.repoPath), "origin.git")
	accepted := got.Services[0].AcceptedMergeSHA
	if gitOutput(t, origin, "cat-file", "-t", "refs/tags/v1.2.3") != "tag" || gitOutput(t, origin, "rev-parse", "refs/tags/v1.2.3^{}") != accepted {
		t.Fatal("the destination must hold the annotated tag peeling to the accepted merge")
	}
	notes := gitOutput(t, env.repoPath, "tag", "-l", "--format=%(contents)", "v1.2.3")
	if len(pub.calls) != 1 || pub.calls[0].Repo != "github.com/org/svc-api" || pub.calls[0].CommitSHA != accepted ||
		pub.calls[0].Title != "svc-api 1.2.3" || pub.calls[0].Notes != strings.TrimSpace(notes) {
		t.Fatalf("publish calls = %+v, tag notes %q", pub.calls, notes)
	}
	stored, err := env.manager.GetRelease(t.Context(), release.ID)
	if err != nil || stored.Services[0].ReleaseURL != "https://github.com/org/repo/releases/tag/v1.2.3" {
		t.Fatalf("stored url = %q, %v", stored.Services[0].ReleaseURL, err)
	}
}

func TestIntegration_ReleasePublish_UnpushedTagIsNeverPushedByPublish(t *testing.T) {
	env, pub := newPublishIntegrationEnv(t)
	*env.manager.cfg.Release.PushTags = false
	release := preparePublishRelease(t, env)

	_, err := env.manager.FinalizeRelease(t.Context(), FinishReleaseParams{ReleaseID: release.ID})
	origin := filepath.Join(filepath.Dir(env.repoPath), "origin.git")
	if err == nil || !strings.Contains(err.Error(), "publish never creates or pushes tags") || len(pub.calls) != 0 || gitOutput(t, origin, "tag", "-l") != "" {
		t.Fatalf("err = %v, calls = %d, origin tags %q", err, len(pub.calls), gitOutput(t, origin, "tag", "-l"))
	}

	mustGit(t, env.repoPath, "push", "origin", "refs/tags/v1.2.3")
	got, err := env.manager.RetryRelease(t.Context(), release.ID)
	if err != nil || got.Status != domain.ReleaseStatusReleased || len(pub.calls) != 1 {
		t.Fatalf("retry after the user pushed the tag = %s, calls %d, %v", got.Status, len(pub.calls), err)
	}
}

func TestIntegration_ReleasePublish_DestinationTagKinds(t *testing.T) {
	env, _ := newPublishIntegrationEnv(t)
	commit := gitOutput(t, env.repoPath, "rev-parse", "master")
	mustGit(t, env.repoPath, "tag", "v9.0.0", commit)
	mustGit(t, env.repoPath, "tag", "-a", "-m", "accepted notes", "v9.0.1", commit)
	mustGit(t, env.repoPath, "tag", "-a", "-m", "other notes", "other", commit)
	mustGit(t, env.repoPath, "push", "origin", "refs/tags/v9.0.0")
	mustGit(t, env.repoPath, "push", "origin", gitOutput(t, env.repoPath, "rev-parse", "refs/tags/other")+":refs/tags/v9.0.1")

	svc := domain.ReleaseService{Name: "svc-api", RepoPath: env.repoPath, AcceptedMergeSHA: commit}
	svc.Tag = "v9.0.0"
	if object, err := env.manager.verifyRemoteReleaseTag(t.Context(), svc, publishForgeURL); err != nil || object != commit {
		t.Fatalf("lightweight tag without a peeled line: object %q, %v", object, err)
	}
	svc.Tag = "v9.0.1"
	if _, err := env.manager.verifyRemoteReleaseTag(t.Context(), svc, publishForgeURL); !errors.Is(err, ErrReleaseRetryUnsafe) {
		t.Fatalf("another annotated object peeling to the same commit must be refused: %v", err)
	}
}
