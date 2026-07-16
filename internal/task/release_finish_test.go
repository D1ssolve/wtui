package task

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/domain"
)

func newFinishTestManager(t *testing.T) (*manager, *mockGitClient) {
	t.Helper()
	gitMock := &mockGitClient{branchExistsRes: true}
	m, _ := newReleasePlanTestManager(t, gitMock)
	return m, gitMock
}

func writeRelease(t *testing.T, m *manager, status domain.ReleaseStatus, svc domain.ReleaseService) domain.Release {
	t.Helper()

	if svc.Name == "" {
		t.Fatalf("service name required")
	}
	if svc.Version == "" {
		svc.Version = "1.2.3"
	}
	if svc.Tag == "" {
		svc.Tag = formatReleaseTag(m.cfg, svc.Version)
	}
	if svc.ReleaseBranch == "" {
		svc.ReleaseBranch = releaseBranchName(m.flow, svc.Version)
	}

	fixed := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	oldNow := defaultReleaseNow
	defaultReleaseNow = func() time.Time { return fixed }
	defer func() { defaultReleaseNow = oldNow }()

	releaseID, err := generateReleaseID(m.cfg.Release.IDFormat, svc.Version, defaultReleaseNow)
	if err != nil {
		t.Fatalf("generateReleaseID error = %v", err)
	}

	release := domain.Release{
		ID:         releaseID,
		Status:     status,
		Checkpoint: string(status),
		Version:    svc.Version,
		Tag:        svc.Tag,
		TaskIDs:    []string{"FIN-1"},
		Services:   []domain.ReleaseService{svc},
		CreatedAt:  fixed,
		UpdatedAt:  fixed,
	}
	if status == domain.ReleaseStatusPrepared {
		release.PreparedAt = &fixed
	}

	release, err = m.writeReleaseManifest(release)
	if err != nil {
		t.Fatalf("writeReleaseManifest error = %v", err)
	}
	return release
}

func TestFinishRelease_HappyPath(t *testing.T) {
	ctx := context.Background()
	m, gitMock := newFinishTestManager(t)
	repoPath := filepath.Join(m.cfg.RootDir, "repo-api")

	release := writeRelease(t, m, domain.ReleaseStatusPrepared, domain.ReleaseService{
		Name:                "svc-api",
		RepoPath:            repoPath,
		Version:             "1.2.3",
		ReleaseSHA:          "release/1.2.3-sha",
		PushedReleaseBranch: true,
	})

	result, err := m.FinishRelease(ctx, FinishReleaseParams{ReleaseID: release.ID})
	if err != nil {
		t.Fatalf("FinishRelease() error = %v", err)
	}
	if result.Status != domain.ReleaseStatusReleased {
		t.Fatalf("release status = %q, want %q", result.Status, domain.ReleaseStatusReleased)
	}
	if result.CompletedAt == nil {
		t.Fatalf("release CompletedAt=nil, want non-nil")
	}
	if result.Error != nil {
		t.Fatalf("release error = %#v, want nil", result.Error)
	}
	if len(result.Services) != 1 {
		t.Fatalf("len(services) = %d, want 1", len(result.Services))
	}

	svc := result.Services[0]
	if svc.Status != domain.ReleaseStatusReleased {
		t.Fatalf("service status = %q, want %q", svc.Status, domain.ReleaseStatusReleased)
	}
	if svc.TagRef != "v1.2.3" {
		t.Fatalf("service TagRef = %q, want %q", svc.TagRef, "v1.2.3")
	}
	if svc.TagSHA != "v1.2.3^{}-sha" {
		t.Fatalf("service TagSHA = %q, want %q", svc.TagSHA, "v1.2.3^{}-sha")
	}
	if !svc.PushedTag {
		t.Fatalf("service PushedTag = %v, want true", svc.PushedTag)
	}

	if gitMock.createTagCalls != 1 {
		t.Fatalf("CreateTag calls = %d, want 1", gitMock.createTagCalls)
	}
	if gitMock.pushTagCalls != 1 {
		t.Fatalf("PushTag calls = %d, want 1", gitMock.pushTagCalls)
	}
	if len(gitMock.createTagCallList) != 1 {
		t.Fatalf("CreateTag call records = %d, want 1", len(gitMock.createTagCallList))
	}
	if got := gitMock.createTagCallList[0].Message; got != "wtui release "+release.ID {
		t.Fatalf("CreateTag message = %q, want %q", got, "wtui release "+release.ID)
	}
	if len(gitMock.pushTagCallList) != 1 {
		t.Fatalf("PushTag call records = %d, want 1", len(gitMock.pushTagCallList))
	}
	if got := gitMock.pushTagCallList[0].RepoPath; got != repoPath {
		t.Fatalf("PushTag repo path = %q, want %q", got, repoPath)
	}
}

