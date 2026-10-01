package git

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const ensureCommitTestSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestEnsureCommit_RejectsNonSHAInput(t *testing.T) {
	t.Parallel()
	client := NewCommandClient(slog.Default())
	for _, sha := range []string{"", "not-a-sha", "-c", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaag"} {
		if err := client.EnsureCommit(t.Context(), "/repo", sha); err == nil {
			t.Fatalf("EnsureCommit(%q) succeeded, want input rejection", sha)
		}
	}
}

func TestEnsureCommit_FetchesOnlyMissingObjectWithoutMovingRefs(t *testing.T) {
	binDir := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "git-args")
	fakeGit := filepath.Join(binDir, "git")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$GIT_ARGS_FILE"
if [ "$3 $4" = "cat-file -e" ]; then
	exit 1
fi
exit 0
`
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_ARGS_FILE", argsFile)

	if err := NewCommandClient(slog.Default()).EnsureCommit(t.Context(), "/repo", ensureCommitTestSHA); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(argsFile)
	want := "-C /repo cat-file -e " + ensureCommitTestSHA + "^{commit}\n" +
		"-C /repo fetch --no-tags origin " + ensureCommitTestSHA + "\n"
	if string(args) != want {
		t.Fatalf("args = %q, want %q", args, want)
	}
}

func TestEnsureCommit_LocalObjectSkipsFetch(t *testing.T) {
	binDir := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "git-args")
	fakeGit := filepath.Join(binDir, "git")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$GIT_ARGS_FILE"
exit 0
`
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_ARGS_FILE", argsFile)

	if err := NewCommandClient(slog.Default()).EnsureCommit(t.Context(), "/repo", ensureCommitTestSHA); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(argsFile)
	want := "-C /repo cat-file -e " + ensureCommitTestSHA + "^{commit}\n"
	if string(args) != want {
		t.Fatalf("args = %q, want %q", args, want)
	}
}

func TestEnsureCommit_RealBareRemoteFetchesObjectWithoutRefMovement(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	root := t.TempDir()
	remote := filepath.Join(root, "origin.git")
	repo := filepath.Join(root, "repo")
	runEnsureCommitGit(t, root, "init", "--bare", remote)
	runEnsureCommitGit(t, remote, "config", "uploadpack.allowAnySHA1InWant", "true")

	seed := filepath.Join(root, "seed")
	runEnsureCommitGit(t, root, "clone", remote, seed)
	ensureCommitCommitFile(t, seed, "one.txt")
	firstSHA := ensureCommitHeadSHA(t, seed)
	runEnsureCommitGit(t, seed, "push", "origin", "HEAD:master")

	runEnsureCommitGit(t, root, "clone", remote, repo)
	client := NewCommandClient(slog.Default())

	ensureCommitCommitFile(t, seed, "two.txt")
	secondSHA := ensureCommitHeadSHA(t, seed)
	runEnsureCommitGit(t, seed, "push", "origin", "HEAD:master")

	if err := client.EnsureCommit(t.Context(), repo, secondSHA); err != nil {
		t.Fatalf("EnsureCommit() = %v", err)
	}
	if out := strings.TrimSpace(string(ensureCommitGitOutput(t, repo, "cat-file", "-t", secondSHA))); out != "commit" {
		t.Fatalf("fetched object type = %q", out)
	}
	if got := ensureCommitHeadSHA(t, repo); got != firstSHA {
		t.Fatalf("local master moved: %s, want %s", got, firstSHA)
	}

	missing := strings.Repeat("f", 40)
	if err := client.EnsureCommit(t.Context(), repo, missing); err == nil {
		t.Fatal("EnsureCommit() succeeded for absent remote object, want fail-closed error")
	}
}

func ensureCommitCommitFile(t *testing.T, repo, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(name), 0o600); err != nil {
		t.Fatal(err)
	}
	runEnsureCommitGit(t, repo, "add", name)
	runEnsureCommitGit(t, repo, "-c", "user.email=test@example.com", "-c", "user.name=Test User", "commit", "-m", "add "+name)
}

func ensureCommitHeadSHA(t *testing.T, repo string) string {
	t.Helper()
	return strings.TrimSpace(string(ensureCommitGitOutput(t, repo, "rev-parse", "HEAD")))
}

func runEnsureCommitGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func ensureCommitGitOutput(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}
