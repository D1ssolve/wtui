package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

const publishOrigin = "git@github.com:org/repo.git"

type fakeReleasePublisher struct {
	*mockForgeClient
	calls []forge.PublishParams
}

func (f *fakeReleasePublisher) PublishRelease(_ context.Context, p forge.PublishParams) (string, error) {
	f.calls = append(f.calls, p)
	return "https://github.com/org/repo/releases/tag/" + p.Tag, nil
}

func usePublisher(m *manager, gitMock *mockGitClient, remote map[string]string) *fakeReleasePublisher {
	pub := &fakeReleasePublisher{mockForgeClient: &mockForgeClient{}}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitHub: pub}
	gitMock.remoteRefSHAAtFn = func(_, url, ref string) (string, error) {
		if url != publishOrigin {
			return "", fmt.Errorf("read from unverified destination %s", url)
		}
		return remote[ref], nil
	}
	gitMock.tagMessageFn = func(_, object string) (string, error) { return "Release notes of " + object, nil }
	return pub
}

const publishSteps = `    steps:
      - cut_branch: {from: integration}
      - gate: {label: regression}
      - review: {from: release, into: production}
      - back_merge: {from: release, into: integration}
      - tag: {on: accepted_merge}
      - publish: {title: "{{.Service}} {{.Version}}"}
`

var annotatedAtDestination = map[string]string{"refs/tags/v1.2.3": "tag-object-sha", "refs/tags/v1.2.3^{}": "accepted-sha"}

func TestFinalizeRelease_PublishesAfterTagAndStoresURL(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	matchingMaster(gitMock)
	useReleaseMachine(t, m, publishSteps)
	pub := usePublisher(m, gitMock, annotatedAtDestination)
	release := writeRelease(t, m, domain.ReleaseStatusMasterMerged, finalizeService(m))

	got, err := m.FinalizeRelease(t.Context(), FinishReleaseParams{ReleaseID: release.ID})
	if err != nil || got.Status != domain.ReleaseStatusReleased {
		t.Fatalf("FinalizeRelease = %s, %v", got.Status, err)
	}
	want := forge.PublishParams{WorktreePath: filepath.Join(m.cfg.RootDir, "repo-api"), Repo: "github.com/org/repo", Tag: "v1.2.3", Title: "svc-api 1.2.3", Notes: "Release notes of tag-object-sha", CommitSHA: "accepted-sha"}
	if len(pub.calls) != 1 || pub.calls[0] != want || gitMock.pushTagCalls != 1 {
		t.Fatalf("publish calls = %+v, pushes = %d", pub.calls, gitMock.pushTagCalls)
	}
	stored, err := m.GetRelease(t.Context(), release.ID)
	if err != nil || stored.Services[0].ReleaseURL != "https://github.com/org/repo/releases/tag/v1.2.3" {
		t.Fatalf("stored release url = %q, %v", stored.Services[0].ReleaseURL, err)
	}
	if err := m.publishReleases(t.Context(), &stored, gitflow.Step{Kind: gitflow.StepPublish, Notes: gitflow.PublishNotesChangelog}, nil); err != nil || len(pub.calls) != 1 {
		t.Fatalf("a recorded URL must not publish again: calls = %d, err = %v", len(pub.calls), err)
	}
}

