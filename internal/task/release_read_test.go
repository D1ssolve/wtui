package task

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
)

func TestListReleases_ScansOnlyConfiguredRootAndReturnsNewestFirst(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	customReleasesRoot := filepath.Join(rootDir, "custom-releases")

	cfg := &config.Config{
		TasksRoot: tasksRoot,
		RootDir:   rootDir,
		Release: &config.ReleaseConfig{
			RootDir: customReleasesRoot,
		},
	}
	if _, err := cfg.Effective(); err != nil {
		t.Fatalf("cfg.Effective() error = %v", err)
	}
	cfg.TasksRoot = tasksRoot
	cfg.RootDir = rootDir
	cfg.Release.RootDir = customReleasesRoot

	mgr := newTestManagerWithCfg(t, cfg, &mockGitClient{})
	m := mgr.(*manager)

	old := domain.Release{
		ID:        "rel-1.0.0-20260616T120000",
		Status:    domain.ReleaseStatusDraft,
		TaskIDs:   []string{"APP-1"},
		CreatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
	}
	newer := domain.Release{
		ID:        "rel-2.0.0-20260617T120000",
		Status:    domain.ReleaseStatusDraft,
		TaskIDs:   []string{"APP-2"},
		CreatedAt: time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC),
	}

	if _, err := m.writeReleaseManifest(old); err != nil {
		t.Fatalf("writeReleaseManifest(old) error = %v", err)
	}
	if _, err := m.writeReleaseManifest(newer); err != nil {
		t.Fatalf("writeReleaseManifest(newer) error = %v", err)
	}

	defaultRoot := filepath.Join(tasksRoot, ".releases")
	foreignID := "rel-foreign-20260618T120000"
	if err := os.MkdirAll(filepath.Join(defaultRoot, foreignID), 0o755); err != nil {
		t.Fatalf("MkdirAll(foreign) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(defaultRoot, foreignID, releaseManifestFileName), []byte(`{"manifest_version":1,"id":"`+foreignID+`","status":"draft","task_ids":["X"],"created_at":"2026-06-18T12:00:00Z","updated_at":"2026-06-18T12:00:00Z"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(foreign manifest) error = %v", err)
	}

	releases, err := mgr.ListReleases(context.Background())
	if err != nil {
		t.Fatalf("ListReleases() error = %v", err)
	}

	if len(releases) != 2 {
		t.Fatalf("len(releases) = %d, want 2", len(releases))
	}
	if releases[0].ID != newer.ID {
		t.Fatalf("releases[0].ID = %q, want %q", releases[0].ID, newer.ID)
	}
	if releases[1].ID != old.ID {
		t.Fatalf("releases[1].ID = %q, want %q", releases[1].ID, old.ID)
	}
}

func TestGetRelease_ValidatesIDAndReturnsNotFound(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})

	_, err := mgr.GetRelease(context.Background(), "bad/id")
	if !errors.Is(err, ErrReleaseNotFound) {
		t.Fatalf("GetRelease(invalid id) error = %v, want ErrReleaseNotFound", err)
	}

	_, err = mgr.GetRelease(context.Background(), "rel-missing-20260616T120000")
	if !errors.Is(err, ErrReleaseNotFound) {
		t.Fatalf("GetRelease(missing) error = %v, want ErrReleaseNotFound", err)
	}
}

func TestListReleases_ContextCanceledDuringScan_ReturnsContextCanceled(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})
	m := mgr.(*manager)

	for i := range 3 {
		release := domain.Release{
			ID:        fmt.Sprintf("rel-1.2.%d-20260616T120000", i),
			Status:    domain.ReleaseStatusDraft,
			TaskIDs:   []string{"APP-1"},
			CreatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
		}
		if _, err := m.writeReleaseManifest(release); err != nil {
			t.Fatalf("writeReleaseManifest() error = %v", err)
		}
	}

	ctx := &cancelAfterErrChecksContext{cancelAfter: 4}
	_, err := mgr.ListReleases(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ListReleases(canceled) error = %v, want context.Canceled", err)
	}
}

func TestGetRelease_ContextCanceledDuringLoad_ReturnsContextCanceled(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})
	m := mgr.(*manager)

	release := domain.Release{
		ID:        "rel-1.2.3-20260616T120000",
		Status:    domain.ReleaseStatusDraft,
		TaskIDs:   []string{"APP-1"},
		CreatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
	}
	if _, err := m.writeReleaseManifest(release); err != nil {
		t.Fatalf("writeReleaseManifest() error = %v", err)
	}

	ctx := &cancelAfterErrChecksContext{cancelAfter: 3}
	_, err := mgr.GetRelease(ctx, release.ID)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("GetRelease(canceled) error = %v, want context.Canceled", err)
	}
}

