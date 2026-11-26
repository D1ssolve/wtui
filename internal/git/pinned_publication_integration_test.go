//go:build integration

package git

import (
	"log/slog"
	"path/filepath"
	"testing"
)

func TestGuardedPublicationUsesApprovedURL(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	approved := filepath.Join(root, "approved.git ")
	other := filepath.Join(root, "other.git")
	mustGit(t, root, "init", "-b", "main", repo)
	mustGit(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "base")
	mustGit(t, root, "init", "--bare", approved)
	mustGit(t, repo, "push", approved, "HEAD:refs/heads/main")
	mustGit(t, root, "clone", "--bare", approved, other)
	base := mergeGit(t, repo, "rev-parse", "HEAD")
	mustGit(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "change")
	tip := mergeGit(t, repo, "rev-parse", "HEAD")
	mustGit(t, repo, "config", "url."+other+".pushInsteadOf", approved)
	client := NewCommandClient(slog.Default())
	ref := "refs/heads/main"
	if err := client.PushRefWithLease(t.Context(), repo, approved, ref, tip, ref, base); err != nil {
		t.Fatal(err)
	}
	if err := client.PushTag(t.Context(), repo, approved, "v1", tip); err != nil {
		t.Fatal(err)
	}
	if got := mergeGit(t, approved, "rev-parse", ref); got != tip {
		t.Errorf("approved tip=%s, want %s", got, tip)
	}
	if got := mergeGit(t, other, "rev-parse", ref); got != base {
		t.Errorf("replacement tip=%s, want %s", got, base)
	}
	if got := mergeGit(t, other, "tag", "--list"); got != "" {
		t.Errorf("replacement tags=%s", got)
	}
	if got := mergeGit(t, approved, "tag", "--list"); got != "v1" {
		t.Errorf("approved tags=%s", got)
	}
}
