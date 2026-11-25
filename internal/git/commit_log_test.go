package git

import (
	"log/slog"
	"os/exec"
	"slices"
	"testing"
)

func TestCommitLog_ReadsSubjectsAndBodiesInRange(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("commit", "-q", "--allow-empty", "-m", "chore: init")
	run("tag", "v1.0.0")
	run("commit", "-q", "--allow-empty", "-m", "feat(api): export", "-m", "BREAKING CHANGE: new format")
	run("commit", "-q", "--allow-empty", "-m", "fix: crash")

	commits, err := NewCommandClient(slog.Default()).CommitLog(t.Context(), dir, "v1.0.0", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	subjects := make([]string, len(commits))
	for i, c := range commits {
		subjects[i] = c.Subject
	}
	if !slices.Equal(subjects, []string{"fix: crash", "feat(api): export"}) || commits[1].Body != "BREAKING CHANGE: new format" || len(commits[0].SHA) != 40 {
		t.Fatalf("commits = %+v", commits)
	}
	if _, err := NewCommandClient(slog.Default()).CommitLog(t.Context(), dir, "--all", "HEAD"); err == nil {
		t.Fatal("option-like range accepted")
	}
}