func TestRejectRelease_AllowedTransitionsPersistRejected(t *testing.T) {
	tests := []struct {
		name   string
		status domain.ReleaseStatus
	}{
		{name: "draft to rejected", status: domain.ReleaseStatusDraft},
		{name: "prepared to rejected", status: domain.ReleaseStatusPrepared},
		{name: "failed to rejected", status: domain.ReleaseStatusFailed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rootDir := t.TempDir()
			tasksRoot := filepath.Join(rootDir, ".tasks")

			mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})
			m := mgr.(*manager)

			release := domain.Release{
				ID:        "rel-1.2.3-20260616T120000",
				Status:    tc.status,
				TaskIDs:   []string{"APP-1"},
				CreatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
			}

			if _, err := m.writeReleaseManifest(release); err != nil {
				t.Fatalf("writeReleaseManifest() error = %v", err)
			}

			rejected, err := mgr.RejectRelease(context.Background(), release.ID)
			if err != nil {
				t.Fatalf("RejectRelease() error = %v", err)
			}
			if rejected.Status != domain.ReleaseStatusRejected {
				t.Fatalf("rejected.Status = %q, want %q", rejected.Status, domain.ReleaseStatusRejected)
			}
			if rejected.CompletedAt == nil {
				t.Fatalf("rejected.CompletedAt = nil, want non-nil")
			}

			persisted, err := m.loadReleaseManifest(release.ID)
			if err != nil {
				t.Fatalf("loadReleaseManifest() error = %v", err)
			}
			if persisted.Status != domain.ReleaseStatusRejected {
				t.Fatalf("persisted.Status = %q, want %q", persisted.Status, domain.ReleaseStatusRejected)
			}
		})
	}
}

func TestRejectRelease_ForbiddenTransition_LeavesManifestUnchanged(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})
	m := mgr.(*manager)

	release := domain.Release{
		ID:        "rel-1.2.3-20260616T120000",
		Status:    domain.ReleaseStatusReleased,
		TaskIDs:   []string{"APP-1"},
		CreatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 6, 16, 12, 1, 0, 0, time.UTC),
	}

	if _, err := m.writeReleaseManifest(release); err != nil {
		t.Fatalf("writeReleaseManifest() error = %v", err)
	}

	_, err := mgr.RejectRelease(context.Background(), release.ID)
	if !errors.Is(err, ErrReleaseInvalidStatusTransition) {
		t.Fatalf("RejectRelease() error = %v, want ErrReleaseInvalidStatusTransition", err)
	}

	persisted, err := m.loadReleaseManifest(release.ID)
	if err != nil {
		t.Fatalf("loadReleaseManifest() error = %v", err)
	}
	if persisted.Status != domain.ReleaseStatusReleased {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, domain.ReleaseStatusReleased)
	}
	if !persisted.UpdatedAt.Equal(release.UpdatedAt) {
		t.Fatalf("persisted.UpdatedAt = %s, want unchanged %s", persisted.UpdatedAt, release.UpdatedAt)
	}
}

func TestRemoveRelease_NeverRemovesActiveAndRemovesDraftAndTerminal(t *testing.T) {
	tests := []struct {
		name           string
		status         domain.ReleaseStatus
		wantErr        error
		wantDirPresent bool
	}{
		{
			name:           "active validating not removed",
			status:         domain.ReleaseStatusValidating,
			wantErr:        ErrReleaseInvalidStatusTransition,
			wantDirPresent: true,
		},
		{
			name:           "active prepared not removed",
			status:         domain.ReleaseStatusPrepared,
			wantErr:        ErrReleaseInvalidStatusTransition,
			wantDirPresent: true,
		},
		{
			name:           "draft removed",
			status:         domain.ReleaseStatusDraft,
			wantErr:        nil,
			wantDirPresent: false,
		},
		{
			name:           "terminal failed removed",
			status:         domain.ReleaseStatusFailed,
			wantErr:        nil,
			wantDirPresent: false,
		},
		{
			name:           "terminal released removed",
			status:         domain.ReleaseStatusReleased,
			wantErr:        nil,
			wantDirPresent: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rootDir := t.TempDir()
			tasksRoot := filepath.Join(rootDir, ".tasks")

			mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})
			m := mgr.(*manager)

			release := domain.Release{
				ID:        "rel-1.2.3-20260616T120000",
				Status:    tc.status,
				TaskIDs:   []string{"APP-1"},
				CreatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
			}

			if _, err := m.writeReleaseManifest(release); err != nil {
				t.Fatalf("writeReleaseManifest() error = %v", err)
			}

			err := mgr.RemoveRelease(context.Background(), release.ID)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("RemoveRelease() error = %v, want nil", err)
				}
			} else if !errors.Is(err, tc.wantErr) {
				t.Fatalf("RemoveRelease() error = %v, want %v", err, tc.wantErr)
			}

			releaseDir := filepath.Join(m.releasesRootDir(), release.ID)
			_, statErr := os.Stat(releaseDir)
			if tc.wantDirPresent {
				if statErr != nil {
					t.Fatalf("os.Stat(%s) error = %v, want directory present", releaseDir, statErr)
				}
			} else if !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("os.Stat(%s) error = %v, want not exists", releaseDir, statErr)
			}
		})
	}
}

type cancelAfterErrChecksContext struct {
	cancelAfter int
	errChecks   int
}

func (c *cancelAfterErrChecksContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (c *cancelAfterErrChecksContext) Done() <-chan struct{} {
	return nil
}

func (c *cancelAfterErrChecksContext) Err() error {
	c.errChecks++
	if c.errChecks >= c.cancelAfter {
		return context.Canceled
	}
	return nil
}

func (c *cancelAfterErrChecksContext) Value(_ any) any {
	return nil
}
