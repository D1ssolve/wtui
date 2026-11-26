//go:build integration

package task

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
	"gopkg.in/yaml.v3"
)

func TestR6V1LocalIntegrationBranchWithSlashCanResume(t *testing.T) {
	env := newReleaseIntegrationEnvWithGitClient(t, &failingCreateBranchClient{Client: &integrationGitClient{Client: git.NewCommandClient(newIntegrationLogger())}, failCount: 1})
	env.addFeatureTask(t, "APP-30", nil)
	m := env.manager
	repo := env.repoPath
	const branch = "team/develop"
	m.flow.IntegrationBranch = branch
	mustGit(t, repo, "branch", branch, "develop")
	mustGit(t, repo, "push", "origin", branch)
	r, err := m.CreateRelease(t.Context(), CreateReleaseParams{TaskIDs: []string{"APP-30"}, ServiceVersions: map[string]string{"svc-api": "1.2.3"}, StartImmediately: true})
	if err == nil || r.Status != domain.ReleaseStatusFailed {
		t.Fatalf("fixture: %s %v", r.Status, err)
	}
	r, err = m.GetRelease(t.Context(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	s := &r.Services[0]
	if got := gitOutput(t, repo, "rev-parse", "refs/heads/"+branch); s.PostIntegrationSHA != got {
		t.Fatalf("pin %s != actual %s", s.PostIntegrationSHA, got)
	}
	// Pre-tracking manifests stored the *local* integration branch, even when its name contained a slash.
	r.ManifestVersion = 1
	r.FlowFingerprint = ""
	r.State = ""
	s.PreIntegrationRef = s.IntegrationBranch
	s.PostIntegrationRef = s.IntegrationBranch
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, m.releaseManifestPath(r.ID), string(b))
	out, err := m.RetryRelease(t.Context(), r.ID)
	if err != nil || out.Status != domain.ReleaseStatusPrepared {
		t.Fatalf("retry %s at %s: status=%s err=%v", s.PostIntegrationRef, s.PostIntegrationSHA, out.Status, err)
	}
}

func TestR6OldTrackingRefWithRemoteNamedRefsRejectsDrift(t *testing.T) {
	env := newReleaseIntegrationEnv(t)
	env.addFeatureTask(t, "APP-30", func(wt string) { mustGit(t, wt, "commit", "--allow-empty", "-m", "included change") })
	repo := env.repoPath
	m := env.manager
	mustGit(t, repo, "remote", "rename", "origin", "refs")
	if err := yaml.Unmarshal([]byte("refs"), &m.cfg.Forge.Remote); err != nil {
		t.Fatal(err)
	}
	g := git.NewRemoteCommandClient(newIntegrationLogger(), m.cfg.ServiceRemote())
	m.git = &failingCreateBranchClient{Client: &integrationGitClient{Client: g}, failCount: 1}
	r, err := m.CreateRelease(t.Context(), CreateReleaseParams{TaskIDs: []string{"APP-30"}, ServiceVersions: map[string]string{"svc-api": "1.2.3"}, StartImmediately: true})
	if err == nil || r.Status != domain.ReleaseStatusFailed {
		t.Fatalf("fixture: %s %v", r.Status, err)
	}
	r, err = m.GetRelease(t.Context(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	s := &r.Services[0]
	saved := s.PostIntegrationSHA
	// Exact bytes stored by the previous short Tracking writer for alias "refs".
	s.PreIntegrationRef = "refs/develop"
	s.PostIntegrationRef = "refs/develop"
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, m.releaseManifestPath(r.ID), string(b))
	mustGit(t, repo, "checkout", "develop")
	mustGit(t, repo, "commit", "--allow-empty", "-m", "later upstream work")
	mustGit(t, repo, "push", "refs", "develop")
	mustGit(t, repo, "checkout", "master")
	bare := gitOutput(t, repo, "remote", "get-url", "refs")
	ref := "refs/heads/" + s.ReleaseBranch
	for _, shadow := range []bool{false, true} {
		if shadow {
			mustGit(t, repo, "branch", "refs/develop", saved)
		}
		if gitOutput(t, bare, "for-each-ref", ref) != "" {
			t.Fatal("release ref already exists before retry")
		}
		out, e := m.RetryRelease(t.Context(), r.ID)
		got := gitOutput(t, bare, "for-each-ref", "--format=%(objectname)", ref)
		if !errors.Is(e, ErrReleaseRetryUnsafe) || got != "" {
			t.Fatalf("shadow=%v old pin=%s actual=%s released=%q status=%s err=%v", shadow, saved, gitOutput(t, bare, "rev-parse", "refs/heads/develop"), got, out.Status, e)
		}
	}
}
