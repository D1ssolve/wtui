//go:build integration

package task

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
)

// Retry must reject integration drift before publishing a release ref. The old
// and new stored spellings identify the same tracking ref, not a local branch.
func TestR6RetryRejectsDriftBeforePublishingWithStoredTrackingRef(t *testing.T) {
	for _, format := range []string{"previous", "current", "v1"} {
		t.Run(format, func(t *testing.T) {
			env := newReleaseIntegrationEnvWithGitClient(t, &failingCreateBranchClient{Client: &integrationGitClient{Client: git.NewCommandClient(newIntegrationLogger())}, failCount: 1})
			m := env.manager
			env.addFeatureTask(t, "APP-30", func(wt string) {
				writeFile(t, filepath.Join(wt, "included.txt"), "included\n")
				mustGit(t, wt, "add", "included.txt")
				mustGit(t, wt, "commit", "-m", "feature included")
			})
			r, err := m.CreateRelease(t.Context(), CreateReleaseParams{TaskIDs: []string{"APP-30"}, ServiceVersions: map[string]string{"svc-api": "1.2.3"}, StartImmediately: true})
			if err == nil || r.Status != domain.ReleaseStatusFailed {
				t.Fatalf("create fixture: %+v %v", r, err)
			}
			r, err = m.GetRelease(t.Context(), r.ID)
			if err != nil {
				t.Fatal(err)
			}
			s := &r.Services[0]
			saved := s.PostIntegrationSHA
			if saved == "" || saved != gitOutput(t, env.repoPath, "rev-parse", "refs/remotes/origin/develop") {
				t.Fatalf("missing pinned integration tip: %+v", s)
			}
			if format != "current" {
				s.PreIntegrationRef = "origin/develop"
				s.PostIntegrationRef = "origin/develop"
			}
			if format == "v1" {
				r.ManifestVersion = 1
				r.FlowFingerprint = ""
				r.State = ""
			}
			b, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, m.releaseManifestPath(r.ID), string(b))
			mustGit(t, env.repoPath, "checkout", "develop")
			mustGit(t, env.repoPath, "commit", "--allow-empty", "-m", "later upstream work")
			mustGit(t, env.repoPath, "push", "origin", "develop")
			mustGit(t, env.repoPath, "checkout", "master")
			bare := gitOutput(t, env.repoPath, "remote", "get-url", "origin")
			ref := "refs/heads/" + s.ReleaseBranch
			for _, shadow := range []bool{false, true} {
				if shadow {
					mustGit(t, env.repoPath, "branch", "origin/develop", saved)
				}
				if before := gitOutput(t, bare, "for-each-ref", "--format=%(objectname)", ref); before != "" {
					t.Fatalf("release exists before retry: %s", before)
				}
				out, err := m.RetryRelease(t.Context(), r.ID)
				after := gitOutput(t, bare, "for-each-ref", "--format=%(objectname)", ref)
				if !errors.Is(err, ErrReleaseRetryUnsafe) || after != "" {
					t.Fatalf("shadow=%t: want ErrReleaseRetryUnsafe and no %s; status=%q err=%v remote release=%q saved=%s actual integration=%s", shadow, ref, out.Status, err, after, saved, gitOutput(t, bare, "rev-parse", "refs/heads/develop"))
				}
			}
		})
	}
}
