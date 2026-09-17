//go:build integration

package task

import (
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
)

func TestHotfixTag_RealGitPinsCommitAndRetries(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	origin := filepath.Join(root, "origin.git")
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run(root, "init", "--bare", origin)
	run(root, "init", "-b", "master", repo)
	run(repo, "config", "user.email", "test@example.com")
	run(repo, "config", "user.name", "Test")
	run(repo, "commit", "--allow-empty", "-m", "old master")
	run(repo, "remote", "add", "origin", origin)
	run(repo, "checkout", "-b", "hotfix/H")
	run(repo, "commit", "--allow-empty", "-m", "fix")
	sha := run(repo, "rev-parse", "HEAD")
	run(repo, "push", "origin", "HEAD:master")
	m := &manager{git: git.NewCommandClient(slog.Default())}
	svc := domain.Service{RepoPath: repo, WorktreePath: repo}
	tag := TagPlan{TagName: "v1.2.4", SourceRef: sha, Annotated: true, Push: true, Message: "Hotfix"}
	for range 2 {
		if err := m.ensureHotfixTag(t.Context(), svc, tag); err != nil {
			t.Fatal(err)
		}
	}
	got := run(root, "--git-dir", origin, "rev-parse", "refs/tags/v1.2.4^{}")
	if got != sha {
		t.Fatalf("remote tag=%s expected=%s", got, sha)
	}
	if run(repo, "rev-parse", "master") == sha {
		t.Fatal("fixture must keep local master stale")
	}
	tag.SourceRef = run(repo, "rev-parse", "master")
	if err := m.ensureHotfixTag(t.Context(), svc, tag); err == nil {
		t.Fatal("conflicting tag accepted")
	}
	tag.TagName = "v1.2.5"
	tag.SourceRef = sha
	tag.Annotated = false
	if err := m.ensureHotfixTag(t.Context(), svc, tag); err != nil {
		t.Fatal(err)
	}
	if kind := run(repo, "cat-file", "-t", "v1.2.5"); kind != "commit" {
		t.Fatalf("lightweight tag type=%s", kind)
	}
}