func TestFinishRelease_NotPrepared_Rejects(t *testing.T) {
	ctx := context.Background()
	statuses := []domain.ReleaseStatus{
		domain.ReleaseStatusDraft,
		domain.ReleaseStatusValidating,
		domain.ReleaseStatusMerging,
		domain.ReleaseStatusBranching,
		domain.ReleaseStatusPushing,
		domain.ReleaseStatusTagging,
		domain.ReleaseStatusReleased,
		domain.ReleaseStatusFailed,
		domain.ReleaseStatusRejected,
	}

	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			m, gitMock := newFinishTestManager(t)
			release := writeRelease(t, m, status, domain.ReleaseService{
				Name:       "svc-api",
				RepoPath:   filepath.Join(m.cfg.RootDir, "repo-api"),
				Version:    "1.2.3",
				ReleaseSHA: "release/1.2.3-sha",
			})

			_, err := m.FinishRelease(ctx, FinishReleaseParams{ReleaseID: release.ID})
			if !errors.Is(err, ErrReleaseInvalidStatusTransition) {
				t.Fatalf("FinishRelease() error = %v, want ErrReleaseInvalidStatusTransition", err)
			}
			if gitMock.createTagCalls != 0 {
				t.Fatalf("CreateTag calls = %d, want 0", gitMock.createTagCalls)
			}
			if gitMock.pushTagCalls != 0 {
				t.Fatalf("PushTag calls = %d, want 0", gitMock.pushTagCalls)
			}
		})
	}
}

func TestFinishRelease_TagCreationFails(t *testing.T) {
	ctx := context.Background()
	m, gitMock := newFinishTestManager(t)
	gitMock.createTagErr = errors.New("tag create failed")

	release := writeRelease(t, m, domain.ReleaseStatusPrepared, domain.ReleaseService{
		Name:                "svc-api",
		RepoPath:            filepath.Join(m.cfg.RootDir, "repo-api"),
		Version:             "1.2.3",
		ReleaseSHA:          "release/1.2.3-sha",
		PushedReleaseBranch: true,
	})

	result, err := m.FinishRelease(ctx, FinishReleaseParams{ReleaseID: release.ID})
	if err == nil {
		t.Fatalf("FinishRelease() error=nil, want non-nil")
	}
	if !errors.Is(err, ErrReleaseTagCreateFailed) {
		t.Fatalf("FinishRelease() error = %v, want ErrReleaseTagCreateFailed", err)
	}
	if result.Status != domain.ReleaseStatusFailed {
		t.Fatalf("release status = %q, want %q", result.Status, domain.ReleaseStatusFailed)
	}
	if result.Error == nil {
		t.Fatalf("release error=nil, want non-nil")
	}
	if result.Error.Stage != "tag" {
		t.Fatalf("release error stage = %q, want %q", result.Error.Stage, "tag")
	}
	if !result.Error.Recoverable {
		t.Fatalf("release error Recoverable = %v, want true", result.Error.Recoverable)
	}
	if result.Error.Code != "ERR_RELEASE_TAG_CREATE" {
		t.Fatalf("release error code = %q, want %q", result.Error.Code, "ERR_RELEASE_TAG_CREATE")
	}
	if gitMock.createTagCalls != 1 {
		t.Fatalf("CreateTag calls = %d, want 1", gitMock.createTagCalls)
	}
	if gitMock.pushTagCalls != 0 {
		t.Fatalf("PushTag calls = %d, want 0", gitMock.pushTagCalls)
	}
}