func TestPublishReleases_BindsDestinationTagToAcceptedCommit(t *testing.T) {
	lightweight := func(g *mockGitClient) {
		g.resolveRefFn = func(_, ref string) (string, error) {
			return map[string]string{"refs/tags/v1.2.3": "accepted-sha", "accepted-sha^{commit}": "accepted-sha"}[ref], nil
		}
	}
	for _, tc := range []struct {
		name      string
		remote    map[string]string
		notes     string
		local     func(*mockGitClient)
		svc       func(*domain.ReleaseService)
		wantNotes string
		wantErr   string
		unsafe    bool
	}{
		{name: "annotated tag object peels to the accepted commit", remote: annotatedAtDestination, wantNotes: "Release notes of tag-object-sha"},
		{name: "tag replaced by another annotated object", remote: map[string]string{"refs/tags/v1.2.3": "other-object", "refs/tags/v1.2.3^{}": "accepted-sha"}, wantErr: "local tag object is tag-object-sha", unsafe: true},
		{name: "annotated object without a peeled line", remote: map[string]string{"refs/tags/v1.2.3": "tag-object-sha"}, wantErr: `peels to ""`, unsafe: true},
		{name: "lightweight at destination for a local annotated tag", remote: map[string]string{"refs/tags/v1.2.3": "accepted-sha"}, wantErr: "local tag object is tag-object-sha", unsafe: true},
		{name: "tag missing at destination is never pushed", remote: map[string]string{}, wantErr: "publish never creates or pushes tags"},
		{name: "lightweight tag publishes the description", remote: map[string]string{"refs/tags/v1.2.3": "accepted-sha"}, notes: gitflow.PublishNotesDescription, local: lightweight,
			svc: func(s *domain.ReleaseService) { s.TagDescription = "Hand-written notes" }, wantNotes: "Hand-written notes"},
		{name: "lightweight tag has no changelog", remote: map[string]string{"refs/tags/v1.2.3": "accepted-sha"}, local: lightweight, wantErr: "changelog needs an annotated tag"},
		{name: "reviewed in another project", remote: annotatedAtDestination, svc: func(s *domain.ReleaseService) {
			s.ProductionMR = &domain.ProductionMRRef{Repo: "github.com/other/repo"}
		}, wantErr: "was reviewed in github.com/other/repo", unsafe: true},
		{name: "https destination on a non-default port", remote: annotatedAtDestination, local: func(g *mockGitClient) {
			g.remoteURLRes, g.pushURLRes = "https://github.com:8443/org/repo.git", "https://github.com:8443/org/repo.git"
		}, wantErr: "non-default port"},
		{name: "ssh destination with a port", remote: annotatedAtDestination, local: func(g *mockGitClient) {
			g.remoteURLRes, g.pushURLRes = "ssh://git@github.com:2222/org/repo.git", "ssh://git@github.com:2222/org/repo.git"
		}, wantErr: "non-default port"},
		{name: "push destination differs from the fetched project", remote: annotatedAtDestination, local: func(g *mockGitClient) { g.pushURLRes = "git@github.com:org/fork.git" }, wantErr: "push URL differs from fetch URL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, gitMock := newFinishTestManager(t)
			matchingMaster(gitMock)
			if tc.local != nil {
				tc.local(gitMock)
			}
			pub := usePublisher(m, gitMock, tc.remote)
			svc := finalizeService(m)
			if tc.svc != nil {
				tc.svc(&svc)
			}
			release := writeRelease(t, m, domain.ReleaseStatusTagging, svc)
			notes := tc.notes
			if notes == "" {
				notes = gitflow.PublishNotesChangelog
			}

			err := m.publishReleases(t.Context(), &release, gitflow.Step{Kind: gitflow.StepPublish, Notes: notes}, nil)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) || errors.Is(err, ErrReleaseRetryUnsafe) != tc.unsafe {
					t.Fatalf("err = %v, want %q (unsafe=%v)", err, tc.wantErr, tc.unsafe)
				}
				if len(pub.calls) != 0 || release.Services[0].ReleaseURL != "" {
					t.Fatalf("refused publish wrote to the forge: %+v", pub.calls)
				}
				return
			}
			if err != nil || len(pub.calls) != 1 || pub.calls[0].Notes != tc.wantNotes || pub.calls[0].Title != "v1.2.3" {
				t.Fatalf("calls = %+v, err = %v", pub.calls, err)
			}
		})
	}
}

func TestPublishReleases_VerifiesEveryServiceBeforeAnyWrite(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	matchingMaster(gitMock)
	pub := usePublisher(m, gitMock, nil)
	web := finalizeService(m)
	web.Name, web.RepoPath = "svc-web", filepath.Join(m.cfg.RootDir, "repo-web")
	gitMock.remoteRefSHAAtFn = func(repo, _, ref string) (string, error) {
		if repo == web.RepoPath {
			return "", nil
		}
		return annotatedAtDestination[ref], nil
	}
	release := writeRelease(t, m, domain.ReleaseStatusTagging, finalizeService(m))
	release.Services = append(release.Services, web)

	err := m.publishReleases(t.Context(), &release, gitflow.Step{Kind: gitflow.StepPublish, Notes: gitflow.PublishNotesChangelog}, nil)
	if err == nil || !strings.Contains(err.Error(), "svc-web") || len(pub.calls) != 0 {
		t.Fatalf("err = %v, calls = %+v: the first service must not publish before the second verifies", err, pub.calls)
	}
}

func TestRetryRelease_LegacyFinishPublishesAfterTag(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	matchingMaster(gitMock)
	gitMock.tagExistsRes = true
	useReleaseMachine(t, m, publishSteps)
	pub := usePublisher(m, gitMock, annotatedAtDestination)
	preparedAt := time.Date(2026, 6, 16, 12, 30, 0, 0, time.UTC)
	svc := finalizeService(m)
	svc.Tag, svc.Status, svc.PushedTag = "v1.2.3", domain.ReleaseStatusFailed, true
	// A version 1 manifest without a fingerprint, as older binaries wrote it, retries outside the machine.
	data, _ := json.Marshal(domain.Release{
		ManifestVersion: 1, ID: "rel-legacy-1", Status: domain.ReleaseStatusFailed, Checkpoint: "push_tag", PreparedAt: &preparedAt, TaskIDs: []string{"FIN-1"},
		Services: []domain.ReleaseService{svc}, Error: &domain.ReleaseError{Code: "ERR_RELEASE_PUSH_FAILED", Recoverable: true}, CreatedAt: preparedAt, UpdatedAt: preparedAt,
	})
	release := domain.Release{ID: "rel-legacy-1"}
	if err := os.MkdirAll(filepath.Dir(m.releaseManifestPath(release.ID)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.releaseManifestPath(release.ID), data, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := m.RetryRelease(t.Context(), release.ID)
	if err != nil || got.Status != domain.ReleaseStatusReleased || len(pub.calls) != 1 || got.Services[0].ReleaseURL == "" {
		t.Fatalf("retry = %s, url %q, calls %d, err %v", got.Status, got.Services[0].ReleaseURL, len(pub.calls), err)
	}
}
