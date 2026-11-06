package git

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeleteRemoteRefIfUnchanged_UsesCapturedURLAndExactLease(t *testing.T) {
	for _, tc := range []struct {
		name, ref, oid string
	}{
		{"branch", "refs/heads/release/1.2.3", strings.Repeat("a", 40)},
		{"tag", "refs/tags/v1.2.3", strings.Repeat("B", 64)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertGitArgs(t, func(client *CommandClient) error {
				return client.DeleteRemoteRefIfUnchanged(t.Context(), "/repo", "/captured.git", tc.ref, tc.oid)
			}, "-C /repo push --force-with-lease="+tc.ref+":"+tc.oid+" -- /captured.git :"+tc.ref+"\n")
		})
	}
}

func TestDeleteRemoteRefIfUnchanged_RejectsInvalidInputBeforeExecution(t *testing.T) {
	bin := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "args")
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" > \"$GIT_ARGS_FILE\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_ARGS_FILE", argsFile)
	client := NewCommandClient(slog.Default())
	const ref = "refs/heads/release/1.2.3"
	const oid = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, tc := range []struct {
		name, url, ref, oid string
	}{
		{"empty URL", "", ref, oid},
		{"blank URL", " \t ", ref, oid},
		{"option URL", "--receive-pack=evil", ref, oid},
		{"indented option URL", " --receive-pack=evil", ref, oid},
		{"empty ref", "/captured.git", "", oid},
		{"short ref", "/captured.git", "release/1.2.3", oid},
		{"other namespace", "/captured.git", "refs/remotes/origin/release/1.2.3", oid},
		{"empty branch", "/captured.git", "refs/heads/", oid},
		{"empty tag", "/captured.git", "refs/tags/", oid},
		{"traversal", "/captured.git", "refs/heads/../evil", oid},
		{"refspec", "/captured.git", ref + ":refs/heads/main", oid},
		{"peeled tag", "/captured.git", "refs/tags/v1.2.3^{}", oid},
		{"control character", "/captured.git", "refs/heads/a\nb", oid},
		{"empty lease", "/captured.git", ref, ""},
		{"abbreviated OID", "/captured.git", ref, oid[:12]},
		{"invalid OID length", "/captured.git", ref, oid + "a"},
		{"nonhex OID", "/captured.git", ref, strings.Repeat("g", 40)},
		{"OID expression", "/captured.git", ref, oid + "^{commit}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := client.DeleteRemoteRefIfUnchanged(t.Context(), "/repo", tc.url, tc.ref, tc.oid); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
	if _, err := os.Stat(argsFile); !os.IsNotExist(err) {
		t.Fatalf("validation invoked git: args file stat = %v", err)
	}
}
