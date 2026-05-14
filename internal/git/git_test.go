package git

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseWorktreeListPorcelain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []WorktreeEntry
	}{
		{
			name:  "empty input returns empty slice",
			input: "",
			want:  nil,
		},
		{
			name:  "whitespace-only input returns empty slice",
			input: "   \n\n  ",
			want:  nil,
		},
		{
			name: "single main worktree with branch",
			input: `worktree /path/to/main
HEAD abc123def456abc123def456abc123def456abc123
branch refs/heads/main

`,
			want: []WorktreeEntry{
				{Path: "/path/to/main", HEAD: "abc123def456abc123def456abc123def456abc123", Branch: "refs/heads/main"},
			},
		},
		{
			name: "detached HEAD worktree",
			input: `worktree /path/to/detached
HEAD deadbeefdeadbeefdeadbeefdeadbeefdeadbeef
detached

`,
			want: []WorktreeEntry{
				{Path: "/path/to/detached", HEAD: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", Branch: "(detached)"},
			},
		},
		{
			name: "multiple worktrees",
			input: `worktree /path/to/main
HEAD aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
branch refs/heads/main

worktree /path/to/feature
HEAD bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
branch refs/heads/feature/IN-6748

worktree /path/to/detached
HEAD cccccccccccccccccccccccccccccccccccccccc
detached

`,
			want: []WorktreeEntry{
				{Path: "/path/to/main", HEAD: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Branch: "refs/heads/main"},
				{Path: "/path/to/feature", HEAD: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Branch: "refs/heads/feature/IN-6748"},
				{Path: "/path/to/detached", HEAD: "cccccccccccccccccccccccccccccccccccccccc", Branch: "(detached)"},
			},
		},
		{
			name: "bare repo annotation is ignored (only worktree/HEAD/branch/detached parsed)",
			input: `worktree /path/to/bare.git
HEAD 0000000000000000000000000000000000000000
bare

`,

			want: []WorktreeEntry{
				{Path: "/path/to/bare.git", HEAD: "0000000000000000000000000000000000000000", Branch: ""},
			},
		},
		{
			name: "no trailing blank line still parses last entry",
			input: `worktree /path/to/main
HEAD aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
branch refs/heads/main`,
			want: []WorktreeEntry{
				{Path: "/path/to/main", HEAD: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Branch: "refs/heads/main"},
			},
		},
		{
			name:  "windows CRLF line endings are handled",
			input: "worktree /path/to/main\r\nHEAD aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\r\nbranch refs/heads/main\r\n\r\n",
			want: []WorktreeEntry{
				{Path: "/path/to/main", HEAD: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Branch: "refs/heads/main"},
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := parseWorktreeListPorcelain(tc.input)
			if !worktreeSlicesEqual(got, tc.want) {
				t.Errorf("parseWorktreeListPorcelain() =\n  %+v\nwant\n  %+v", got, tc.want)
			}
		})
	}
}

func worktreeSlicesEqual(a, b []WorktreeEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestExecError_Error(t *testing.T) {
	t.Parallel()

	e := &ExecError{
		Argv:     []string{"git", "-C", "/repo", "status"},
		ExitCode: 128,
		Stderr:   "fatal: not a git repository\n",
	}
	got := e.Error()
	if !strings.Contains(got, "128") {
		t.Errorf("expected exit code in error message, got: %q", got)
	}
	if !strings.Contains(got, "fatal: not a git repository") {
		t.Errorf("expected stderr in error message, got: %q", got)
	}
}

func TestExecError_ErrorNoStderr(t *testing.T) {
	t.Parallel()

	e := &ExecError{
		Argv:     []string{"git", "status"},
		ExitCode: 1,
		Stderr:   "",
	}
	got := e.Error()
	if !strings.Contains(got, "exit 1") {
		t.Errorf("expected 'exit 1' in error message, got: %q", got)
	}
}

func TestExecError_Is_SentinelCheck(t *testing.T) {
	t.Parallel()

	e := &ExecError{Argv: []string{"git"}, ExitCode: 1, Stderr: ""}

	if !errors.Is(e, ErrExec) {
		t.Error("errors.Is(execErr, ErrExec) should return true")
	}

	wrapped := &wrappedErr{inner: e}
	if !errors.Is(wrapped, ErrExec) {
		t.Error("errors.Is(wrapped execErr, ErrExec) should return true through chain")
	}
}

func TestCommandClient_RemoteBranchExistsUsesLiveRemoteLookup(t *testing.T) {
	binDir := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "git-args")
	fakeGit := filepath.Join(binDir, "git")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$GIT_ARGS_FILE"
exit 0
`
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_ARGS_FILE", argsFile)

	client := NewCommandClient(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	exists, err := client.RemoteBranchExists(t.Context(), "/repo", "feature/ABC-123")
	if err != nil {
		t.Fatalf("RemoteBranchExists returned error: %v", err)
	}
	if !exists {
		t.Fatal("RemoteBranchExists = false, want true")
	}

	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	want := "-C /repo ls-remote --exit-code --heads origin feature/ABC-123\n"
	if string(args) != want {
		t.Fatalf("git args = %q, want %q", string(args), want)
	}
}

func TestCommandClient_RemoteBranchExistsReturnsFalseWhenLiveRemoteHasNoMatch(t *testing.T) {
	binDir := t.TempDir()
	fakeGit := filepath.Join(binDir, "git")
	script := `#!/bin/sh
exit 2
`
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	client := NewCommandClient(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	exists, err := client.RemoteBranchExists(t.Context(), "/repo", "feature/missing")
	if err != nil {
		t.Fatalf("RemoteBranchExists returned error: %v", err)
	}
	if exists {
		t.Fatal("RemoteBranchExists = true, want false")
	}
}

type wrappedErr struct{ inner error }

func (w *wrappedErr) Error() string { return w.inner.Error() }
func (w *wrappedErr) Unwrap() error { return w.inner }
