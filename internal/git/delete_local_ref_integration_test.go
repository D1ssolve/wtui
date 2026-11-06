//go:build integration

package git

import (
	"log/slog"
	"testing"
)

func TestDeleteLocalReleaseRef_SymbolicTargetPreserved_Integration(t *testing.T) {
	for _, ref := range []string{"refs/heads/release/1.2.3", "refs/tags/v1.2.3"} {
		t.Run(ref, func(t *testing.T) {
			repo := t.TempDir()
			mustGit(t, repo, "init", "-b", "main")
			mustGit(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "initial")
			mustGit(t, repo, "symbolic-ref", ref, "refs/heads/main")
			client := NewCommandClient(slog.Default())
			oid, err := client.ResolveRef(t.Context(), repo, "refs/heads/main")
			if err != nil {
				t.Fatal(err)
			}
			if ref == "refs/tags/v1.2.3" {
				err = client.DeleteTagIfUnchanged(t.Context(), repo, "v1.2.3", oid)
			} else {
				err = client.DeleteBranchIfUnchanged(t.Context(), repo, "release/1.2.3", oid)
			}
			if err != nil {
				t.Fatal(err)
			}
			if got, err := client.ResolveRef(t.Context(), repo, "refs/heads/main"); err != nil || got != oid {
				t.Fatalf("deleting release alias changed main: %q, %v", got, err)
			}
		})
	}
}
