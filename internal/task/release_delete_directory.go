package task

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func removeReleaseDeletionDirectory(root string) error {
	// Keep the manifest while pruning, including when new unknown files appear.
	unknown, err := pruneKnownEntries(root, nil)
	if err != nil {
		return err
	}
	if len(unknown) != 1 || unknown[0] != releaseManifestFileName {
		return fmt.Errorf("release directory %s retains unknown entries: %v", root, unknown)
	}
	backup, err := os.CreateTemp(filepath.Dir(root), ".release-delete-*.json")
	if err != nil {
		return err
	}
	path := backup.Name()
	if err := backup.Close(); err != nil {
		return errors.Join(err, os.Remove(path))
	}
	manifest := filepath.Join(root, releaseManifestFileName)
	if err := os.Rename(manifest, path); err != nil {
		return errors.Join(err, os.Remove(path))
	}
	if err := os.Remove(root); err != nil {
		if restoreErr := os.Rename(path, manifest); restoreErr != nil {
			return fmt.Errorf("release manifest retained at %s: %w", path, errors.Join(err, restoreErr))
		}
		return fmt.Errorf("release directory %s could not be removed; manifest restored: %w", root, err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove release recovery manifest %s: %w", path, err)
	}
	return nil
}
