package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/D1ssolve/wtui/internal/domain"
)

const (
	releaseManifestFileName = "release.json"
	releaseManifestVersion  = 1
)

func (m *manager) releasesRootDir() string {
	if m.cfg != nil && m.cfg.Release != nil && m.cfg.Release.RootDir != "" {
		return m.cfg.Release.RootDir
	}

	return filepath.Join(m.cfg.TasksRoot, ".releases")
}

func (m *manager) ensureReleasesRoot() error {
	if err := os.MkdirAll(m.releasesRootDir(), 0o755); err != nil {
		return fmt.Errorf("%w: %v", ErrReleaseManifestInvalid, err)
	}

	return nil
}

func (m *manager) ensureReleaseDir(releaseID string, allowExisting bool) (string, error) {
	if err := validateReleaseID(releaseID); err != nil {
		return "", err
	}

	if err := m.ensureReleasesRoot(); err != nil {
		return "", err
	}

	dir := filepath.Join(m.releasesRootDir(), releaseID)
	if !allowExisting {
		if _, err := os.Stat(dir); err == nil {
			return "", fmt.Errorf("%w: %s", ErrReleaseTargetExists, releaseID)
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("%w: %v", ErrReleaseManifestInvalid, err)
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("%w: %v", ErrReleaseManifestInvalid, err)
	}

	return dir, nil
}

func (m *manager) releaseManifestPath(releaseID string) string {
	return filepath.Join(m.releasesRootDir(), releaseID, releaseManifestFileName)
}

func (m *manager) writeReleaseManifest(release domain.Release) (domain.Release, error) {
	releaseID := release.ID
	if err := validateReleaseID(releaseID); err != nil {
		return domain.Release{}, err
	}

	releaseDir, err := m.ensureReleaseDir(releaseID, true)
	if err != nil {
		return domain.Release{}, err
	}

	now := defaultReleaseNow()
	release.ManifestVersion = releaseManifestVersion
	release.ID = releaseID
	release.Dir = releaseDir
	if release.CreatedAt.IsZero() {
		release.CreatedAt = now
	}
	if release.UpdatedAt.IsZero() {
		release.UpdatedAt = now
	}
	normalizeReleaseTimesUTC(&release)

	manifestPath := filepath.Join(releaseDir, releaseManifestFileName)
	tmpFile, err := os.CreateTemp(releaseDir, "release-*.tmp")
	if err != nil {
		return domain.Release{}, fmt.Errorf("%w: %v", ErrReleaseManifestInvalid, err)
	}
	tmpPath := tmpFile.Name()

	enc := json.NewEncoder(tmpFile)
	enc.SetIndent("", "  ")
	if err := enc.Encode(release); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return domain.Release{}, fmt.Errorf("%w: %v", ErrReleaseManifestInvalid, err)
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return domain.Release{}, fmt.Errorf("%w: %v", ErrReleaseManifestInvalid, err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return domain.Release{}, fmt.Errorf("%w: %v", ErrReleaseManifestInvalid, err)
	}

	if err := os.Rename(tmpPath, manifestPath); err != nil {
		_ = os.Remove(tmpPath)
		return domain.Release{}, fmt.Errorf("%w: %v", ErrReleaseManifestInvalid, err)
	}

	return release, nil
}

func (m *manager) loadReleaseManifest(releaseID string) (domain.Release, error) {
	return m.loadReleaseManifestWithContext(context.Background(), releaseID)
}

func (m *manager) loadReleaseManifestWithContext(ctx context.Context, releaseID string) (domain.Release, error) {
	if err := ctx.Err(); err != nil {
		return domain.Release{}, err
	}

	if err := validateReleaseID(releaseID); err != nil {
		return domain.Release{}, err
	}

	if err := ctx.Err(); err != nil {
		return domain.Release{}, err
	}

	releaseDir := filepath.Join(m.releasesRootDir(), releaseID)
	manifestPath := filepath.Join(releaseDir, releaseManifestFileName)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.Release{}, fmt.Errorf("%w: %s", ErrReleaseNotFound, releaseID)
		}
		return domain.Release{}, fmt.Errorf("%w: %v", ErrReleaseManifestInvalid, err)
	}

	if err := ctx.Err(); err != nil {
		return domain.Release{}, err
	}

	var release domain.Release
	if err := json.Unmarshal(data, &release); err != nil {
		return domain.Release{}, fmt.Errorf("%w: %v", ErrReleaseManifestInvalid, err)
	}

	if err := ctx.Err(); err != nil {
		return domain.Release{}, err
	}

	if release.ManifestVersion != releaseManifestVersion {
		return domain.Release{}, fmt.Errorf("%w: manifest version %d unsupported", ErrReleaseManifestInvalid, release.ManifestVersion)
	}

	if release.ID == "" {
		release.ID = releaseID
	}
	release.Dir = releaseDir
	normalizeReleaseTimesUTC(&release)

	return release, nil
}

