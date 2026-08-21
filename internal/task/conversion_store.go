package task

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	conversionManifestVersion = 1
	conversionRootName        = ".conversions"
	conversionMarkerName      = ".wtui-conversion.json"
)

type conversionManifest struct {
	Version      int                 `json:"version"`
	SourceTaskID string              `json:"source_task_id"`
	TargetTaskID string              `json:"target_task_id"`
	StagingDir   string              `json:"staging_dir"`
	CreatedAt    time.Time           `json:"created_at"`
	Services     []conversionService `json:"services"`
}

type conversionService struct {
	Name                string `json:"name"`
	RepoPath            string `json:"repo_path"`
	SourceWorktreePath  string `json:"source_worktree_path"`
	StagingWorktreePath string `json:"staging_worktree_path"`
	FinalWorktreePath   string `json:"final_worktree_path"`
	SourceBranch        string `json:"source_branch"`
	TargetBranch        string `json:"target_branch"`
	SourceSHA           string `json:"source_sha"`
	SourceRemoteSHA     string `json:"source_remote_sha,omitempty"`
}

func (m *manager) conversionStagingDir(sourceTaskID, targetTaskID string) string {
	sum := sha256.Sum256([]byte(sourceTaskID + "\x00" + targetTaskID))
	return filepath.Join(m.cfg.TasksRoot, conversionRootName, hex.EncodeToString(sum[:8]))
}

func (m *manager) loadConversionManifest(sourceTaskID string) (conversionManifest, bool, error) {
	return readConversionManifest(filepath.Join(m.taskDir(sourceTaskID), conversionMarkerName))
}

func readConversionManifest(path string) (conversionManifest, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return conversionManifest{}, false, nil
	}
	if err != nil {
		return conversionManifest{}, false, fmt.Errorf("conversion: read marker: %w", err)
	}
	manifest, err := decodeConversionManifest(data)
	if err != nil {
		return conversionManifest{}, false, fmt.Errorf("conversion: read marker: %w", err)
	}
	return manifest, true, nil
}

func decodeConversionManifest(data []byte) (conversionManifest, error) {
	var manifest conversionManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return conversionManifest{}, err
	}
	if manifest.Version != conversionManifestVersion || manifest.SourceTaskID == "" || manifest.TargetTaskID == "" || manifest.StagingDir == "" {
		return conversionManifest{}, fmt.Errorf("invalid conversion manifest")
	}
	return manifest, nil
}

func (m *manager) writeConversionManifest(manifest conversionManifest) error {
	if err := m.writeConversionCoordinator(manifest); err != nil {
		return err
	}
	if manifest.TargetTaskID != manifest.SourceTaskID {
		data, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return fmt.Errorf("conversion: encode manifest: %w", err)
		}
		return writeConversionFile(filepath.Join(m.taskDir(manifest.TargetTaskID), conversionMarkerName), data)
	}
	return nil
}

func (m *manager) writeConversionCoordinator(manifest conversionManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("conversion: encode manifest: %w", err)
	}
	paths := []string{
		filepath.Join(m.taskDir(manifest.SourceTaskID), conversionMarkerName),
		filepath.Join(manifest.StagingDir, conversionMarkerName),
	}
	for _, path := range paths {
		if err := writeConversionFile(path, data); err != nil {
			return err
		}
	}
	return nil
}

func writeConversionFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("conversion: create manifest directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".conversion-*")
	if err != nil {
		return fmt.Errorf("conversion: create manifest: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("conversion: write manifest: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("conversion: close manifest: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("conversion: replace manifest: %w", err)
	}
	return nil
}
