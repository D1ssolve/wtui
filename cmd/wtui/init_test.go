package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	c.init = true
	var stdout, stderr bytes.Buffer
	code := initCommand(c, strings.NewReader(input), &stdout, &stderr, draft)
	return code, stdout.String(), stderr.String()
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
	entries, _ := os.ReadDir(filepath.Dir(target))
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

func TestInit_YesRoundTripsSpecialCharacters(t *testing.T) {
	env := newInitEnv(t)
	root := filepath.Join(env.root, `my repos #1: "a" 'b'`)
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(env.root, "conf dir", "c.yaml")
	editor := `code --wait # "x": y`
	code, out, errOut := runInit(cliArgs{yes: true, config: target, root: root, tasksRoot: "~/tasks: here", editor: editor, preset: "trunk-based"}, "", noDraft)
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if strings.Contains(out, "[") {
		t.Fatalf("--yes prompted: %q", out)
	}
	raw, describe := loadWritten(t, target)
	home, _ := os.UserHomeDir()
	if raw.RootDir != root || raw.TasksRoot != filepath.Join(home, "tasks: here") || raw.Editor != editor || !strings.Contains(describe, "workflow: trunk-based") {
		t.Fatalf("config = %+v, describe %q", raw, describe)
	}
}

func TestInit_YesRejectsMissingRoot(t *testing.T) {
	env := newInitEnv(t)
	code, _, errOut := runInit(cliArgs{yes: true, root: "/nonexistent/wtui-root"}, "", noDraft)
	if code != 2 || !strings.Contains(errOut, "--root: /nonexistent/wtui-root is not an existing directory") || !strings.Contains(errOut, "usage:") {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	assertNothingWritten(t, env.target)
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
