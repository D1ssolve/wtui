package task

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestWriteAndLoadReleaseManifest_PersistsVersionAndUTCAndUnknownFields(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})
	m := mgr.(*manager)

	release := domain.Release{
		ID:      "rel-1.2.3-20260616T120000",
		Status:  domain.ReleaseStatusDraft,
		TaskIDs: []string{"APP-1"},
		CreatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0,
			time.FixedZone("UTC+3", 3*60*60)),
		UpdatedAt: time.Date(2026, 6, 16, 12, 1, 0, 0,
			time.FixedZone("UTC+3", 3*60*60)),
	}

	written, err := m.writeReleaseManifest(release)
	if err != nil {
		t.Fatalf("writeReleaseManifest() error = %v", err)
	}

	if written.ManifestVersion != releaseManifestVersion {
		t.Fatalf("ManifestVersion = %d, want %d", written.ManifestVersion, releaseManifestVersion)
	}
	if written.CreatedAt.Location() != time.UTC {
		t.Fatalf("CreatedAt location = %v, want UTC", written.CreatedAt.Location())
	}
	if written.UpdatedAt.Location() != time.UTC {
		t.Fatalf("UpdatedAt location = %v, want UTC", written.UpdatedAt.Location())
	}

	manifestPath := m.releaseManifestPath(release.ID)
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", manifestPath, err)
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("json.Unmarshal(manifest) error = %v", err)
	}

	if _, ok := payload["manifest_version"]; !ok {
		t.Fatalf("manifest JSON missing snake_case key manifest_version")
	}
	if _, ok := payload["created_at"]; !ok {
		t.Fatalf("manifest JSON missing snake_case key created_at")
	}
	if _, ok := payload["updated_at"]; !ok {
		t.Fatalf("manifest JSON missing snake_case key updated_at")
	}
	if _, ok := payload["ManifestVersion"]; ok {
		t.Fatalf("manifest JSON unexpectedly contains non-snake-case key ManifestVersion")
	}

	createdAtRaw, ok := payload["created_at"].(string)
	if !ok || !strings.HasSuffix(createdAtRaw, "Z") {
		t.Fatalf("created_at = %#v, want RFC3339 UTC string", payload["created_at"])
	}
	updatedAtRaw, ok := payload["updated_at"].(string)
	if !ok || !strings.HasSuffix(updatedAtRaw, "Z") {
		t.Fatalf("updated_at = %#v, want RFC3339 UTC string", payload["updated_at"])
	}

	payload["future_field"] = "forward-compatible"
	updatedRaw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal(updated payload) error = %v", err)
	}
	if err := os.WriteFile(manifestPath, updatedRaw, 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", manifestPath, err)
	}

	loaded, err := m.loadReleaseManifest(release.ID)
	if err != nil {
		t.Fatalf("loadReleaseManifest() error = %v", err)
	}
	if loaded.ID != release.ID {
		t.Fatalf("loaded.ID = %q, want %q", loaded.ID, release.ID)
	}
	if loaded.ManifestVersion != releaseManifestVersion {
		t.Fatalf("loaded.ManifestVersion = %d, want %d", loaded.ManifestVersion, releaseManifestVersion)
	}
}

func TestLoadReleaseManifest_Missing_ReturnsErrReleaseNotFound(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})
	m := mgr.(*manager)

	_, err := m.loadReleaseManifest("rel-missing-20260616T120000")
	if !errors.Is(err, ErrReleaseNotFound) {
		t.Fatalf("loadReleaseManifest() error = %v, want ErrReleaseNotFound", err)
	}
}

func TestLoadReleaseManifest_Corrupt_ReturnsErrReleaseManifestInvalid(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})
	m := mgr.(*manager)

	releaseID := "rel-corrupt-20260616T120000"
	releaseDir, err := m.ensureReleaseDir(releaseID, true)
	if err != nil {
		t.Fatalf("ensureReleaseDir() error = %v", err)
	}

	manifestPath := filepath.Join(releaseDir, releaseManifestFileName)
	if err := os.WriteFile(manifestPath, []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", manifestPath, err)
	}

	_, err = m.loadReleaseManifest(releaseID)
	if !errors.Is(err, ErrReleaseManifestInvalid) {
		t.Fatalf("loadReleaseManifest() error = %v, want ErrReleaseManifestInvalid", err)
	}
}

