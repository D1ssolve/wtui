package task

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

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

func generateWorkspaceFile(taskID, taskDir string) error {
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return fmt.Errorf("workspace: read task dir %s: %w", taskDir, err)
	}

	var folders []workspaceFolder
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		folders = append(folders, workspaceFolder{Path: entry.Name()})
	}

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

	data = append(data, '\n')

	wsPath := filepath.Join(taskDir, taskID+".code-workspace")

	tmp, err := os.CreateTemp(taskDir, ".wtui-workspace-*.tmp")
	if err != nil {
		return fmt.Errorf("workspace: create temp file: %w", err)
	}
	tmpName := tmp.Name()

	success := false
	defer func() {
		if !success {
			os.Remove(tmpName)
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
