package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

const applyDraft = "# Drafted by AI\nworkflow:\n  extends: github-flow\n"

func TestApplyWorkflow_StagedConfigPrivateBeforePublicationLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	env := newInitEnv(t)
	const old = "editor: vim\n"
	seedConfig(t, env.target, old, 0o600)
	lock := acquireConfigLock
	t.Cleanup(func() { acquireConfigLock = lock })
	acquireConfigLock = func(path string) (func(), error) {
		files, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".wtui-init-*.yaml"))
		if err != nil || len(files) != 1 {
			t.Fatalf("staged file: %v, %v", files, err)
		}
		info, err := os.Stat(files[0])
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode&0o077 != 0 {
			t.Errorf("staged config mode before lock: %o; must be private", mode)
		}
		return lock(path)
	}
	if _, err := applyWorkflowFile(env.target, []byte(old), applyDraft); err != nil {
		t.Fatal(err)
	}
}

func TestApplyWorkflow_ReplacesOnlyWorkflow(t *testing.T) {
	env := newInitEnv(t)
	before := "# Config\r\neditor: &editor 'vim' # editor note\r\n"
	after := "\r\n  # Keep this next to the extension\r\nx-copy: {value: *editor} # extension note\r\n...\r\n# EOF\r\n"
	old := before + "workflow:\r\n    extends: git-flow\r\n" + after
	seedConfig(t, env.target, old, 0o640)

	backup, err := applyWorkflowFile(env.target, []byte(old), applyDraft)
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, env.target); got != before+"workflow:\r\n  extends: github-flow\r\n"+after {
		t.Fatalf("unexpected config: %q", got)
	}
	if got := readFile(t, backup); got != old {
		t.Fatalf("backup = %q, want exact original bytes", got)
	}
	for path, want := range map[string]os.FileMode{env.target: 0o640, backup: 0o600} {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != want {
			t.Fatalf("%s: stat = %v, %v; want permissions %o", path, info, err, want)
		}
	}
}

func TestApplyWorkflow_HandlesMissingBlockAndMissingFile(t *testing.T) {
	for _, old := range []*string{nil, new(""), new("# Config\neditor: vim\n...\n# EOF\n"), new("# Comment only")} {
		env := newInitEnv(t)
		t.Chdir(env.root)
		var expected []byte
		if old != nil {
			seedConfig(t, env.target, *old, 0o600)
			expected = []byte(*old)
		}
		backup, err := applyWorkflowFile(env.target, expected, applyDraft)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := loadWritten(t, env.target)
		if raw.Workflow.Extends != "github-flow" {
			t.Fatalf("workflow = %+v", raw.Workflow)
		}
		if old == nil && backup != "" || old != nil && readFile(t, backup) != *old {
			t.Fatalf("unexpected backup %q for snapshot %q", backup, expected)
		}
	}
}

func TestApplyWorkflow_RejectsUnsafeMergeWithoutPublishing(t *testing.T) {
	for _, tc := range []struct{ name, old, draft string }{
		{"extra draft key", "editor: vim\n", applyDraft + "editor: nano\n"},
		{"unknown draft workflow key", "editor: vim\n", applyDraft + "  typo: true\n"},
		{"invalid draft workflow", "editor: vim\n", "workflow: {extends: bogus}\n"},
		{"invalid effective config", "concurrency: -1\n", applyDraft},
		{"second draft document", "editor: vim\n", applyDraft + "---\n"},
		{"second existing document", "editor: vim\n---\neditor: nano\n", applyDraft},
		{"duplicate workflow key", "workflow: {}\n'workflow': {}\n", applyDraft},
		{"top-level merge", "x-base: &base {editor: vim}\n<<: *base\n", applyDraft},
		{"alias to replaced workflow", "workflow: &old {extends: git-flow}\nx-ref: *old\n", applyDraft},
		{"alias must not rebind", "workflow: &old {extends: git-flow}\nx-ref: *old\n", "workflow: &old {extends: github-flow}\n"},
		{"draft anchor collision", "editor: &preset vim\n", "workflow:\n  extends: &preset github-flow\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newInitEnv(t)
			seedConfig(t, env.target, tc.old, 0o600)
			if backup, err := applyWorkflowFile(env.target, []byte(tc.old), tc.draft); err == nil || backup != "" {
				t.Fatalf("backup = %q, err = %v", backup, err)
			}
			if got := readFile(t, env.target); got != tc.old {
				t.Fatalf("target changed: %q", got)
			}
			if backups, _ := filepath.Glob(env.target + ".bak-*"); len(backups) != 0 {
				t.Fatalf("unexpected backups: %v", backups)
			}
			assertNoTemps(t, filepath.Dir(env.target))
		})
	}
}

