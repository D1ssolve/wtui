package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestPushTask_NoServices verifies that PushTask returns nil when the task has
// no service subdirectories.
func TestPushTask_NoServices(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	// Create an empty task directory (no service subdirs).
	taskDir := filepath.Join(tasksRoot, "IN-EMPTY")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})

	lineCh := make(chan string, 4)
	err := mgr.PushTask(context.Background(), "IN-EMPTY", lineCh)
	if err != nil {
		t.Errorf("PushTask returned error for task with no services: %v", err)
	}

	// lineCh should be closed after PushTask returns.
	for line := range lineCh {
		t.Errorf("unexpected line on lineCh: %q", line)
	}
}

// TestPushTask_PushesAllServices verifies that PushTask calls git.Push once per
// service in parallel and that all worktree paths are correct.
func TestPushTask_PushesAllServices(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskDir := filepath.Join(tasksRoot, "IN-PUSH-ALL")

	servicePaths := map[string]string{
		"svc-a": filepath.Join(taskDir, "svc-a"),
		"svc-b": filepath.Join(taskDir, "svc-b"),
	}
	for _, path := range servicePaths {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("setup: create service dir %s: %v", path, err)
		}
	}

	// Fake common dirs so ListServices can discover them as valid worktrees.
	fakeCommonDirs := map[string]string{
		servicePaths["svc-a"]: filepath.Join(rootDir, "repos", "svc-a", ".git"),
		servicePaths["svc-b"]: filepath.Join(rootDir, "repos", "svc-b", ".git"),
	}
	for _, path := range fakeCommonDirs {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("setup: create fake common dir %s: %v", path, err)
		}
	}

	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			commonDir, ok := fakeCommonDirs[path]
			if !ok {
				return "", errors.New("not a git worktree")
			}
			return commonDir, nil
		},
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	lineCh := make(chan string, 16)
	err := mgr.PushTask(context.Background(), "IN-PUSH-ALL", lineCh)
	if err != nil {
		t.Errorf("PushTask returned unexpected error: %v", err)
	}

	// Collect all progress lines.
	var lines []string
	for line := range lineCh {
		lines = append(lines, line)
	}

	// Both services must have been pushed.
	gitMock.mu.Lock()
	pushCalls := append([]string(nil), gitMock.pushCalls...)
	gitMock.mu.Unlock()

	if len(pushCalls) != 2 {
		t.Fatalf("expected 2 Push calls, got %d", len(pushCalls))
	}

	// Verify correct worktree paths.
	wantPaths := map[string]bool{
		servicePaths["svc-a"]: false,
		servicePaths["svc-b"]: false,
	}
	for _, path := range pushCalls {
		if _, ok := wantPaths[path]; ok {
			wantPaths[path] = true
		} else {
			t.Errorf("unexpected Push path: %q", path)
		}
	}
	for path, seen := range wantPaths {
		if !seen {
			t.Errorf("Push was not called for: %s", path)
		}
	}

	// Verify progress lines contain pushing and pushed messages for both services.
	assertContainsLine(t, lines, "[svc-a] pushing...")
	assertContainsLine(t, lines, "[svc-a] pushed.")
	assertContainsLine(t, lines, "[svc-b] pushing...")
	assertContainsLine(t, lines, "[svc-b] pushed.")
}

// TestPushTask_ContinuesOnError verifies that PushTask continues pushing all
// services even when one or more fail, and returns the first error encountered.
func TestPushTask_ContinuesOnError(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskDir := filepath.Join(tasksRoot, "IN-PUSH-ERR")

	servicePaths := map[string]string{
		"svc-a": filepath.Join(taskDir, "svc-a"),
		"svc-b": filepath.Join(taskDir, "svc-b"),
	}
	for _, path := range servicePaths {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("setup: create service dir %s: %v", path, err)
		}
	}

	fakeCommonDirs := map[string]string{
		servicePaths["svc-a"]: filepath.Join(rootDir, "repos", "svc-a", ".git"),
		servicePaths["svc-b"]: filepath.Join(rootDir, "repos", "svc-b", ".git"),
	}
	for _, path := range fakeCommonDirs {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("setup: create fake common dir %s: %v", path, err)
		}
	}

	pushErr := errors.New("push rejected")
	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			commonDir, ok := fakeCommonDirs[path]
			if !ok {
				return "", errors.New("not a git worktree")
			}
			return commonDir, nil
		},
		pushFn: func(path string, lineCh chan<- string) error {
			if filepath.Base(path) == "svc-b" {
				return pushErr
			}
			return nil
		},
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	lineCh := make(chan string, 16)
	err := mgr.PushTask(context.Background(), "IN-PUSH-ERR", lineCh)

	// Must return the first push error.
	if !errors.Is(err, pushErr) {
		t.Fatalf("PushTask error = %v, want %v", err, pushErr)
	}

	// Both services must have been attempted (best-effort).
	gitMock.mu.Lock()
	pushCalls := append([]string(nil), gitMock.pushCalls...)
	gitMock.mu.Unlock()

	if len(pushCalls) != 2 {
		t.Fatalf("expected 2 Push calls (best-effort), got %d", len(pushCalls))
	}

	// Collect progress lines.
	var lines []string
	for line := range lineCh {
		lines = append(lines, line)
	}

	// Both services must have been attempted.
	assertContainsLine(t, lines, "[svc-a] pushing...")
	assertContainsLine(t, lines, "[svc-a] pushed.")
	assertContainsLine(t, lines, "[svc-b] pushing...")
	assertContainsLine(t, lines, "[svc-b] push error: push rejected")
}

// TestPushTask_ReportsProgress verifies that PushTask writes progress lines to
// lineCh for a single service: pushing... message, git output, and pushed. message.
func TestPushTask_ReportsProgress(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskDir := filepath.Join(tasksRoot, "IN-PUSH-PROG")

	worktreePath := filepath.Join(taskDir, "svc-a")
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	fakeCommonDir := filepath.Join(rootDir, "repos", "svc-a", ".git")
	if err := os.MkdirAll(fakeCommonDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			if filepath.Base(path) == "svc-a" {
				return fakeCommonDir, nil
			}
			return "", errors.New("not a git worktree")
		},
		pushFn: func(path string, lineCh chan<- string) error {
			lineCh <- "Enumerating objects: 3, done."
			lineCh <- "To origin/feature/IN-PUSH-PROG"
			return nil
		},
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)

	lineCh := make(chan string, 8)
	err := mgr.PushTask(context.Background(), "IN-PUSH-PROG", lineCh)
	if err != nil {
		t.Fatalf("PushTask returned unexpected error: %v", err)
	}

	// Collect all lines from lineCh.
	var lines []string
	for line := range lineCh {
		lines = append(lines, line)
	}

	// Verify progress sequence.
	wantLines := []string{
		"[svc-a] pushing...",
		"Enumerating objects: 3, done.",
		"To origin/feature/IN-PUSH-PROG",
		"[svc-a] pushed.",
	}
	if len(lines) != len(wantLines) {
		t.Fatalf("got %d lines, want %d; lines = %v", len(lines), len(wantLines), lines)
	}
	for i, want := range wantLines {
		if lines[i] != want {
			t.Errorf("line %d = %q, want %q", i, lines[i], want)
		}
	}
}
