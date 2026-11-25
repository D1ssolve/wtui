package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

type initEnv struct {
	root, target string
}

func newInitEnv(t *testing.T) initEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("EDITOR", "")
	t.Setenv("WTUI_ROOT", "")
	t.Setenv("WTUI_TASKS_ROOT", "")
	return initEnv{root: t.TempDir(), target: filepath.Join(home, "xdg", "wtui", "config.yaml")}
}

func runInit(c cliArgs, input string, draft draftFunc) (int, string, string) {
	return runInitReader(c, strings.NewReader(input), draft)
}

func runInitReader(c cliArgs, stdin io.Reader, draft draftFunc) (int, string, string) {
	c.init = true
	var stdout, stderr bytes.Buffer
	code := initCommand(c, stdin, &stdout, &stderr, draft)
	return code, stdout.String(), stderr.String()
}

// beforeRead runs hook once, at the first prompt, then serves the scripted answers.
type beforeRead struct {
	hook func()
	r    io.Reader
}

func (b *beforeRead) Read(p []byte) (int, error) {
	if b.hook != nil {
		b.hook()
		b.hook = nil
	}
	return b.r.Read(p)
}

type unreadable struct{ t *testing.T }

func (u unreadable) Read([]byte) (int, error) {
	u.t.Error("--yes read stdin")
	return 0, io.EOF
}

func assertNoTemps(t *testing.T, dir string) {
	t.Helper()
	if temps, _ := filepath.Glob(filepath.Join(dir, ".wtui-init-*")); len(temps) != 0 {
		t.Fatalf("temp files left: %v", temps)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func noDraft(context.Context, string) (string, error) {
	return "", errors.New("unexpected draft call")
}

// loadWritten reads the file back through the loader the TUI uses and returns the raw and resolved views.
func loadWritten(t *testing.T, path string) (*config.Config, string) {
	t.Helper()
	raw, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	effective, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if effective, err = effective.Effective(); err != nil {
		t.Fatal(err)
	}
	flow, err := gitflow.Resolve(effective)
	if err != nil {
		t.Fatal(err)
	}
	return raw, flow.Describe()
}

func assertNothingWritten(t *testing.T, target string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no files in %s, found %v", filepath.Dir(target), entries)
	}
}

func TestInit_InteractiveChoosesEachPreset(t *testing.T) {
	for _, tt := range []struct{ choice, preset string }{{"1", "git-flow"}, {"2", "github-flow"}, {"3", "trunk-based"}, {"trunk-based", "trunk-based"}} {
		t.Run(tt.choice, func(t *testing.T) {
			env := newInitEnv(t)
			code, out, errOut := runInit(cliArgs{}, env.root+"\n\nvim\n"+tt.choice+"\n", noDraft)
			if code != 0 {
				t.Fatalf("code %d, stderr %q", code, errOut)
			}
			raw, describe := loadWritten(t, env.target)
			if raw.RootDir != env.root || raw.TasksRoot != filepath.Join(env.root, ".tasks") || raw.Editor != "vim" || raw.Workflow.Extends != tt.preset {
				t.Fatalf("config = %+v / %+v", raw, raw.Workflow)
			}
			if !strings.Contains(describe, "workflow: "+tt.preset) {
				t.Fatalf("describe = %q", describe)
			}
			if !strings.Contains(out, "Wrote "+env.target) || !strings.Contains(out, "wtui flow") || !strings.Contains(out, "wtui --help") {
				t.Fatalf("stdout = %q", out)
			}
			if info, err := os.Stat(env.target); err != nil || info.Mode().Perm() != 0o644 {
				t.Fatalf("stat = %v, %v", info, err)
			}
		})
	}
}

func TestInit_EmptyLinesAndEndOfInputAcceptDefaults(t *testing.T) {
	for name, input := range map[string]string{"empty lines": "\n\n\n\n", "EOF mid-way": "\n", "no input": ""} {
		t.Run(name, func(t *testing.T) {
			env := newInitEnv(t)
			t.Chdir(env.root)
			t.Setenv("EDITOR", "nano")
			if code, _, errOut := runInit(cliArgs{}, input, noDraft); code != 0 {
				t.Fatalf("code %d, stderr %q", code, errOut)
			}
			raw, _ := loadWritten(t, env.target)
			cwd, _ := os.Getwd()
			if raw.RootDir != cwd || raw.TasksRoot != filepath.Join(cwd, ".tasks") || raw.Editor != "nano" || raw.Workflow.Extends != "git-flow" {
				t.Fatalf("config = %+v / %+v", raw, raw.Workflow)
			}
		})
	}
}

func TestInit_FailsCleanlyWithoutWriting(t *testing.T) {
	failDraft := func(context.Context, string) (string, error) { return "workflow:\n  extends: nope\n", nil }
	tests := []struct {
		name, input, wantErr string
		draft                draftFunc
	}{
		{name: "invalid root three times", input: "/nonexistent/a\n/nonexistent/b\n/nonexistent/c\n", wantErr: "no existing directory given after 3 attempts"},
		{name: "invalid menu choice three times", input: "\n\n\n9\nx\n0\n", wantErr: "no valid workflow choice after 3 attempts"},
		{name: "EOF at AI description", input: "\n\n\n4\n", wantErr: "input ended before"},
		{name: "line over the limit", input: strings.Repeat("a", 1<<20) + "\n", wantErr: "longer than"},
		{name: "binary junk", input: "\x00\x01\xff\xfe\n", wantErr: "control characters or invalid UTF-8"},
		{name: "validation rejects the draft", input: "\n\n\n4\nwe ship from main\n", draft: failDraft, wantErr: "nothing was written: workflow.extends: unknown preset"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newInitEnv(t)
			t.Chdir(env.root)
			draft := tt.draft
			if draft == nil {
				draft = noDraft
			}
			code, _, errOut := runInit(cliArgs{}, tt.input, draft)
			if code != 1 || !strings.Contains(errOut, tt.wantErr) {
				t.Fatalf("code %d, stderr %q, want %q", code, errOut, tt.wantErr)
			}
			assertNothingWritten(t, env.target)
		})
	}
}