func TestFinishRelease_TagPushFails(t *testing.T) {
	ctx := context.Background()
	m, gitMock := newFinishTestManager(t)
	gitMock.pushTagErr = errors.New("push tag failed")

	release := writeRelease(t, m, domain.ReleaseStatusPrepared, domain.ReleaseService{
		Name:                "svc-api",
		RepoPath:            filepath.Join(m.cfg.RootDir, "repo-api"),
		Version:             "1.2.3",
		ReleaseSHA:          "release/1.2.3-sha",
		PushedReleaseBranch: true,
	})

	result, err := m.FinishRelease(ctx, FinishReleaseParams{ReleaseID: release.ID})
	if err == nil {
		t.Fatalf("FinishRelease() error=nil, want non-nil")
	}
	if !errors.Is(err, ErrReleaseTagPushFailed) {
		t.Fatalf("FinishRelease() error = %v, want ErrReleaseTagPushFailed", err)
	}
	if result.Status != domain.ReleaseStatusFailed {
		t.Fatalf("release status = %q, want %q", result.Status, domain.ReleaseStatusFailed)
	}
	if result.Error == nil {
		t.Fatalf("release error=nil, want non-nil")
	}
	if result.Error.Stage != "push_tag" {
		t.Fatalf("release error stage = %q, want %q", result.Error.Stage, "push_tag")
	}
	if !result.Error.Recoverable {
		t.Fatalf("release error Recoverable = %v, want true", result.Error.Recoverable)
	}
	if result.Error.Code != "ERR_RELEASE_TAG_PUSH" {
		t.Fatalf("release error code = %q, want %q", result.Error.Code, "ERR_RELEASE_TAG_PUSH")
	}
	if gitMock.createTagCalls != 1 {
		t.Fatalf("CreateTag calls = %d, want 1", gitMock.createTagCalls)
	}
	if gitMock.pushTagCalls != 1 {
		t.Fatalf("PushTag calls = %d, want 1", gitMock.pushTagCalls)
	}
}

func TestFinishRelease_IdempotentRetry(t *testing.T) {
	ctx := context.Background()
	m, gitMock := newFinishTestManager(t)

	release := writeRelease(t, m, domain.ReleaseStatusPrepared, domain.ReleaseService{
		Name:                "svc-api",
		RepoPath:            filepath.Join(m.cfg.RootDir, "repo-api"),
		Version:             "1.2.3",
		ReleaseSHA:          "release/1.2.3-sha",
		PushedReleaseBranch: true,
		PushedTag:           true,
		TagRef:              "v1.2.3",
	})

	result, err := m.FinishRelease(ctx, FinishReleaseParams{ReleaseID: release.ID})
	if err != nil {
		t.Fatalf("FinishRelease() error = %v", err)
	}
	if result.Status != domain.ReleaseStatusReleased {
		t.Fatalf("release status = %q, want %q", result.Status, domain.ReleaseStatusReleased)
	}
	if result.CompletedAt == nil {
		t.Fatalf("release CompletedAt=nil, want non-nil")
	}
	if gitMock.createTagCalls != 0 {
		t.Fatalf("CreateTag calls = %d, want 0", gitMock.createTagCalls)
	}
	if gitMock.pushTagCalls != 0 {
		t.Fatalf("PushTag calls = %d, want 0", gitMock.pushTagCalls)
	}
	if !result.Services[0].PushedTag {
		t.Fatalf("service PushedTag = %v, want true", result.Services[0].PushedTag)
	}
}

func TestFinishRelease_SHADrift_Rejects(t *testing.T) {
	ctx := context.Background()
	m, gitMock := newFinishTestManager(t)
	gitMock.resolveRefFn = func(repoPath, ref string) (string, error) {
		if ref == "release/1.2.3" {
			return "drifted-sha", nil
		}
		return ref + "-sha", nil
	}

	release := writeRelease(t, m, domain.ReleaseStatusPrepared, domain.ReleaseService{
		Name:                "svc-api",
		RepoPath:            filepath.Join(m.cfg.RootDir, "repo-api"),
		Version:             "1.2.3",
		ReleaseSHA:          "release/1.2.3-sha",
		PushedReleaseBranch: true,
	})

	result, err := m.FinishRelease(ctx, FinishReleaseParams{ReleaseID: release.ID})
	if !errors.Is(err, ErrReleaseRetryUnsafe) {
		t.Fatalf("FinishRelease() error = %v, want ErrReleaseRetryUnsafe", err)
	}
	if gitMock.createTagCalls != 0 {
		t.Fatalf("CreateTag calls = %d, want 0", gitMock.createTagCalls)
	}
	if gitMock.pushTagCalls != 0 {
		t.Fatalf("PushTag calls = %d, want 0", gitMock.pushTagCalls)
	}
	if result.Status != domain.ReleaseStatusFailed {
		t.Fatalf("release status = %q, want %q", result.Status, domain.ReleaseStatusFailed)
	}
}

