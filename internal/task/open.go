package task

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/diss0x/wtui/internal/config"
)

// OpenableFile represents a file the user can open for a task.
type OpenableFile struct {
	Name string // display label, e.g. "task.sln"
	Path string // absolute path
	Ext  string // ".sln" or ".code-workspace"
}

// AppEntry represents an application that can open a file.
type AppEntry struct {
	Name   string // display label, e.g. "VS Code"
	Binary string // exec path, e.g. "code" or "/Applications/Rider.app/Contents/MacOS/rider"
}

// OpenCandidates bundles the files and apps for the open picker.
type OpenCandidates struct {
	Files []OpenableFile
	Apps  []AppEntry
}

// detectApps returns the list of applications that can open task files.
//
// Phase 1: exec.LookPath for known binaries in PATH. Phase 2 (macOS only):
// os.Stat on well-known .app bundle binaries under /Applications.
// De-duplicates by resolved Binary path. Falls back to cfg.Editor when no
// apps are detected. Never returns an error — detection is best-effort.
func detectApps(cfg *config.Config) []AppEntry {
	type knownBinary struct {
		binary      string
		displayName string
	}

	binaries := []knownBinary{
		{"code", "VS Code"},
		{"cursor", "Cursor"},
		{"rider", "Rider"},
		{"webstorm", "WebStorm"},
	}

	seen := make(map[string]struct{})
	var apps []AppEntry

	// Phase 1: binary lookup via PATH.
	for _, kb := range binaries {
		resolved, err := exec.LookPath(kb.binary)
		if err != nil {
			continue
		}
		seen[resolved] = struct{}{}
		apps = append(apps, AppEntry{Name: kb.displayName, Binary: resolved})
	}

	// Phase 2: macOS /Applications bundles (checked at call-time, not build-time,
	// so the binary remains CGO-free and cross-compilable).
	if runtime.GOOS == "darwin" {
		type appBundle struct {
			binaryPath  string
			displayName string
		}
		bundles := []appBundle{
			{"/Applications/Visual Studio Code.app/Contents/MacOS/Electron", "VS Code (App)"},
			{"/Applications/Rider.app/Contents/MacOS/rider", "Rider (App)"},
			{"/Applications/Cursor.app/Contents/MacOS/Cursor", "Cursor (App)"},
			{"/Applications/WebStorm.app/Contents/MacOS/webstorm", "WebStorm (App)"},
		}
		for _, b := range bundles {
			if _, err := os.Stat(b.binaryPath); err != nil {
				continue
			}
			if _, already := seen[b.binaryPath]; already {
				continue
			}
			seen[b.binaryPath] = struct{}{}
			apps = append(apps, AppEntry{Name: b.displayName, Binary: b.binaryPath})
		}
	}

	// Fallback: use configured editor when nothing was detected so callers
	// always receive at least one usable entry.
	if len(apps) == 0 {
		apps = append(apps, AppEntry{Name: cfg.Editor, Binary: cfg.Editor})
	}

	return apps
}

// openableFiles lists all .sln and .code-workspace files in taskDir.
//
// Sort order: .sln entries first, then .code-workspace, then alphabetically
// within each group. Returns []OpenableFile{} (not nil) when no matching files
// are found.
func openableFiles(taskDir string) ([]OpenableFile, error) {
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return nil, fmt.Errorf("openable files: read dir %s: %w", taskDir, err)
	}

	var files []OpenableFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := filepath.Ext(name)
		if ext != ".sln" && ext != ".code-workspace" {
			continue
		}
		absPath := filepath.Join(taskDir, name)
		files = append(files, OpenableFile{
			Name: filepath.Base(absPath),
			Path: absPath,
			Ext:  ext,
		})
	}

	// Sort: .sln first, then .code-workspace; alphabetically within each group.
	sort.Slice(files, func(i, j int) bool {
		ei, ej := files[i].Ext, files[j].Ext
		if ei != ej {
			// .sln sorts before .code-workspace
			return ei == ".sln"
		}
		return files[i].Name < files[j].Name
	})

	// Always return an initialized (non-nil) slice.
	if files == nil {
		return []OpenableFile{}, nil
	}
	return files, nil
}

// ListOpenCandidates returns all openable files for the task directory and
// the detected applications that can open them. Never returns a nil Files slice.
func (m *manager) ListOpenCandidates(ctx context.Context, taskID string) (OpenCandidates, error) {
	if err := validateTaskID(taskID); err != nil {
		return OpenCandidates{}, err
	}

	taskDir := m.taskDir(taskID)

	// Guard: return ErrTaskNotFound when the task directory does not exist,
	// consistent with ListServices and other task-scoped operations.
	if _, err := os.Stat(taskDir); os.IsNotExist(err) {
		return OpenCandidates{}, fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	} else if err != nil {
		return OpenCandidates{}, fmt.Errorf("list open candidates: stat task dir %s: %w", taskDir, err)
	}

	files, err := openableFiles(taskDir)
	if err != nil {
		return OpenCandidates{}, fmt.Errorf("list open candidates: %w", err)
	}

	apps := detectApps(m.cfg)
	return OpenCandidates{Files: files, Apps: apps}, nil
}

// OpenFile launches app with path non-blocking (cmd.Start only, not cmd.Run).
// Returns an error if path or app is empty, or if cmd.Start fails.
func (m *manager) OpenFile(ctx context.Context, path, app string) error {
	if path == "" {
		return fmt.Errorf("open file: path must not be empty")
	}
	if app == "" {
		return fmt.Errorf("open file: app must not be empty")
	}

	cmd := exec.CommandContext(ctx, app, path)
	cmd.Dir = filepath.Dir(path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open file: start %q: %w", app, err)
	}

	m.logger.InfoContext(ctx, "opened file",
		slog.String("path", path),
		slog.String("app", app),
	)
	return nil
}
