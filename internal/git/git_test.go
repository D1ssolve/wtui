package git

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
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

func TestCommandClient_RepoStatusParsesPorcelainV2(t *testing.T) {
	binDir := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "git-args")
	fakeGit := filepath.Join(binDir, "git")
	script := `#!/bin/sh
if [ "$4" = "status" ]; then
  printf '# branch.head feature/ABC-123\0# branch.upstream origin/feature/ABC-123\0# branch.ab +2 -1\0u UU N... 100644 100644 100644 100644 abc123 def456 fedcba conflict.txt\0? untracked.txt\0'
fi
printf '%s\n' "$*" >> "$GIT_ARGS_FILE"
exit 0
`
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_ARGS_FILE", argsFile)

	client := NewCommandClient(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	status, err := client.RepoStatus(t.Context(), "/repo")
	if err != nil {
		t.Fatalf("RepoStatus returned error: %v", err)
	}

	if status.Branch != "feature/ABC-123" {
		t.Fatalf("Branch = %q, want %q", status.Branch, "feature/ABC-123")
	}
	if status.Upstream != "origin/feature/ABC-123" {
		t.Fatalf("Upstream = %q, want %q", status.Upstream, "origin/feature/ABC-123")
	}
	if status.Ahead != 2 || status.Behind != 1 {
		t.Fatalf("Ahead/Behind = %d/%d, want 2/1", status.Ahead, status.Behind)
	}
	if len(status.ChangedEntries) != 1 {
		t.Fatalf("ChangedEntries len = %d, want 1", len(status.ChangedEntries))
	}
	if status.ChangedEntries[0].XY != "UU" || status.ChangedEntries[0].Path != "conflict.txt" {
		t.Fatalf("first entry = %+v, want XY=UU Path=conflict.txt", status.ChangedEntries[0])
	}
	if len(status.ConflictPaths) != 1 || status.ConflictPaths[0] != "conflict.txt" {
		t.Fatalf("ConflictPaths = %+v, want [conflict.txt]", status.ConflictPaths)
	}
	if len(status.UntrackedPaths) != 1 || status.UntrackedPaths[0] != "untracked.txt" {
		t.Fatalf("UntrackedPaths = %+v, want [untracked.txt]", status.UntrackedPaths)
	}

	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	want := "-C /repo --no-optional-locks status --porcelain=v2 --branch -z\n"
	if string(args) != want {
		t.Fatalf("git args = %q, want %q", string(args), want)
	}
}

func TestCommandClient_RepoStatusContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := NewCommandClient(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	_, err := client.RepoStatus(ctx, "/repo")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RepoStatus error = %v, want context.Canceled", err)
	}
}