func TestListReleaseManifests_NewestFirstAndCorruptHandling(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})
	m := mgr.(*manager)

	releaseOld := domain.Release{
		ID:        "rel-1.0.0-20260616T120000",
		Status:    domain.ReleaseStatusDraft,
		TaskIDs:   []string{"A"},
		CreatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
	}
	releaseNew := domain.Release{
		ID:        "rel-1.1.0-20260617T120000",
		Status:    domain.ReleaseStatusDraft,
		TaskIDs:   []string{"B"},
		CreatedAt: time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC),
	}

	if _, err := m.writeReleaseManifest(releaseOld); err != nil {
		t.Fatalf("writeReleaseManifest(old) error = %v", err)
	}
	if _, err := m.writeReleaseManifest(releaseNew); err != nil {
		t.Fatalf("writeReleaseManifest(new) error = %v", err)
	}

	corruptID := "rel-corrupt-20260618T120000"
	corruptDir, err := m.ensureReleaseDir(corruptID, true)
	if err != nil {
		t.Fatalf("ensureReleaseDir(corrupt) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(corruptDir, releaseManifestFileName), []byte("{"), 0o644); err != nil {
		t.Fatalf("WriteFile(corrupt manifest) error = %v", err)
	}
	if err := os.Chtimes(corruptDir,
		time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("Chtimes(corruptDir) error = %v", err)
	}

	invalidIDDir := filepath.Join(m.releasesRootDir(), "bad?entry")
	if err := os.MkdirAll(invalidIDDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(invalid id dir) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(invalidIDDir, releaseManifestFileName), []byte("{"), 0o644); err != nil {
		t.Fatalf("WriteFile(invalid id manifest) error = %v", err)
	}

	releases, err := m.listReleaseManifests()
	if err != nil {
		t.Fatalf("listReleaseManifests() error = %v", err)
	}

	if len(releases) != 3 {
		t.Fatalf("len(releases) = %d, want 3 (2 valid + 1 derivable corrupt)", len(releases))
	}

	if releases[0].ID != releaseNew.ID {
		t.Fatalf("releases[0].ID = %q, want newest %q", releases[0].ID, releaseNew.ID)
	}
	if releases[1].ID != releaseOld.ID {
		t.Fatalf("releases[1].ID = %q, want second %q", releases[1].ID, releaseOld.ID)
	}

	corrupt := releases[2]
	if corrupt.ID != corruptID {
		t.Fatalf("corrupt release ID = %q, want %q", corrupt.ID, corruptID)
	}
	if corrupt.Status != domain.ReleaseStatusFailed {
		t.Fatalf("corrupt release status = %q, want %q", corrupt.Status, domain.ReleaseStatusFailed)
	}
	if corrupt.Error == nil || corrupt.Error.Code != "ERR_RELEASE_MANIFEST_INVALID" {
		t.Fatalf("corrupt release error = %#v, want ERR_RELEASE_MANIFEST_INVALID", corrupt.Error)
	}

	for _, rel := range releases {
		if rel.ID == "bad?entry" {
			t.Fatalf("list contains invalid-id entry %q, want skipped", rel.ID)
		}
	}
}

func TestWriteReleaseManifest_AtomicReplaceNoTempLeft(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})
	m := mgr.(*manager)

	releaseID := "rel-atomic-20260616T120000"
	first := domain.Release{
		ID:        releaseID,
		Status:    domain.ReleaseStatusDraft,
		TaskIDs:   []string{"A"},
		CreatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
	}
	second := first
	second.Status = domain.ReleaseStatusReleased
	second.UpdatedAt = time.Date(2026, 6, 16, 12, 5, 0, 0, time.UTC)

	if _, err := m.writeReleaseManifest(first); err != nil {
		t.Fatalf("writeReleaseManifest(first) error = %v", err)
	}
	if _, err := m.writeReleaseManifest(second); err != nil {
		t.Fatalf("writeReleaseManifest(second) error = %v", err)
	}

	loaded, err := m.loadReleaseManifest(releaseID)
	if err != nil {
		t.Fatalf("loadReleaseManifest() error = %v", err)
	}
	if loaded.Status != domain.ReleaseStatusReleased {
		t.Fatalf("loaded.Status = %q, want %q", loaded.Status, domain.ReleaseStatusReleased)
	}

	entries, err := os.ReadDir(filepath.Join(m.releasesRootDir(), releaseID))
	if err != nil {
		t.Fatalf("ReadDir(release dir) error = %v", err)
	}

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "release-") && strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("temporary manifest file leaked after atomic write: %s", entry.Name())
		}
	}
}

func TestListReleaseManifests_MissingRoot_ReturnsEmpty(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})
	m := mgr.(*manager)

	if err := os.RemoveAll(m.releasesRootDir()); err != nil {
		t.Fatalf("RemoveAll(releasesRootDir) error = %v", err)
	}

	releases, err := m.listReleaseManifests()
	if err != nil {
		t.Fatalf("listReleaseManifests() error = %v", err)
	}
	if len(releases) != 0 {
		t.Fatalf("len(releases) = %d, want 0", len(releases))
	}
}

func TestReleaseStore_HelpersCallableFromContext(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})
	m := mgr.(*manager)

	if err := m.ensureReleasesRoot(); err != nil {
		t.Fatalf("ensureReleasesRoot() error = %v", err)
	}

	_, err := m.listReleaseManifests()
	if err != nil {
		t.Fatalf("listReleaseManifests() error = %v", err)
	}

	_ = context.Background()
}

func TestEnsureReleaseDir_ExistingReleaseID_ReturnsErrReleaseTargetExists(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})
	m := mgr.(*manager)

	releaseID := "rel-existing-20260616T120000"
	if _, err := m.ensureReleaseDir(releaseID, true); err != nil {
		t.Fatalf("ensureReleaseDir(create initial) error = %v", err)
	}

	_, err := m.ensureReleaseDir(releaseID, false)
	if !errors.Is(err, ErrReleaseTargetExists) {
		t.Fatalf("ensureReleaseDir(existing) error = %v, want ErrReleaseTargetExists", err)
	}
}