func TestFinishRelease_TagAlreadyExists_Rejects(t *testing.T) {
	ctx := context.Background()
	m, gitMock := newFinishTestManager(t)
	gitMock.tagExistsRes = true

	release := writeRelease(t, m, domain.ReleaseStatusPrepared, domain.ReleaseService{
		Name:                "svc-api",
		RepoPath:            filepath.Join(m.cfg.RootDir, "repo-api"),
		Version:             "1.2.3",
		ReleaseSHA:          "release/1.2.3-sha",
		PushedReleaseBranch: true,
	})

	result, err := m.FinishRelease(ctx, FinishReleaseParams{ReleaseID: release.ID})
	if !errors.Is(err, ErrReleaseRetryUnsafe) {
		t.Fatalf("FinishRelease() error = %v, want ErrReleaseRetryUnsafe", err)
	}
	if gitMock.createTagCalls != 0 {
		t.Fatalf("CreateTag calls = %d, want 0", gitMock.createTagCalls)
	}
	if gitMock.pushTagCalls != 0 {
		t.Fatalf("PushTag calls = %d, want 0", gitMock.pushTagCalls)
	}
	if result.Status != domain.ReleaseStatusFailed {
		t.Fatalf("release status = %q, want %q", result.Status, domain.ReleaseStatusFailed)
	}
}

func TestFinishRelease_FetchFailure_PersistsFailedManifest_ReturnsLoadedRelease(t *testing.T) {
	ctx := context.Background()
	m, gitMock := newFinishTestManager(t)
	fetchErr := errors.New("fetch boom")
	gitMock.fetchErr = fetchErr

	release := writeRelease(t, m, domain.ReleaseStatusPrepared, domain.ReleaseService{
		Name:                "svc-api",
		RepoPath:            filepath.Join(m.cfg.RootDir, "repo-api"),
		Version:             "1.2.3",
		ReleaseSHA:          "release/1.2.3-sha",
		PushedReleaseBranch: true,
	})

	result, err := m.FinishRelease(ctx, FinishReleaseParams{ReleaseID: release.ID})
	if err == nil {
		t.Fatalf("FinishRelease() error=nil, want non-nil")
	}
	if !errors.Is(err, ErrReleaseOperationInProgress) {
		t.Fatalf("FinishRelease() error = %v, want ErrReleaseOperationInProgress", err)
	}
	if !errors.Is(err, fetchErr) {
		t.Fatalf("FinishRelease() error = %v, does not wrap injected fetchErr", err)
	}
	if result.ID != release.ID {
		t.Fatalf("result.ID = %q, want %q", result.ID, release.ID)
	}
	if result.Status != domain.ReleaseStatusFailed {
		t.Fatalf("release status = %q, want %q", result.Status, domain.ReleaseStatusFailed)
	}
	if result.Error == nil {
		t.Fatalf("release error=nil, want non-nil")
	}
	if result.Error.Stage != "finish_fetch" {
		t.Fatalf("release error stage = %q, want %q", result.Error.Stage, "finish_fetch")
	}
	if !result.Error.Recoverable {
		t.Fatalf("release error Recoverable = %v, want true", result.Error.Recoverable)
	}
	if result.Error.Code != "ERR_RELEASE_FETCH" {
		t.Fatalf("release error code = %q, want %q", result.Error.Code, "ERR_RELEASE_FETCH")
	}

	loaded, loadErr := m.loadReleaseManifest(release.ID)
	if loadErr != nil {
		t.Fatalf("loadReleaseManifest error = %v", loadErr)
	}
	if loaded.Status != domain.ReleaseStatusFailed {
		t.Fatalf("loaded release status = %q, want %q", loaded.Status, domain.ReleaseStatusFailed)
	}
	if loaded.Error == nil || loaded.Error.Stage != "finish_fetch" {
		t.Fatalf("loaded release error stage = %q, want finish_fetch", loaded.Error.Stage)
	}
	if !loaded.Error.Recoverable {
		t.Fatalf("loaded release error Recoverable = %v, want true", loaded.Error.Recoverable)
	}
}