func (m *manager) listReleaseManifests() ([]domain.Release, error) {
	return m.listReleaseManifestsWithContext(context.Background())
}

func (m *manager) listReleaseManifestsWithContext(ctx context.Context) ([]domain.Release, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	root := m.releasesRootDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []domain.Release{}, nil
		}
		return nil, fmt.Errorf("%w: %v", ErrReleaseManifestInvalid, err)
	}

	releases := make([]domain.Release, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if !entry.IsDir() {
			continue
		}

		releaseID := entry.Name()
		release, loadErr := m.loadReleaseManifestWithContext(ctx, releaseID)
		if loadErr == nil {
			releases = append(releases, release)
			continue
		}

		if errors.Is(loadErr, context.Canceled) || errors.Is(loadErr, context.DeadlineExceeded) {
			return nil, loadErr
		}

		if !errors.Is(loadErr, ErrReleaseManifestInvalid) {
			if m.logger != nil {
				m.logger.Warn("release list: skipping invalid release entry",
					"release_id", releaseID,
					"error", loadErr.Error(),
				)
			}
			continue
		}

		if err := validateReleaseID(releaseID); err != nil {
			if m.logger != nil {
				m.logger.Warn("release list: corrupt manifest without derivable id, skipping",
					"entry", releaseID,
					"error", loadErr.Error(),
				)
			}
			continue
		}

		releaseDir := filepath.Join(root, releaseID)
		createdAt := defaultReleaseNow()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if info, statErr := os.Stat(releaseDir); statErr == nil {
			createdAt = info.ModTime().UTC()
		}

		releases = append(releases, domain.Release{
			ManifestVersion: releaseManifestVersion,
			ID:              releaseID,
			Dir:             releaseDir,
			Status:          domain.ReleaseStatusFailed,
			CreatedAt:       createdAt,
			UpdatedAt:       createdAt,
			Error: &domain.ReleaseError{
				Code:        "ERR_RELEASE_MANIFEST_INVALID",
				Message:     ErrReleaseManifestInvalid.Error(),
				Stage:       "persist",
				Recoverable: false,
				Cause:       loadErr.Error(),
			},
		})
	}

	sort.Slice(releases, func(i, j int) bool {
		if releases[i].CreatedAt.Equal(releases[j].CreatedAt) {
			return releases[i].ID > releases[j].ID
		}
		return releases[i].CreatedAt.After(releases[j].CreatedAt)
	})

	return releases, nil
}

func normalizeReleaseTimesUTC(release *domain.Release) {
	release.CreatedAt = release.CreatedAt.UTC()
	release.UpdatedAt = release.UpdatedAt.UTC()
	if release.StartedAt != nil {
		v := release.StartedAt.UTC()
		release.StartedAt = &v
	}
	if release.CompletedAt != nil {
		v := release.CompletedAt.UTC()
		release.CompletedAt = &v
	}
}