func TestInit_AIDraft(t *testing.T) {
	env := newInitEnv(t)
	var descriptions []string
	draft := func(_ context.Context, d string) (string, error) {
		descriptions = append(descriptions, d)
		if len(descriptions) == 1 {
			return "", errors.New("codex is not installed")
		}
		return "# Drafted by AI\nworkflow:\n  extends: github-flow\n  task_types:\n    feature:\n      prefixes: [\"feat/\"]\n      from: production\n      close:\n        - review: {into: [production]}\n", nil
	}
	code, out, errOut := runInit(cliArgs{}, env.root+"\n\n\n4\nPRs into main\n4\nPRs into main, feat/ branches\n", draft)
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if !strings.Contains(out, "AI draft failed: codex is not installed") || !strings.Contains(out, "# Drafted by AI") || len(descriptions) != 2 {
		t.Fatalf("stdout %q, descriptions %q", out, descriptions)
	}
	raw, describe := loadWritten(t, env.target)
	if raw.Workflow.Extends != "github-flow" || !strings.Contains(describe, "feat/") {
		t.Fatalf("workflow %+v, describe %q", raw.Workflow, describe)
	}
}

func TestInit_AIDraftFailureFallsBackToPreset(t *testing.T) {
	env := newInitEnv(t)
	draft := func(context.Context, string) (string, error) { return "", errors.New("not signed in") }
	if code, _, errOut := runInit(cliArgs{}, env.root+"\n\n\n4\nx\n3\n", draft); code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if raw, _ := loadWritten(t, env.target); raw.Workflow.Extends != "trunk-based" {
		t.Fatalf("workflow = %+v", raw.Workflow)
	}
}

