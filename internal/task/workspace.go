package task

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
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
	services, err := discoverServicesFromTaskDir(taskDir)
	if err != nil {
		return err
	}

	folders := make([]workspaceFolder, 0, len(services))
	for _, svc := range services {
		folders = append(folders, workspaceFolder{Path: svc.Name})
	}

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

func discoverServicesFromTaskDir(taskDir string) ([]domain.Service, error) {
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return nil, fmt.Errorf("workspace: read task dir %s: %w", taskDir, err)
	}

	services := make([]domain.Service, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		services = append(services, domain.Service{
			Name:     name,
			RepoPath: filepath.Join(taskDir, name),
		})
	}

	sort.Slice(services, func(i, j int) bool {
		return services[i].Name < services[j].Name
	})

	return services, nil
}

func removeGeneratedTaskFiles(taskDir, taskID string) error {
	paths := []string{
		filepath.Join(taskDir, taskID+".code-workspace"),
		filepath.Join(taskDir, taskID+".sln"),
	}

	var removeErrs []error
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			removeErrs = append(removeErrs, fmt.Errorf("workspace: remove generated file %s: %w", path, err))
		}
	}

	if len(removeErrs) > 0 {
		return errors.Join(removeErrs...)
	}

	return nil
}