func TestApplyWorkflow_StaleSnapshotLeavesTargetAndBackupsUntouched(t *testing.T) {
	for _, tc := range []struct {
		name     string
		expected []byte
		current  *string
	}{
		{"edited by init", []byte("editor: old\n"), new("init")},
		{"removed", []byte{}, nil},
		{"created empty", nil, new("")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newInitEnv(t)
			protected := env.target + ".bak-previous"
			seedConfig(t, protected, "keep me", 0o600)
			var current string
			if tc.current != nil {
				current = *tc.current
				if current == "init" {
					if code, _, stderr := runInit(cliArgs{yes: true, root: env.root}, "", noDraft); code != 0 {
						t.Fatalf("init: %s", stderr)
					}
					current = readFile(t, env.target)
				} else {
					seedConfig(t, env.target, current, 0o600)
				}
			}
			if backup, err := applyWorkflowFile(env.target, tc.expected, applyDraft); err == nil || backup != "" {
				t.Fatalf("backup = %q, err = %v", backup, err)
			}
			if tc.current != nil {
				if got := readFile(t, env.target); got != current {
					t.Fatalf("target changed: %q", got)
				}
			} else if _, err := os.Lstat(env.target); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("target should still be missing: %v", err)
			}
			if backups, _ := filepath.Glob(env.target + ".bak-*"); len(backups) != 1 || readFile(t, protected) != "keep me" {
				t.Fatalf("backups changed: %v", backups)
			}
			assertNoTemps(t, filepath.Dir(env.target))
		})
	}
}

func TestApplyWorkflow_ConcurrentStaleWriterCannotReplaceWinner(t *testing.T) {
	env := newInitEnv(t)
	const old = "editor: vim\n"
	seedConfig(t, env.target, old, 0o600)
	drafts := []string{applyDraft, "workflow: {extends: trunk-based}\n"}
	var wg sync.WaitGroup
	var errs [2]error
	var backups [2]string
	for i, draft := range drafts {
		wg.Go(func() { backups[i], errs[i] = applyWorkflowFile(env.target, []byte(old), draft) })
	}
	wg.Wait()
	winner := 0
	if errs[0] != nil {
		winner = 1
	}
	if errs[winner] != nil || errs[1-winner] == nil || backups[1-winner] != "" {
		t.Fatalf("backups = %q, errors = %v", backups, errs)
	}
	raw, _ := loadWritten(t, env.target)
	if raw.Workflow.Extends != []string{"github-flow", "trunk-based"}[winner] {
		t.Fatalf("losing writer replaced winner: %+v", raw.Workflow)
	}
	if paths, _ := filepath.Glob(env.target + ".bak-*"); len(paths) != 1 || readFile(t, backups[winner]) != old {
		t.Fatalf("unexpected backups: %v", paths)
	}
}

func TestApplyWorkflow_MissingConfigWithoutUsableRootSuggestsInit(t *testing.T) {
	env := newInitEnv(t)
	t.Setenv("WTUI_ROOT", filepath.Join(env.root, "missing"))
	backup, err := applyWorkflowFile(env.target, nil, applyDraft)
	if err == nil || !strings.Contains(err.Error(), "wtui init") || backup != "" {
		t.Fatalf("backup = %q, err = %v; want guidance to run wtui init", backup, err)
	}
	assertNothingWritten(t, env.target)
}
