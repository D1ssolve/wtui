//go:build integration

package git

import (
	"log/slog"
	"path/filepath"
	"testing"
)

func TestDeleteRemoteRefIfUnchanged_ExactLease_Integration(t *testing.T) {
	t.Setenv("GIT_ALLOW_PROTOCOL", "file")
	for _, tc := range []struct {
		name, ref string
		move      bool
		peeled    bool
	}{
		{name: "branch deletion", ref: "refs/heads/release/1.2.3"},
		{name: "moved branch preserved", ref: "refs/heads/release/1.2.3", move: true},
		{name: "annotated tag deletion", ref: "refs/tags/v1.2.3"},
		{name: "moved annotated tag preserved", ref: "refs/tags/v1.2.3", move: true},
		{name: "peeled tag OID rejected", ref: "refs/tags/v1.2.3", peeled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			repo := filepath.Join(root, "repo")
			remote := filepath.Join(root, "origin.git")
			changedOrigin := filepath.Join(root, "changed-origin.git")
			changedPushURL := filepath.Join(root, "changed-pushurl.git")
			for _, bare := range []string{remote, changedOrigin, changedPushURL} {
				mustGit(t, root, "init", "--bare", bare)
			}
			mustGit(t, root, "init", "-b", "main", repo)
			mustGit(t, repo, "config", "user.name", "Test User")
			mustGit(t, repo, "config", "user.email", "test@example.com")
			mustGit(t, repo, "config", "commit.gpgSign", "false")
			mustGit(t, repo, "config", "tag.gpgSign", "false")
			mustGit(t, repo, "commit", "--allow-empty", "-m", "initial")
			if tc.ref == "refs/tags/v1.2.3" {
				mustGit(t, repo, "tag", "-a", "-m", "release", "v1.2.3")
			} else {
				mustGit(t, repo, "branch", "release/1.2.3")
			}
			capturedOID := mergeGit(t, repo, "rev-parse", tc.ref)
			for _, bare := range []string{remote, changedOrigin, changedPushURL} {
				mustGit(t, repo, "push", bare, tc.ref+":"+tc.ref)
			}
			mustGit(t, repo, "remote", "add", "origin", remote)
			client := NewCommandClient(slog.Default())
			capturedURL, err := client.PushURL(t.Context(), repo, "origin")
			if err != nil {
				t.Fatal(err)
			}
			mustGit(t, repo, "remote", "set-url", "origin", changedOrigin)
			mustGit(t, repo, "remote", "set-url", "--push", "origin", changedPushURL)

			expectedOID := capturedOID
			if tc.move {
				mustGit(t, repo, "commit", "--allow-empty", "-m", "concurrent change")
				if tc.ref == "refs/tags/v1.2.3" {
					mustGit(t, repo, "tag", "-f", "-a", "-m", "replacement", "v1.2.3")
				} else {
					mustGit(t, repo, "branch", "-f", "release/1.2.3", "HEAD")
				}
				expectedOID = mergeGit(t, repo, "rev-parse", tc.ref)
				mustGit(t, repo, "push", "--force", remote, tc.ref+":"+tc.ref)
			}
			leaseOID := capturedOID
			if tc.peeled {
				leaseOID = mergeGit(t, repo, "rev-parse", tc.ref+"^{}")
				if leaseOID == capturedOID {
					t.Fatal("fixture must use an annotated tag object distinct from its commit")
				}
			}

			err = client.DeleteRemoteRefIfUnchanged(t.Context(), repo, capturedURL, tc.ref, leaseOID)

			wantErr := tc.move || tc.peeled
			if (err != nil) != wantErr {
				t.Fatalf("delete error = %v, want error = %v", err, wantErr)
			}
			got, lookupErr := client.RemoteRefSHAAt(t.Context(), repo, remote, tc.ref)
			if !wantErr {
				expectedOID = ""
			}
			if lookupErr != nil || got != expectedOID {
				t.Fatalf("captured remote ref = %q, %v; want %q", got, lookupErr, expectedOID)
			}
			for _, bare := range []string{changedOrigin, changedPushURL} {
				if got := mergeGit(t, bare, "rev-parse", tc.ref); got != capturedOID {
					t.Fatalf("changed destination %s ref = %q, want %q", bare, got, capturedOID)
				}
			}
			localOID := capturedOID
			if tc.move {
				localOID = expectedOID
			}
			if got := mergeGit(t, repo, "rev-parse", tc.ref); got != localOID {
				t.Fatalf("local ref = %q, want %q", got, localOID)
			}
		})
	}
}