func TestCommandClient_OperationStateDetectsInterruptedOps(t *testing.T) {
	binDir := t.TempDir()
	fakeGit := filepath.Join(binDir, "git")
	commonDir := t.TempDir()
	script := `#!/bin/sh
if [ "$4" = "--git-common-dir" ]; then
  printf '%s' "$COMMON_DIR"
  exit 0
fi
exit 1
`
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("COMMON_DIR", commonDir)

	for _, name := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "BISECT_LOG"} {
		if err := os.WriteFile(filepath.Join(commonDir, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := os.Mkdir(filepath.Join(commonDir, "rebase-merge"), 0o755); err != nil {
		t.Fatalf("mkdir rebase-merge: %v", err)
	}
	if err := os.Mkdir(filepath.Join(commonDir, "rebase-apply"), 0o755); err != nil {
		t.Fatalf("mkdir rebase-apply: %v", err)
	}

	client := NewCommandClient(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	states, err := client.OperationState(t.Context(), "/repo")
	if err != nil {
		t.Fatalf("OperationState returned error: %v", err)
	}

	want := []domain.RepoState{
		domain.RepoStateMerging,
		domain.RepoStateRebasing,
		domain.RepoStateCherryPick,
		domain.RepoStateReverting,
		domain.RepoStateBisect,
	}
	for _, state := range want {
		if !slices.Contains(states, state) {
			t.Fatalf("states = %+v, missing %v", states, state)
		}
	}

	rebaseCount := 0
	for _, state := range states {
		if state == domain.RepoStateRebasing {
			rebaseCount++
		}
	}
	if rebaseCount != 1 {
		t.Fatalf("RepoStateRebasing count = %d, want 1", rebaseCount)
	}
}

func TestCommandClient_IsAncestorMapsExitCodes(t *testing.T) {
	tests := []struct {
		name      string
		exitCode  string
		want      bool
		wantError bool
	}{
		{name: "exit 0 means ancestor", exitCode: "0", want: true},
		{name: "exit 1 means not ancestor", exitCode: "1", want: false},
		{name: "exit 2 returns error", exitCode: "2", wantError: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			binDir := t.TempDir()
			fakeGit := filepath.Join(binDir, "git")
			script := `#!/bin/sh
exit "$GIT_EXIT_CODE"
`
			if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
				t.Fatalf("write fake git: %v", err)
			}
			t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("GIT_EXIT_CODE", tc.exitCode)

			client := NewCommandClient(slog.New(slog.NewTextHandler(os.Stderr, nil)))
			got, err := client.IsAncestor(t.Context(), "/repo", "main", "feature")
			if tc.wantError {
				if err == nil {
					t.Fatalf("IsAncestor error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("IsAncestor returned error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("IsAncestor = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCommandClient_CreateTagUsesAnnotatedTag(t *testing.T) {
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
	if err := client.CreateTag(t.Context(), "/repo", "v1.2.0", "main", "Release v1.2.0"); err != nil {
		t.Fatalf("CreateTag returned error: %v", err)
	}

	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	want := "-C /repo tag -a v1.2.0 main -m Release v1.2.0\n"
	if string(args) != want {
		t.Fatalf("git args = %q, want %q", string(args), want)
	}
}

func TestCommandClient_PushTagUsesOrigin(t *testing.T) {
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
	if err := client.PushTag(t.Context(), "/repo", "v1.2.0"); err != nil {
		t.Fatalf("PushTag returned error: %v", err)
	}

	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	want := "-C /repo push origin v1.2.0\n"
	if string(args) != want {
		t.Fatalf("git args = %q, want %q", string(args), want)
	}
}

func TestCommandClient_DeleteTagUsesDeleteFlag(t *testing.T) {
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
	if err := client.DeleteTag(t.Context(), "/repo", "v1.2.0"); err != nil {
		t.Fatalf("DeleteTag returned error: %v", err)
	}

	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	want := "-C /repo tag -d v1.2.0\n"
	if string(args) != want {
		t.Fatalf("git args = %q, want %q", string(args), want)
	}
}

func TestCreateBranchFromBranch(t *testing.T) {
	t.Run("uses expected argv", func(t *testing.T) {
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
		if err := client.CreateBranchFromBranch(t.Context(), "/repo", "release/1.2.0", "feature/ABC-123"); err != nil {
			t.Fatalf("CreateBranchFromBranch returned error: %v", err)
		}

		args, err := os.ReadFile(argsFile)
		if err != nil {
			t.Fatalf("read args file: %v", err)
		}
		want := "-C /repo branch release/1.2.0 feature/ABC-123\n"
		if string(args) != want {
			t.Fatalf("git args = %q, want %q", string(args), want)
		}
	})

	t.Run("returns ExecError on git failure", func(t *testing.T) {
		binDir := t.TempDir()
		fakeGit := filepath.Join(binDir, "git")
		script := `#!/bin/sh
printf '%s' 'fatal: branch creation failed' >&2
exit 128
`
		if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
			t.Fatalf("write fake git: %v", err)
		}
		t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

		client := NewCommandClient(slog.New(slog.NewTextHandler(os.Stderr, nil)))
		err := client.CreateBranchFromBranch(t.Context(), "/repo", "release/1.2.0", "feature/ABC-123")
		if err == nil {
			t.Fatal("CreateBranchFromBranch error = nil, want error")
		}

		var execErr *ExecError
		if !errors.As(err, &execErr) {
			t.Fatalf("CreateBranchFromBranch error = %T, want *ExecError", err)
		}
		if execErr.ExitCode != 128 {
			t.Fatalf("ExitCode = %d, want 128", execErr.ExitCode)
		}
	})
}

func TestPushBranchExplicit(t *testing.T) {
	t.Run("uses expected argv", func(t *testing.T) {
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
		if err := client.PushBranchExplicit(t.Context(), "/worktree", "release/1.2.0"); err != nil {
			t.Fatalf("PushBranchExplicit returned error: %v", err)
		}

		args, err := os.ReadFile(argsFile)
		if err != nil {
			t.Fatalf("read args file: %v", err)
		}
		want := "-C /worktree push -u origin release/1.2.0\n"
		if string(args) != want {
			t.Fatalf("git args = %q, want %q", string(args), want)
		}
	})

	t.Run("returns ExecError on git failure", func(t *testing.T) {
		binDir := t.TempDir()
		fakeGit := filepath.Join(binDir, "git")
		script := `#!/bin/sh
printf '%s' 'fatal: push failed' >&2
exit 1
`
		if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
			t.Fatalf("write fake git: %v", err)
		}
		t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

		client := NewCommandClient(slog.New(slog.NewTextHandler(os.Stderr, nil)))
		err := client.PushBranchExplicit(t.Context(), "/worktree", "release/1.2.0")
		if err == nil {
			t.Fatal("PushBranchExplicit error = nil, want error")
		}

		var execErr *ExecError
		if !errors.As(err, &execErr) {
			t.Fatalf("PushBranchExplicit error = %T, want *ExecError", err)
		}
		if execErr.ExitCode != 1 {
			t.Fatalf("ExitCode = %d, want 1", execErr.ExitCode)
		}
	})
}

func TestCommandClient_ListTagsSortedBySemver(t *testing.T) {
	binDir := t.TempDir()
	fakeGit := filepath.Join(binDir, "git")
	script := "#!/bin/sh\n" +
		"printf 'v1.2.0|abc123|Release 1.2.0|tag\\n'\n" +
		"printf 'other-tag|def456|Other|commit\\n'\n" +
		"printf 'v1.10.0|987aaa|Release 1.10.0|tag\\n'\n" +
		"printf 'v1.3.0|444bbb|Release 1.3.0|tag\\n'\n" +
		"exit 0\n"
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	client := NewCommandClient(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	tags, err := client.ListTags(t.Context(), "/repo")
	if err != nil {
		t.Fatalf("ListTags returned error: %v", err)
	}

	if len(tags) != 4 {
		t.Fatalf("tags len = %d, want 4", len(tags))
	}

	wantOrder := []string{"v1.10.0", "v1.3.0", "v1.2.0", "other-tag"}
	for i, want := range wantOrder {
		if tags[i].Name != want {
			t.Fatalf("tags[%d].Name = %q, want %q", i, tags[i].Name, want)
		}
	}

	if !tags[0].IsSemver || tags[3].IsSemver {
		t.Fatalf("semver flags not parsed as expected: %+v", tags)
	}
	if !tags[0].IsAnnotated || tags[3].IsAnnotated {
		t.Fatalf("annotated flags not parsed as expected: %+v", tags)
	}
}

func TestCommandClient_LatestSemverTag(t *testing.T) {
	t.Run("returns latest", func(t *testing.T) {
		binDir := t.TempDir()
		fakeGit := filepath.Join(binDir, "git")
		script := "#!/bin/sh\nprintf 'v1.2.0\\nbad-tag\\nv1.2.3\\n'\nexit 0\n"
		if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
			t.Fatalf("write fake git: %v", err)
		}
		t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

		client := NewCommandClient(slog.New(slog.NewTextHandler(os.Stderr, nil)))
		tag, err := client.LatestSemverTag(t.Context(), "/repo", "main")
		if err != nil {
			t.Fatalf("LatestSemverTag returned error: %v", err)
		}
		if tag != "v1.2.3" {
			t.Fatalf("LatestSemverTag = %q, want %q", tag, "v1.2.3")
		}
	})

	t.Run("returns empty when no semver", func(t *testing.T) {
		binDir := t.TempDir()
		fakeGit := filepath.Join(binDir, "git")
		script := "#!/bin/sh\nprintf 'foo\\nbar\\n'\nexit 0\n"
		if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
			t.Fatalf("write fake git: %v", err)
		}
		t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

		client := NewCommandClient(slog.New(slog.NewTextHandler(os.Stderr, nil)))
		tag, err := client.LatestSemverTag(t.Context(), "/repo", "main")
		if err != nil {
			t.Fatalf("LatestSemverTag returned error: %v", err)
		}
		if tag != "" {
			t.Fatalf("LatestSemverTag = %q, want empty string", tag)
		}
	})
}