func TestFinishRelease_TagCreateFailure_ReachedTaggingCheckpoint(t *testing.T) {
	ctx := context.Background()
	m, gitMock := newFinishTestManager(t)
	createErr := errors.New("tag create failed")
	gitMock.createTagErr = createErr

	release := writeRelease(t, m, domain.ReleaseStatusPrepared, domain.ReleaseService{
		Name:                "svc-api",
		RepoPath:            filepath.Join(m.cfg.RootDir, "repo-api"),
		Version:             "1.2.3",
		ReleaseSHA:          "release/1.2.3-sha",
		PushedReleaseBranch: true,
	})

	gitMock.createTagFn = func(repoPath, tag, target, message string) error {
		loaded, loadErr := m.loadReleaseManifest(release.ID)
		if loadErr != nil {
			t.Errorf("load manifest during CreateTag: %v", loadErr)
			return createErr
		}
		if loaded.Status != domain.ReleaseStatusTagging {
			t.Errorf("manifest status during CreateTag = %q, want %q", loaded.Status, domain.ReleaseStatusTagging)
		}
		if loaded.Checkpoint != "tagging" {
			t.Errorf("manifest checkpoint during CreateTag = %q, want %q", loaded.Checkpoint, "tagging")
		}
		return createErr
	}

	result, err := m.FinishRelease(ctx, FinishReleaseParams{ReleaseID: release.ID})
	if err == nil {
		t.Fatalf("FinishRelease() error=nil, want non-nil")
	}
	if !errors.Is(err, ErrReleaseTagCreateFailed) {
		t.Fatalf("FinishRelease() error = %v, want ErrReleaseTagCreateFailed", err)
	}
	if result.Status != domain.ReleaseStatusFailed {
		t.Fatalf("release status = %q, want %q", result.Status, domain.ReleaseStatusFailed)
	}
	if result.Error == nil || result.Error.Stage != "tag" {
		t.Fatalf("release error stage = %q, want tag", result.Error.Stage)
	}
}

func TestFinishRelease_TagPushFailure_ReachedPushingCheckpoint(t *testing.T) {
	ctx := context.Background()
	m, gitMock := newFinishTestManager(t)
	pushErr := errors.New("push tag failed")
	gitMock.pushTagErr = pushErr

	release := writeRelease(t, m, domain.ReleaseStatusPrepared, domain.ReleaseService{
		Name:                "svc-api",
		RepoPath:            filepath.Join(m.cfg.RootDir, "repo-api"),
		Version:             "1.2.3",
		ReleaseSHA:          "release/1.2.3-sha",
		PushedReleaseBranch: true,
	})

	gitMock.pushTagFn = func(repoPath, tag string) error {
		loaded, loadErr := m.loadReleaseManifest(release.ID)
		if loadErr != nil {
			t.Errorf("load manifest during PushTag: %v", loadErr)
			return pushErr
		}
		if loaded.Status != domain.ReleaseStatusPushing {
			t.Errorf("manifest status during PushTag = %q, want %q", loaded.Status, domain.ReleaseStatusPushing)
		}
		if loaded.Checkpoint != "pushing" {
			t.Errorf("manifest checkpoint during PushTag = %q, want %q", loaded.Checkpoint, "pushing")
		}
		return pushErr
	}

	result, err := m.FinishRelease(ctx, FinishReleaseParams{ReleaseID: release.ID})
	if err == nil {
		t.Fatalf("FinishRelease() error=nil, want non-nil")
	}
	if !errors.Is(err, ErrReleaseTagPushFailed) {
		t.Fatalf("FinishRelease() error = %v, want ErrReleaseTagPushFailed", err)
	}
	if result.Status != domain.ReleaseStatusFailed {
		t.Fatalf("release status = %q, want %q", result.Status, domain.ReleaseStatusFailed)
	}
	if result.Error == nil || result.Error.Stage != "push_tag" {
		t.Fatalf("release error stage = %q, want push_tag", result.Error.Stage)
	}
}

func TestFinishRelease_Success_TransitionsToReleased(t *testing.T) {
	ctx := context.Background()
	m, gitMock := newFinishTestManager(t)

	release := writeRelease(t, m, domain.ReleaseStatusPrepared, domain.ReleaseService{
		Name:                "svc-api",
		RepoPath:            filepath.Join(m.cfg.RootDir, "repo-api"),
		Version:             "1.2.3",
		ReleaseSHA:          "release/1.2.3-sha",
		PushedReleaseBranch: true,
	})

	result, err := m.FinishRelease(ctx, FinishReleaseParams{ReleaseID: release.ID})
	if err != nil {
		t.Fatalf("FinishRelease() error = %v", err)
	}
	if result.Status != domain.ReleaseStatusReleased {
		t.Fatalf("release status = %q, want %q", result.Status, domain.ReleaseStatusReleased)
	}
	if result.CompletedAt == nil {
		t.Fatalf("release CompletedAt=nil, want non-nil")
	}
	if result.Error != nil {
		t.Fatalf("release error = %#v, want nil", result.Error)
	}
	if gitMock.createTagCalls != 1 {
		t.Fatalf("CreateTag calls = %d, want 1", gitMock.createTagCalls)
	}
	if gitMock.pushTagCalls != 1 {
		t.Fatalf("PushTag calls = %d, want 1", gitMock.pushTagCalls)
	}
}
