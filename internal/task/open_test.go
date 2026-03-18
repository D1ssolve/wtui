package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/diss0x/wtui/internal/config"
)

// TestOpenableFiles_SortOrder verifies that openableFiles returns .sln entries
// before .code-workspace entries, and excludes files with other extensions.
func TestOpenableFiles_SortOrder(t *testing.T) {
	dir := t.TempDir()

	// Create files in an order that would yield wrong results if unsorted.
	filesToCreate := []string{
		"b.code-workspace",
		"a.sln",
		"z.code-workspace",
		"c.sln",
		"ignored.txt",
	}
	for _, name := range filesToCreate {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
			t.Fatalf("setup: create %s: %v", name, err)
		}
	}

	files, err := openableFiles(dir)
	if err != nil {
		t.Fatalf("openableFiles returned unexpected error: %v", err)
	}

	// .txt must be excluded.
	for _, f := range files {
		if f.Ext == ".txt" {
			t.Errorf("openableFiles included .txt file %q, should be excluded", f.Name)
		}
	}

	// Must contain exactly 4 files (2 .sln + 2 .code-workspace).
	if len(files) != 4 {
		t.Fatalf("got %d files, want 4 (2 .sln + 2 .code-workspace)", len(files))
	}

	// First two entries must be .sln, sorted alphabetically within the group.
	if files[0].Ext != ".sln" {
		t.Errorf("files[0].Ext = %q, want %q", files[0].Ext, ".sln")
	}
	if files[0].Name != "a.sln" {
		t.Errorf("files[0].Name = %q, want %q", files[0].Name, "a.sln")
	}
	if files[1].Ext != ".sln" {
		t.Errorf("files[1].Ext = %q, want %q", files[1].Ext, ".sln")
	}
	if files[1].Name != "c.sln" {
		t.Errorf("files[1].Name = %q, want %q", files[1].Name, "c.sln")
	}

	// Last two entries must be .code-workspace, sorted alphabetically.
	if files[2].Ext != ".code-workspace" {
		t.Errorf("files[2].Ext = %q, want %q", files[2].Ext, ".code-workspace")
	}
	if files[2].Name != "b.code-workspace" {
		t.Errorf("files[2].Name = %q, want %q", files[2].Name, "b.code-workspace")
	}
	if files[3].Ext != ".code-workspace" {
		t.Errorf("files[3].Ext = %q, want %q", files[3].Ext, ".code-workspace")
	}
	if files[3].Name != "z.code-workspace" {
		t.Errorf("files[3].Name = %q, want %q", files[3].Name, "z.code-workspace")
	}

	// Path field must be absolute.
	for _, f := range files {
		if !filepath.IsAbs(f.Path) {
			t.Errorf("file %q has non-absolute path: %q", f.Name, f.Path)
		}
	}
}

// TestOpenableFiles_Empty verifies that openableFiles returns an initialized
// (non-nil) empty slice when no matching files exist in the directory.
func TestOpenableFiles_Empty(t *testing.T) {
	dir := t.TempDir()

	// Create a file that should NOT be matched.
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte(""), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	files, err := openableFiles(dir)
	if err != nil {
		t.Fatalf("openableFiles returned unexpected error: %v", err)
	}

	if files == nil {
		t.Fatal("openableFiles returned nil slice, want initialized empty slice")
	}

	if len(files) != 0 {
		t.Errorf("openableFiles returned %d files, want 0", len(files))
	}
}

// TestDetectApps_Fallback verifies that detectApps returns exactly one entry
// using cfg.Editor as both Name and Binary when no known apps are available
// (PATH is cleared so exec.LookPath finds nothing).
//
// On darwin, /Applications bundles may be present independent of PATH, so the
// fallback is only testable when no .app bundles are installed. The test skips
// on darwin to avoid false failures on developer machines.
func TestDetectApps_Fallback(t *testing.T) {
	if runtime.GOOS == "darwin" {
		// Phase 2 of detectApps stats fixed /Applications paths — if any of the
		// known bundles are installed the fallback will never fire, making an
		// "exactly one entry" assertion unreliable on a developer machine.
		t.Skip("skipping fallback test on darwin: /Applications bundles may be present")
	}

	// Clear PATH so exec.LookPath cannot resolve any known binary.
	t.Setenv("PATH", "")

	cfg := &config.Config{
		Editor: "myeditor",
	}

	apps := detectApps(cfg)

	if len(apps) != 1 {
		t.Fatalf("detectApps returned %d entries, want exactly 1 (fallback)", len(apps))
	}

	if apps[0].Binary != "myeditor" {
		t.Errorf("apps[0].Binary = %q, want %q", apps[0].Binary, "myeditor")
	}

	if apps[0].Name != "myeditor" {
		t.Errorf("apps[0].Name = %q, want %q", apps[0].Name, "myeditor")
	}
}

