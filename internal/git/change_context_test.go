package git

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func changeContextGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	argv := append([]string{"-C", dir, "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "-c", "user.name=Metadata Test", "-c", "user.email=metadata@example.test"}, args...)
	cmd := exec.CommandContext(t.Context(), "git", argv...)
	cmd.Env = append(os.Environ(), "GIT_MASTER=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func changeContextRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	changeContextGit(t, dir, "init", "--initial-branch=develop")
	if err := os.WriteFile(filepath.Join(dir, "api.txt"), []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changeContextGit(t, dir, "add", ".")
	changeContextGit(t, dir, "commit", "-m", "Initial API")
	changeContextGit(t, dir, "checkout", "-b", "feature/APP-1")
	if err := os.WriteFile(filepath.Join(dir, "api.txt"), []byte("original\nsearch endpoint\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changeContextGit(t, dir, "add", ".")
	changeContextGit(t, dir, "commit", "-m", "Add search endpoint")
	return dir
}

func TestReadChanges_UsesCommittedBranchSnapshotWithoutChangingWorktree(t *testing.T) {
	dir := changeContextRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "api.txt"), []byte("private uncommitted edit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("private untracked content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := changeContextGit(t, dir, "status", "--porcelain")
	client := NewCommandClient(slog.Default())

	changes, err := client.ReadChanges(t.Context(), dir, "develop", "feature/APP-1")

	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(changes.Commits, "Add search endpoint") || !strings.Contains(changes.Diff, "+search endpoint") {
		t.Fatalf("change context = %#v", changes)
	}
	if strings.Contains(changes.Diff, "private") || changes.Truncated {
		t.Fatalf("context included local edits or unexpected truncation: %#v", changes)
	}
	if after := changeContextGit(t, dir, "status", "--porcelain"); after != before {
		t.Fatalf("worktree changed: before %q, after %q", before, after)
	}
}

func TestReadChanges_RejectsMissingBranchesAndCancellation(t *testing.T) {
	dir := changeContextRepo(t)
	client := NewCommandClient(slog.Default())
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	for _, tt := range []struct {
		name   string
		ctx    context.Context
		base   string
		source string
	}{
		{"missing target", t.Context(), "missing", "feature/APP-1"},
		{"missing source", t.Context(), "develop", "missing"},
		{"canceled", canceled, "develop", "feature/APP-1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.ReadChanges(tt.ctx, dir, tt.base, tt.source)
			if err == nil {
				t.Fatal("ReadChanges succeeded without a readable branch comparison")
			}
		})
	}
}

func TestReadChanges_BoundsLargeDiffAndDisablesTextConversion(t *testing.T) {
	dir := changeContextRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "api.txt"), []byte(strings.Repeat("large change\n", 12000)), 0o600); err != nil {
		t.Fatal(err)
	}
	changeContextGit(t, dir, "add", ".")
	changeContextGit(t, dir, "commit", "-m", "Large API change")
	marker := filepath.Join(t.TempDir(), "textconv-ran")
	changeContextGit(t, dir, "config", "diff.untrusted.textconv", "touch "+marker)
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("*.txt diff=untrusted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := NewCommandClient(slog.Default())

	changes, err := client.ReadChanges(t.Context(), dir, "develop", "feature/APP-1")

	if err != nil {
		t.Fatal(err)
	}
	if len(changes.Diff) > 64*1024 || !changes.Truncated {
		t.Fatalf("diff size = %d; truncated = %v", len(changes.Diff), changes.Truncated)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("read-only context ran textconv: %v", err)
	}
}
