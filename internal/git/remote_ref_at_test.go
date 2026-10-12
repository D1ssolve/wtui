package git

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteRefSHAAt_UsesCapturedURLAndExactRef(t *testing.T) {
	for _, tc := range []struct {
		output, want string
		wantErr      bool
	}{
		{ensureCommitTestSHA + "\trefs/heads/target\n", ensureCommitTestSHA, false},
		{"", "", false},
		{ensureCommitTestSHA + "\trefs/heads/target-other\n", "", true},
		{"invalid\trefs/heads/target\n", "", true},
	} {
		t.Run(tc.output, func(t *testing.T) {
			bin := t.TempDir()
			argsFile := filepath.Join(t.TempDir(), "args")
			if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" > \"$GIT_ARGS_FILE\"\nprintf '%s' \"$GIT_OUTPUT\"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("GIT_ARGS_FILE", argsFile)
			t.Setenv("GIT_OUTPUT", tc.output)
			got, err := NewCommandClient(slog.Default()).RemoteRefSHAAt(t.Context(), "/repo", "/captured.git", "refs/heads/target")
			if got != tc.want || (err != nil) != tc.wantErr {
				t.Fatalf("lookup = %q, %v", got, err)
			}
			args, err := os.ReadFile(argsFile)
			if err != nil || string(args) != "-C /repo ls-remote -- /captured.git refs/heads/target\n" {
				t.Fatalf("args = %q, %v", args, err)
			}
		})
	}
}

func TestRemoteRefSHAAt_RejectsInvalidInput(t *testing.T) {
	client := NewCommandClient(slog.Default())
	for _, pair := range [][2]string{{"", "refs/heads/target"}, {" --upload-pack=evil", "refs/heads/target"}, {"/remote", "target"}, {"/remote", "refs/heads/a..b"}, {"/remote", "refs/heads/a\nb"}} {
		if _, err := client.RemoteRefSHAAt(t.Context(), "/nonexistent", pair[0], pair[1]); err == nil || strings.Contains(err.Error(), "exit") {
			t.Fatalf("invalid lookup %+v: %v", pair, err)
		}
	}
}
