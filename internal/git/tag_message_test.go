package git

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestTagMessage_ReadsTagObjectWithoutSignature(t *testing.T) {
	bin := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "args")
	script := `#!/bin/sh
printf '%s\n' "$*" > "$GIT_ARGS_FILE"
printf 'object aaaa\ntype commit\ntag v1.2.3\ntagger T <t@example.com> 1 +0000\n\nRelease 1.2.3\n\n## Features\n- one\n-----BEGIN PGP SIGNATURE-----\nxyz\n-----END PGP SIGNATURE-----\n'
`
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_ARGS_FILE", argsFile)
	client := NewCommandClient(slog.Default())

	msg, err := client.TagMessage(t.Context(), "/repo", ensureCommitTestSHA)
	if err != nil || msg != "Release 1.2.3\n\n## Features\n- one" {
		t.Fatalf("message = %q, err = %v", msg, err)
	}
	if args, _ := os.ReadFile(argsFile); string(args) != "-C /repo cat-file tag "+ensureCommitTestSHA+"\n" {
		t.Fatalf("args = %q", args)
	}
	if _, err := client.TagMessage(t.Context(), "/repo", "refs/tags/v1.2.3"); err == nil {
		t.Fatal("a ref name must be rejected: only a verified object id binds the message")
	}
}