func TestInit_ExistingFile(t *testing.T) {
	env := newInitEnv(t)
	if err := os.MkdirAll(filepath.Dir(env.target), 0o755); err != nil {
		t.Fatal(err)
	}
	const old = "editor: old\n"
	if err := os.WriteFile(env.target, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	args := cliArgs{yes: true, root: env.root, preset: "github-flow"}

	code, _, errOut := runInit(args, "", noDraft)
	if code != 1 || !strings.Contains(errOut, env.target+" already exists; use --force to replace it") {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if data, _ := os.ReadFile(env.target); string(data) != old {
		t.Fatalf("file clobbered: %q", data)
	}

	args.force = true
	code, out, errOut := runInit(args, "", noDraft)
	if code != 0 || !strings.Contains(out, "Backed up the previous config to "+env.target+".bak-") {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
	backups, _ := filepath.Glob(env.target + ".bak-*")
	if len(backups) != 1 {
		t.Fatalf("backups = %v", backups)
	}
	if data, _ := os.ReadFile(backups[0]); string(data) != old {
		t.Fatalf("backup = %q", data)
	}
	if raw, _ := loadWritten(t, env.target); raw.Workflow.Extends != "github-flow" {
		t.Fatalf("workflow = %+v", raw.Workflow)
	}
}

func TestInit_NextStepsQuoteConfigPathForSh(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the printed command is POSIX sh on Unix")
	}
	t.Setenv("F4_ENV", "expanded")
	for _, name := range []string{"plain.yaml", "with space.yaml", "c$F4_ENV.yaml", "it's.yaml", `dq"q.yaml`, "back`echo x`tick.yaml", `back\slash.yaml`} {
		t.Run(name, func(t *testing.T) {
			env := newInitEnv(t)
			target := filepath.Join(env.root, name)
			code, out, errOut := runInit(cliArgs{yes: true, config: target, root: env.root}, "", noDraft)
			if code != 0 {
				t.Fatalf("code %d, stderr %q", code, errOut)
			}
			var line string
			for _, l := range strings.Split(out, "\n") {
				if cmd, ok := strings.CutSuffix(strings.TrimSpace(l), "start wtui"); ok {
					line = strings.TrimSpace(cmd)
				}
			}
			// wtui is shadowed by a function that echoes its arguments one per line.
			got, err := exec.Command("/bin/sh", "-c", `wtui() { printf '%s\n' "$@"; }; `+line).Output()
			if err != nil {
				t.Fatalf("%q: %v", line, err)
			}
			if want := "--config\n" + target + "\n"; string(got) != want {
				t.Fatalf("%q ran as %q, want %q", line, got, want)
			}
		})
	}
}

func TestInit_YesRoundTripsSpecialCharacters(t *testing.T) {
	env := newInitEnv(t)
	root := filepath.Join(env.root, `my repos #1: "a" 'b'`)
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(env.root, "conf dir", "c.yaml")
	editor := `code --wait # "x": y`
	code, _, errOut := runInitReader(cliArgs{yes: true, config: target, root: root, tasksRoot: "~/tasks: here", editor: editor, preset: "trunk-based"}, unreadable{t}, noDraft)
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	raw, describe := loadWritten(t, target)
	home, _ := os.UserHomeDir()
	if raw.RootDir != root || raw.TasksRoot != filepath.Join(home, "tasks: here") || raw.Editor != editor || !strings.Contains(describe, "workflow: trunk-based") {
		t.Fatalf("config = %+v, describe %q", raw, describe)
	}
}

func TestInit_YesRejectsRootThatIsNotADirectory(t *testing.T) {
	env := newInitEnv(t)
	file := filepath.Join(env.root, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{"/nonexistent/wtui-root", file} {
		code, _, errOut := runInitReader(cliArgs{yes: true, root: root}, unreadable{t}, noDraft)
		if code != 2 || !strings.Contains(errOut, "--root: "+root+" is not an existing directory") || !strings.Contains(errOut, "usage:") {
			t.Fatalf("root %s: code %d, stderr %q", root, code, errOut)
		}
		assertNothingWritten(t, env.target)
	}
}

func TestInit_InteractiveReasksWhenRootIsAFile(t *testing.T) {
	env := newInitEnv(t)
	file := filepath.Join(env.root, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := runInit(cliArgs{}, file+"\n"+env.root+"\n\n\n\n", noDraft); code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if raw, _ := loadWritten(t, env.target); raw.RootDir != env.root {
		t.Fatalf("root_dir = %q, want %q", raw.RootDir, env.root)
	}
}

func TestInit_DoesNotClobberConfigWrittenDuringPrompts(t *testing.T) {
	env := newInitEnv(t)
	var winner string
	stdin := &beforeRead{r: strings.NewReader(env.root + "\n\nlate-writer\n1\n"), hook: func() {
		if code, _, errOut := runInit(cliArgs{yes: true, root: env.root, editor: "winner"}, "", noDraft); code != 0 {
			t.Fatalf("competing init: code %d, stderr %q", code, errOut)
		}
		winner = readFile(t, env.target)
	}}
	code, _, errOut := runInitReader(cliArgs{}, stdin, noDraft)
	if code != 1 || !strings.Contains(errOut, env.target+" already exists; use --force to replace it") {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if got := readFile(t, env.target); got != winner || !strings.Contains(got, "editor: winner") {
		t.Fatalf("target = %q, want the competing init's %q", got, winner)
	}
	assertNoTemps(t, filepath.Dir(env.target))
}

func TestInit_SuccessiveForcesKeepEveryGeneration(t *testing.T) {
	env := newInitEnv(t)
	fixed := time.Date(2026, 11, 25, 12, 0, 0, 0, time.Local)
	clock = func() time.Time { return fixed }
	t.Cleanup(func() { clock = time.Now })
	if err := os.MkdirAll(filepath.Dir(env.target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.target, []byte("editor: OLD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, editor := range []string{"A", "B"} {
		if code, _, errOut := runInit(cliArgs{yes: true, force: true, root: env.root, editor: editor}, "", noDraft); code != 0 {
			t.Fatalf("force %s: code %d, stderr %q", editor, code, errOut)
		}
	}
	backups, _ := filepath.Glob(env.target + ".bak-*")
	var kept []string
	for _, b := range backups {
		kept = append(kept, readFile(t, b))
	}
	if len(kept) != 2 || kept[0] != "editor: OLD\n" || !strings.Contains(kept[1], "editor: A\n") {
		t.Fatalf("backups %v hold %q, want OLD then A", backups, kept)
	}
	if !strings.Contains(readFile(t, env.target), "editor: B\n") {
		t.Fatal("target is not the last forced write")
	}
}

func TestInit_ConcurrentForcesLoseNoGeneration(t *testing.T) {
	env := newInitEnv(t)
	if err := os.MkdirAll(filepath.Dir(env.target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env.target, []byte("editor: OLD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const writers = 8
	var wg sync.WaitGroup
	errs := make([]string, writers)
	for i := range writers {
		wg.Go(func() {
			if code, _, errOut := runInit(cliArgs{yes: true, force: true, root: env.root, editor: fmt.Sprintf("w%d", i)}, "", noDraft); code != 0 {
				errs[i] = errOut
			}
		})
	}
	wg.Wait()
	for i, e := range errs {
		if e != "" {
			t.Fatalf("writer %d failed: %s", i, e)
		}
	}
	files, _ := filepath.Glob(env.target + "*")
	seen := map[string]int{}
	for _, f := range files {
		content := readFile(t, f)
		key := "OLD"
		if _, after, ok := strings.Cut(content, "editor: "); ok && !strings.HasPrefix(after, "OLD") {
			key, _, _ = strings.Cut(after, "\n")
		}
		seen[key]++
	}
	if len(files) != writers+1 || seen["OLD"] != 1 {
		t.Fatalf("files %v, generations %v", files, seen)
	}
	for i := range writers {
		if seen[fmt.Sprintf("w%d", i)] != 1 {
			t.Fatalf("generation w%d kept %d times: %v", i, seen[fmt.Sprintf("w%d", i)], seen)
		}
	}
	assertNoTemps(t, filepath.Dir(env.target))
}

// afterCommit fails the test when anything reaches stdout before target holds the new config.
type afterCommit struct {
	t            *testing.T
	target, want string
	buf          bytes.Buffer
}

func (w *afterCommit) Write(p []byte) (int, error) {
	if got, _ := os.ReadFile(w.target); !strings.Contains(string(got), w.want) {
		w.t.Errorf("stdout written before the new config was in place: %q", p)
	}
	return w.buf.Write(p)
}

func seedConfig(t *testing.T, target, content string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(content), perm); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, perm); err != nil {
		t.Fatal(err)
	}
}

func failWith(err error) func(string, string) error {
	return func(string, string) error { return err }
}

func TestInit_ForceReplacesInPlaceWithoutHardLinks(t *testing.T) {
	env := newInitEnv(t)
	seedConfig(t, env.target, "editor: OLD\n", 0o600)
	linkFile = failWith(errors.New("operation not supported"))
	t.Cleanup(func() { linkFile = os.Link })

	out := &afterCommit{t: t, target: env.target, want: "editor: NEW"}
	var stderr bytes.Buffer
	if code := initCommand(cliArgs{init: true, yes: true, force: true, root: env.root, editor: "NEW"}, unreadable{t}, out, &stderr, noDraft); code != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr.String())
	}
	if info, err := os.Lstat(env.target); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("target mode = %v, %v; want the original 0600", info, err)
	}
	backups, _ := filepath.Glob(env.target + ".bak-*")
	if len(backups) != 1 || readFile(t, backups[0]) != "editor: OLD\n" || !strings.Contains(out.buf.String(), "Backed up the previous config to "+backups[0]) {
		t.Fatalf("backups %v, stdout %q", backups, out.buf.String())
	}
	assertNoTemps(t, filepath.Dir(env.target))
}

func TestInit_FailedPublicationChangesNothing(t *testing.T) {
	unsupported := failWith(errors.New("operation not supported"))
	for _, tt := range []struct {
		name     string
		existing bool
		link     func(string, string) error
		replace  func(string, string) error
	}{
		{name: "replace fails on force", existing: true, link: os.Link, replace: unsupported},
		{name: "link fails on create", link: unsupported, replace: os.Rename},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env := newInitEnv(t)
			if tt.existing {
				seedConfig(t, env.target, "editor: OLD\n", 0o644)
			}
			linkFile, replaceFile = tt.link, tt.replace
			t.Cleanup(func() { linkFile, replaceFile = os.Link, os.Rename })

			out := &afterCommit{t: t, target: env.target, want: "editor: NEW"}
			var stderr bytes.Buffer
			if code := initCommand(cliArgs{init: true, yes: true, force: true, root: env.root, editor: "NEW"}, unreadable{t}, out, &stderr, noDraft); code != 1 || !strings.Contains(stderr.String(), "operation not supported") {
				t.Fatalf("code %d, stderr %q", code, stderr.String())
			}
			if tt.existing {
				if got := readFile(t, env.target); got != "editor: OLD\n" {
					t.Fatalf("target = %q", got)
				}
			} else if _, err := os.Lstat(env.target); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("target exists after failed create: %v", err)
			}
			if backups, _ := filepath.Glob(env.target + ".bak-*"); len(backups) != 0 {
				t.Fatalf("backups left: %v", backups)
			}
			assertNoTemps(t, filepath.Dir(env.target))
		})
	}
}

func TestInit_RefusesSymlinkTargetsWithoutTouchingThem(t *testing.T) {
	for _, tt := range []struct {
		name     string
		dangling bool
		force    bool
	}{{"dangling", true, false}, {"dangling force", true, true}, {"live", false, false}, {"live force", false, true}} {
		t.Run(tt.name, func(t *testing.T) {
			env := newInitEnv(t)
			dir := filepath.Dir(env.target)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if !tt.dangling {
				if err := os.WriteFile(filepath.Join(dir, "real.yaml"), []byte("editor: real\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink("real.yaml", env.target); err != nil {
				t.Skip("symlinks unavailable:", err)
			}
			code, _, errOut := runInit(cliArgs{yes: true, force: tt.force, root: env.root}, "", noDraft)
			if code != 1 || !strings.Contains(errOut, env.target+" is a symbolic link") {
				t.Fatalf("code %d, stderr %q", code, errOut)
			}
			if link, err := os.Readlink(env.target); err != nil || link != "real.yaml" {
				t.Fatalf("link = %q, %v", link, err)
			}
			entries, _ := os.ReadDir(dir)
			want := 1
			if !tt.dangling {
				want = 2
				if got := readFile(t, filepath.Join(dir, "real.yaml")); got != "editor: real\n" {
					t.Fatalf("referent = %q", got)
				}
			}
			if len(entries) != want {
				t.Fatalf("dir entries = %v", entries)
			}
		})
	}
}

func TestRun_InitYesRoutesToInit(t *testing.T) {
	env := newInitEnv(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"init", "--yes", "--root", env.root}, &stdout, &stderr); code != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr.String())
	}
	if raw, _ := loadWritten(t, env.target); raw.Workflow.Extends != "git-flow" || raw.RootDir != env.root {
		t.Fatalf("config = %+v", raw)
	}
}
