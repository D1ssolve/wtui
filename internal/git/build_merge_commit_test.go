package git

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildMergeCommit_RejectsInvalidInputsBeforeGit(t *testing.T) {
	bin := t.TempDir()
	marker := filepath.Join(t.TempDir(), "called")
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\ntouch \"$GIT_MARKER\"\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_MARKER", marker)
	client := NewCommandClient(slog.Default())
	for _, tc := range []struct{ target, head, method string }{
		{"HEAD", ensureCommitTestSHA, "merge"},
		{ensureCommitTestSHA, "feature", "merge"},
		{ensureCommitTestSHA[:39], ensureCommitTestSHA, ""},
		{ensureCommitTestSHA, strings.Repeat("g", 40), "squash"},
		{ensureCommitTestSHA, ensureCommitTestSHA, "invalid"},
	} {
		if sha, err := client.BuildMergeCommit(t.Context(), "/repo", tc.target, tc.head, tc.method); err == nil || sha != "" {
			t.Fatalf("BuildMergeCommit(%+v) = %q, %v", tc, sha, err)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("git was called for invalid inputs: %v", err)
	}
}

func TestBuildMergeCommit_ReportsOperationAndCleanupErrors(t *testing.T) {
	bin := t.TempDir()
	script := `#!/bin/sh
case "$3 $4" in
"cat-file -t") echo commit ;;
"worktree add") exit 0 ;;
"worktree remove") echo cleanup-failed >&2; exit 1 ;;
*) echo operation-failed >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	sha, err := NewCommandClient(slog.Default()).BuildMergeCommit(t.Context(), "/repo", ensureCommitTestSHA, ensureCommitTestSHA, "merge")
	if sha != "" || err == nil || !strings.Contains(err.Error(), "operation-failed") || !strings.Contains(err.Error(), "cleanup-failed") {
		t.Fatalf("BuildMergeCommit() = %q, %v, want both failures", sha, err)
	}
}
