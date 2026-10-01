package git

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// assertGuardedDelete runs invoke against a fake git that records argv and
// stdin, then asserts both.
func assertGuardedDelete(t *testing.T, invoke func(*CommandClient) error, wantArgs, wantStdin string) {
	t.Helper()
	binDir := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "git-args")
	stdinFile := filepath.Join(t.TempDir(), "git-stdin")
	fakeGit := filepath.Join(binDir, "git")
	script := `#!/bin/sh
printf '%s\n' "$*" > "$GIT_ARGS_FILE"
cat > "$GIT_STDIN_FILE"
case "$*" in
	*symbolic-ref*) exit 1 ;;
esac
exit 0
`
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_ARGS_FILE", argsFile)
	t.Setenv("GIT_STDIN_FILE", stdinFile)
	if err := invoke(NewCommandClient(slog.Default())); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(argsFile)
	if string(args) != wantArgs {
		t.Fatalf("args = %q, want %q", args, wantArgs)
	}
	stdin, _ := os.ReadFile(stdinFile)
	if string(stdin) != wantStdin {
		t.Fatalf("stdin = %q, want %q", stdin, wantStdin)
	}
}

func TestDeleteBranchIfUnchanged_GuardedUsesStdinTransaction(t *testing.T) {
	assertGuardedDelete(t, func(client *CommandClient) error {
		return client.DeleteBranchIfUnchanged(t.Context(), "/repo", "feature/ABC-1",
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			RefGuard{Ref: "refs/remotes/origin/develop", OID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"})
	},
		"-C /repo update-ref --stdin\n",
		"verify refs/remotes/origin/develop bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\ndelete refs/heads/feature/ABC-1 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n")
}

func TestDeleteBranchIfUnchanged_MultipleGuardsVerifyAllBeforeDelete(t *testing.T) {
	assertGuardedDelete(t, func(client *CommandClient) error {
		return client.DeleteBranchIfUnchanged(t.Context(), "/repo", "feature/ABC-1",
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			RefGuard{Ref: "refs/remotes/origin/develop", OID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			RefGuard{Ref: "refs/remotes/origin/master", OID: "cccccccccccccccccccccccccccccccccccccccc"})
	},
		"-C /repo update-ref --stdin\n",
		"verify refs/remotes/origin/develop bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n"+
			"verify refs/remotes/origin/master cccccccccccccccccccccccccccccccccccccccc\n"+
			"delete refs/heads/feature/ABC-1 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n")
}

func TestDeleteBranchIfUnchanged_RejectsInvalidGuardRefs(t *testing.T) {
	client := NewCommandClient(slog.Default())
	sha := strings.Repeat("a", 40)
	oid := strings.Repeat("b", 40)
	for _, ref := range []string{
		"develop",
		"refs/heads/../evil",
		"refs/remotes/origin/feature ABC-1",
		"refs/remotes/origin/lock~1",
		"refs/remotes/origin/x..y",
		"",
	} {
		if err := client.DeleteBranchIfUnchanged(t.Context(), "/repo", "feature/ABC-1", sha, RefGuard{Ref: ref, OID: oid}); err == nil {
			t.Fatalf("guard ref %q accepted", ref)
		}
	}
	if err := client.DeleteBranchIfUnchanged(t.Context(), "/repo", "feature/ABC-1", sha, RefGuard{Ref: "refs/remotes/origin/develop", OID: "notasha"}); err == nil {
		t.Fatal("invalid guard OID accepted")
	}
}

func TestDeleteBranchIfUnchanged_RejectsSymbolicGuardRef(t *testing.T) {
	binDir := t.TempDir()
	fakeGit := filepath.Join(binDir, "git")
	script := `#!/bin/sh
case "$*" in
	*symbolic-ref*) exit 0 ;;
esac
exit 1
`
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	client := NewCommandClient(slog.Default())
	err := client.DeleteBranchIfUnchanged(t.Context(), "/repo", "feature/ABC-1",
		strings.Repeat("a", 40),
		RefGuard{Ref: "refs/remotes/origin/HEAD", OID: strings.Repeat("b", 40)})
	if err == nil || !strings.Contains(err.Error(), "symbolic") {
		t.Fatalf("error = %v, want symbolic ref rejection", err)
	}
}

func TestDeleteBranchIfUnchanged_WithoutGuardsKeepsSingleLeaseCommand(t *testing.T) {
	assertGitArgs(t, func(client *CommandClient) error {
		return client.DeleteBranchIfUnchanged(t.Context(), "/repo", "feature/ABC-1", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	}, "-C /repo update-ref -d refs/heads/feature/ABC-1 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n")
}