// TestOpenFile_ValidBinary verifies that OpenFile with a real binary (echo)
// and a valid path starts without error.
func TestOpenFile_ValidBinary(t *testing.T) {
	// Write a temp file so echo has a real argument.
	dir := t.TempDir()
	target := filepath.Join(dir, "testfile.txt")
	if err := os.WriteFile(target, []byte(""), 0o644); err != nil {
		t.Fatalf("setup: create temp file: %v", err)
	}

	tasksRoot := filepath.Join(dir, ".tasks")
	mgr := newTestManager(t, tasksRoot, dir, &mockGitClient{})

	// echo is always present; cmd.Start returns immediately.
	err := mgr.OpenFile(context.Background(), target, "echo")
	if err != nil {
		t.Errorf("OpenFile with echo returned unexpected error: %v", err)
	}
}

// TestOpenFile_EmptyPath verifies that OpenFile returns an error containing
// "path must not be empty" when called with an empty path.
func TestOpenFile_EmptyPath(t *testing.T) {
	dir := t.TempDir()
	tasksRoot := filepath.Join(dir, ".tasks")
	mgr := newTestManager(t, tasksRoot, dir, &mockGitClient{})

	err := mgr.OpenFile(context.Background(), "", "echo")
	if err == nil {
		t.Fatal("OpenFile with empty path returned nil, want error")
	}
	if !strings.Contains(err.Error(), "path must not be empty") {
		t.Errorf("error message = %q, want to contain %q", err.Error(), "path must not be empty")
	}
}

// TestOpenFile_EmptyApp verifies that OpenFile returns an error containing
// "app must not be empty" when called with an empty app.
func TestOpenFile_EmptyApp(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "testfile.txt")
	if err := os.WriteFile(target, []byte(""), 0o644); err != nil {
		t.Fatalf("setup: create temp file: %v", err)
	}

	tasksRoot := filepath.Join(dir, ".tasks")
	mgr := newTestManager(t, tasksRoot, dir, &mockGitClient{})

	err := mgr.OpenFile(context.Background(), target, "")
	if err == nil {
		t.Fatal("OpenFile with empty app returned nil, want error")
	}
	if !strings.Contains(err.Error(), "app must not be empty") {
		t.Errorf("error message = %q, want to contain %q", err.Error(), "app must not be empty")
	}
}

// TestOpenFile_WorkingDir verifies that OpenFile sets the child process working
// directory to the directory that contains the opened file.
//
// Strategy: write a small shell script that prints its own $PWD to an output
// file, then launch it via OpenFile and poll for the result.
// Skipped on Windows (no sh).
func TestOpenFile_WorkingDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh not available on Windows")
	}

	dir := t.TempDir()
	// Resolve symlinks so the comparison works on macOS where t.TempDir()
	// returns a path under /var that is a symlink to /private/var.
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	target := filepath.Join(resolvedDir, "project.sln")
	if err := os.WriteFile(target, []byte(""), 0o644); err != nil {
		t.Fatalf("setup: create target file: %v", err)
	}

	outFile := filepath.Join(resolvedDir, "cwd.out")

	// Write a shell script that echoes its working directory to outFile.
	scriptPath := filepath.Join(resolvedDir, "printcwd.sh")
	script := "#!/bin/sh\npwd > " + outFile + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("setup: write script: %v", err)
	}

	tasksRoot := filepath.Join(resolvedDir, ".tasks")
	mgr := newTestManager(t, tasksRoot, resolvedDir, &mockGitClient{})

	if err := mgr.OpenFile(context.Background(), target, scriptPath); err != nil {
		t.Fatalf("OpenFile returned unexpected error: %v", err)
	}

	// Poll for the output file with a short timeout (script exits almost
	// immediately; 2 s is generous even on busy CI).
	var gotCwd string
	for range 200 {
		data, readErr := os.ReadFile(outFile)
		if readErr == nil {
			gotCwd = strings.TrimSpace(string(data))
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if gotCwd == "" {
		t.Fatal("script did not write output file; cmd.Dir may not be set")
	}

	wantDir := resolvedDir
	if gotCwd != wantDir {
		t.Errorf("child process cwd = %q, want %q (directory of opened file)", gotCwd, wantDir)
	}
}

// TestListOpenCandidates_TaskNotFound verifies that ListOpenCandidates returns
// an error wrapping ErrTaskNotFound when the task directory does not exist.
func TestListOpenCandidates_TaskNotFound(t *testing.T) {
	dir := t.TempDir()
	// tasksRoot does not have a "NOTFOUND" subdirectory.
	tasksRoot := filepath.Join(dir, ".tasks")
	mgr := newTestManager(t, tasksRoot, dir, &mockGitClient{})

	_, err := mgr.ListOpenCandidates(context.Background(), "NOTFOUND")
	if err == nil {
		t.Fatal("ListOpenCandidates returned nil, want error for missing task dir")
	}
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("ListOpenCandidates error = %v, want error wrapping ErrTaskNotFound", err)
	}
}
