package task

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
)

func TestInit_CopiesConfiguredLocalFilesAfterCreatingWorktree(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	repoPath := filepath.Join(rootDir, "service-api")
	makeGitDir(t, repoPath)
	localPath := filepath.Join(repoPath, "appsettings.Development.json")
	if err := os.WriteFile(localPath, []byte(`{"Local":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	gitClient := &mockGitClient{
		baseBranchResult:  "main",
		listLocalFilesRes: []string{"appsettings.Development.json"},
		addWorktreeFn: func(_, dest, _ string, _ bool, _ string) error {
			return os.MkdirAll(dest, 0o755)
		},
	}
	cfg := &config.Config{
		RootDir:   rootDir,
		TasksRoot: tasksRoot,
		Worktree: &config.WorktreeConfig{Copy: []string{
			"**/appsettings.Development.json",
		}},
	}
	if _, err := cfg.Effective(); err != nil {
		t.Fatal(err)
	}
	mgr := newTestManagerWithCfg(t, cfg, gitClient)

	if _, err := mgr.Init(context.Background(), InitParams{
		TaskID:   "APP-123",
		Services: []string{"service-api"},
	}); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	copied := filepath.Join(tasksRoot, "APP-123", "service-api", "appsettings.Development.json")
	got, err := os.ReadFile(copied)
	if err != nil {
		t.Fatalf("read copied file: %v", err)
	}
	if string(got) != `{"Local":true}` {
		t.Fatalf("copied content = %q", got)
	}
}

func TestCopyLocalFiles_CopiesMatchingFilesAndPreservesMode(t *testing.T) {
	source := t.TempDir()
	dest := t.TempDir()
	settingsPath := filepath.Join(source, ".claude", "settings.json")
	appsettingsPath := filepath.Join(source, "src", "appsettings.Development.json")
	for path, content := range map[string]string{
		settingsPath:                       `{"theme":"dark"}`,
		appsettingsPath:                    `{"Local":true}`,
		filepath.Join(source, "README.md"): "skip",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	gitClient := &mockGitClient{listLocalFilesRes: []string{
		".claude/settings.json",
		"README.md",
		"src/appsettings.Development.json",
	}}
	m := &manager{
		cfg: &config.Config{Worktree: &config.WorktreeConfig{Copy: []string{
			".claude/**",
			"**/appsettings.Development.json",
		}}},
		git:    gitClient,
		logger: slog.Default(),
	}

	m.copyLocalFiles(t.Context(), source, dest, nil)

	for relative, want := range map[string]string{
		".claude/settings.json":            `{"theme":"dark"}`,
		"src/appsettings.Development.json": `{"Local":true}`,
	} {
		path := filepath.Join(dest, filepath.FromSlash(relative))
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read copied %s: %v", relative, err)
		}
		if string(got) != want {
			t.Errorf("copied %s = %q, want %q", relative, got, want)
		}
		if runtime.GOOS != "windows" {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Errorf("copied %s mode = %o, want 600", relative, info.Mode().Perm())
			}
		}
	}
	if _, err := os.Stat(filepath.Join(dest, "README.md")); !os.IsNotExist(err) {
		t.Fatalf("unmatched README copied, stat error = %v", err)
	}
}

func TestCopyLocalFiles_ListFailureWarnsAndReturns(t *testing.T) {
	statusCh := make(chan string, 1)
	m := &manager{
		cfg:    &config.Config{Worktree: &config.WorktreeConfig{Copy: []string{".claude/**"}}},
		git:    &mockGitClient{listLocalFilesErr: errors.New("git failed")},
		logger: slog.Default(),
	}

	m.copyLocalFiles(t.Context(), t.TempDir(), t.TempDir(), statusCh)

	select {
	case status := <-statusCh:
		if !strings.Contains(status, "failed to list local files") {
			t.Fatalf("status = %q, want list failure warning", status)
		}
	default:
		t.Fatal("expected warning status")
	}
}

func TestCopyLocalFiles_SkipsSymlinks(t *testing.T) {
	source := t.TempDir()
	dest := t.TempDir()
	target := filepath.Join(source, "target.json")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(source, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	m := &manager{
		cfg:    &config.Config{Worktree: &config.WorktreeConfig{Copy: []string{".claude/**"}}},
		git:    &mockGitClient{listLocalFilesRes: []string{".claude/settings.json"}},
		logger: slog.Default(),
	}

	m.copyLocalFiles(t.Context(), source, dest, nil)

	if _, err := os.Lstat(filepath.Join(dest, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("symlink copied, stat error = %v", err)
	}
}

func TestCopyLocalFiles_DoesNotOverwriteExistingDestination(t *testing.T) {
	source := t.TempDir()
	dest := t.TempDir()
	relative := "appsettings.Development.json"
	if err := os.WriteFile(filepath.Join(source, relative), []byte("source-local"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, relative), []byte("target-branch"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &manager{
		cfg: &config.Config{Worktree: &config.WorktreeConfig{Copy: []string{
			"**/appsettings.Development.json",
		}}},
		git:    &mockGitClient{listLocalFilesRes: []string{relative}},
		logger: slog.Default(),
	}

	m.copyLocalFiles(t.Context(), source, dest, nil)

	got, err := os.ReadFile(filepath.Join(dest, relative))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "target-branch" {
		t.Fatalf("destination = %q, want existing target-branch content", got)
	}
}

func TestCopyLocalFiles_DoesNotFollowDestinationSymlinkOutsideWorktree(t *testing.T) {
	source := t.TempDir()
	dest := t.TempDir()
	outside := t.TempDir()
	relative := filepath.Join(".claude", "settings.json")
	if err := os.MkdirAll(filepath.Join(source, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, relative), []byte("local"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dest, ".claude")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	m := &manager{
		cfg:    &config.Config{Worktree: &config.WorktreeConfig{Copy: []string{".claude/**"}}},
		git:    &mockGitClient{listLocalFilesRes: []string{filepath.ToSlash(relative)}},
		logger: slog.Default(),
	}

	m.copyLocalFiles(t.Context(), source, dest, nil)

	if _, err := os.Stat(filepath.Join(outside, "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("copy escaped worktree through symlink, stat error = %v", err)
	}
}
