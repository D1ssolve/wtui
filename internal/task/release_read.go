package task

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/D1ssolve/wtui/internal/domain"
)

func (m *manager) ListReleases(ctx context.Context) ([]domain.Release, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return m.listReleaseManifestsWithContext(ctx)
}

func (m *manager) GetRelease(ctx context.Context, releaseID string) (domain.Release, error) {
	if err := ctx.Err(); err != nil {
		return domain.Release{}, err
	}

	if err := validateReleaseID(releaseID); err != nil {
		return domain.Release{}, fmt.Errorf("%w: %v", ErrReleaseNotFound, err)
	}

	release, err := m.loadReleaseManifestWithContext(ctx, releaseID)
	if err != nil {
		if errors.Is(err, ErrReleaseNotFound) {
			return domain.Release{}, fmt.Errorf("%w: %s", ErrReleaseNotFound, releaseID)
		}
		return domain.Release{}, err
	}

	return release, nil
}

func (m *manager) RejectRelease(ctx context.Context, releaseID string) (domain.Release, error) {
	release, err := m.GetRelease(ctx, releaseID)
	if err != nil {
		return domain.Release{}, err
	}

	if err := validateReleaseStatusTransition(release.Status, domain.ReleaseStatusRejected); err != nil {
		return domain.Release{}, err
	}

	now := defaultReleaseNow().UTC()
	release.Status = domain.ReleaseStatusRejected
	release.UpdatedAt = now
	if release.CompletedAt == nil {
		release.CompletedAt = &now
	}

	return m.writeReleaseManifest(release)
}

func (m *manager) RemoveRelease(ctx context.Context, releaseID string) error {
	release, err := m.GetRelease(ctx, releaseID)
	if err != nil {
		return err
	}

	if release.Status != domain.ReleaseStatusDraft && !isReleaseTerminalStatus(release.Status) {
		return fmt.Errorf("%w: %s -> removed", ErrReleaseInvalidStatusTransition, release.Status)
	}

	releaseDir := filepath.Join(m.releasesRootDir(), releaseID)
	if err := os.RemoveAll(releaseDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrReleaseNotFound, releaseID)
		}
		return fmt.Errorf("remove release dir %q: %w", releaseDir, err)
	}

	return nil
}
