package task

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// conversionKnownSourceFile reports whether name is a generated artifact
// the conversion itself writes into a source task root and may therefore
// delete during cleanup.
func conversionKnownSourceFile(sourceTaskID, name string) bool {
	switch name {
	case ".DS_Store", conversionMarkerName, sourceTaskID + ".code-workspace", sourceTaskID + ".sln":
		return true
	}
	return false
}

// removeConversionSourceRoot removes the source task directory after a
// successful conversion without recursive deletion. Only known generated
// artifacts and proven-empty service directories (their worktrees were
// already moved or removed) are deleted. Unknown entries — race-injected
// files, replacement service directories — are preserved and block
// completion so the resumable checkpoint stays truthful.
func (m *manager) removeConversionSourceRoot(manifest conversionManifest) error {
	root := m.taskDir(manifest.SourceTaskID)
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("conversion: read source task root: %w", err)
	}
	services := make(map[string]struct{}, len(manifest.Services))
	for _, svc := range manifest.Services {
		services[svc.Name] = struct{}{}
	}

	var preserved []string
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		if conversionKnownSourceFile(manifest.SourceTaskID, entry.Name()) {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				preserved = append(preserved, entry.Name())
			}
			continue
		}
		if entry.IsDir() {
			if _, ok := services[entry.Name()]; ok {
				if err := os.Remove(path); err != nil {
					preserved = append(preserved, entry.Name())
				}
				continue
			}
		}
		preserved = append(preserved, entry.Name())
	}
	if len(preserved) > 0 {
		return fmt.Errorf("conversion: source task root %s still contains preserved entries: %s", root, strings.Join(preserved, ", "))
	}
	if err := os.Remove(root); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("conversion: remove empty source task root: %w", err)
	}
	return nil
}

// removeConversionStagingDir removes a conversion staging directory
// without recursive deletion. Only the conversion marker and proven-empty
// service directories (their staged worktrees were already promoted) are
// deleted; anything else is preserved and blocks completion.
func removeConversionStagingDir(stagingDir string) error {
	entries, err := os.ReadDir(stagingDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("conversion: read staging directory: %w", err)
	}

	var preserved []string
	for _, entry := range entries {
		path := filepath.Join(stagingDir, entry.Name())
		switch {
		case entry.Name() == conversionMarkerName:
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				preserved = append(preserved, entry.Name())
			}
		case entry.IsDir() && entry.Name() == "services":
			subs, err := os.ReadDir(path)
			if err != nil {
				preserved = append(preserved, entry.Name())
				continue
			}
			emptied := true
			for _, sub := range subs {
				if err := os.Remove(filepath.Join(path, sub.Name())); err != nil {
					preserved = append(preserved, "services/"+sub.Name())
					emptied = false
				}
			}
			if emptied {
				if err := os.Remove(path); err != nil {
					preserved = append(preserved, entry.Name())
				}
			}
		default:
			preserved = append(preserved, entry.Name())
		}
	}
	if len(preserved) > 0 {
		return fmt.Errorf("conversion: staging directory %s still contains preserved entries: %s", stagingDir, strings.Join(preserved, ", "))
	}
	if err := os.Remove(stagingDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("conversion: remove empty staging directory: %w", err)
	}
	return nil
}
