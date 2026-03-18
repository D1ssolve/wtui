package task

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// workspaceFile is the JSON structure written to <taskID>.code-workspace.
// Matches the VS Code multi-root workspace schema.
type workspaceFile struct {
	Folders  []workspaceFolder `json:"folders"`
	Settings workspaceSettings `json:"settings"`
}

type workspaceFolder struct {
	Path string `json:"path"`
}

type workspaceSettings struct {
	LabelFormat string `json:"workbench.editor.labelFormat"`
}

// generateWorkspaceFile creates or overwrites the VS Code .code-workspace file
// at <taskDir>/<taskID>.code-workspace.
//
// The folders list contains one entry per direct subdirectory of taskDir; paths
// are relative from taskDir to each service worktree (i.e., just the directory
// name since worktrees are direct children of taskDir).
//
// Entries are sorted alphabetically by path to produce a stable output.
//
// The file is written atomically (temp file + rename) to prevent partial reads by
// VS Code or other consumers.
func generateWorkspaceFile(taskID, taskDir string) error {
	// Collect subdirectories of taskDir.
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return fmt.Errorf("workspace: read task dir %s: %w", taskDir, err)
	}

	var folders []workspaceFolder
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// Compute the path relative from taskDir to the service worktree dir.
		// Since worktrees are direct children, this is just entry.Name(), but
		// filepath.Rel is used for correctness in case paths ever diverge.
		relPath, relErr := filepath.Rel(taskDir, filepath.Join(taskDir, entry.Name()))
		if relErr != nil {
			// Fallback: use the entry name directly.
			relPath = entry.Name()
		}
		folders = append(folders, workspaceFolder{Path: relPath})
	}

	// Sort alphabetically for a stable, diff-friendly output.
	sort.Slice(folders, func(i, j int) bool {
		return folders[i].Path < folders[j].Path
	})

	ws := workspaceFile{
		Folders: folders,
		Settings: workspaceSettings{
			LabelFormat: "medium",
		},
	}

	data, err := json.MarshalIndent(ws, "", "  ")
	if err != nil {
		return fmt.Errorf("workspace: marshal JSON: %w", err)
	}
	// Append a trailing newline for POSIX compliance and cleaner diffs.
	data = append(data, '\n')

	wsPath := filepath.Join(taskDir, taskID+".code-workspace")

	// Atomic write: write to a temp file in the same directory then rename.
	// Using the same directory ensures the rename is atomic on the same filesystem.
	tmp, err := os.CreateTemp(taskDir, ".wtui-workspace-*.tmp")
	if err != nil {
		return fmt.Errorf("workspace: create temp file: %w", err)
	}
	tmpName := tmp.Name()

	// Ensure the temp file is cleaned up on any error path.
	success := false
	defer func() {
		if !success {
			os.Remove(tmpName) //nolint:errcheck
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("workspace: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("workspace: close temp file: %w", err)
	}

	if err := os.Rename(tmpName, wsPath); err != nil {
		return fmt.Errorf("workspace: rename temp file to %s: %w", wsPath, err)
	}

	success = true
	return nil
}
