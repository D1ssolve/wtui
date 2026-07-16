package task

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestRetryRelease_InvalidStatuses_ReturnInvalidTransition(t *testing.T) {
	tests := []struct {
		name   string
		status domain.ReleaseStatus
	}{
		{name: "draft", status: domain.ReleaseStatusDraft},
		{name: "prepared", status: domain.ReleaseStatusPrepared},
		{name: "released", status: domain.ReleaseStatusReleased},
		{name: "rejected", status: domain.ReleaseStatusRejected},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := newReleasePlanTestManager(t, &mockGitClient{})
			release := domain.Release{
				ID:        "rel-1.2.3-20260616T120000",
				Status:    tc.status,
				TaskIDs:   []string{"APP-1"},
				CreatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2026, 6, 16, 12, 1, 0, 0, time.UTC),
			}
			if _, err := m.writeReleaseManifest(release); err != nil {
				t.Fatalf("writeReleaseManifest() error = %v", err)
			}

			_, err := m.RetryRelease(context.Background(), release.ID)
			if !errors.Is(err, ErrReleaseInvalidStatusTransition) {
				t.Fatalf("RetryRelease() error = %v, want ErrReleaseInvalidStatusTransition", err)
			}
		})
	}
}

func TestRetryRelease_UnsafeRefMismatch_LeavesManifestUnchanged(t *testing.T) {
	ctx := context.Background()
	gitMock := &mockGitClient{resolveRefFn: func(_ string, ref string) (string, error) {
		if ref == "release/1.2.3" {
			return "different-sha", nil
		}
		return ref + "-sha", nil
	}}
	m, _ := newReleasePlanTestManager(t, gitMock)

	rel := domain.Release{
		ID:         "rel-1.2.3-20260616T120000",
		Status:     domain.ReleaseStatusFailed,
		Checkpoint: "push_branch",
		TaskIDs:    []string{"APP-1"},
		Services: []domain.ReleaseService{
			{
				Name:                "svc-api",
				RepoPath:            filepath.Join(m.cfg.RootDir, "repo-api"),
				IntegrationBranch:   "develop",
				ReleaseBranch:       "release/1.2.3",
				Tag:                 "v1.2.3",
				ReleaseSHA:          "expected-release-sha",
				PushedReleaseBranch: true,
			},
		},
		Error:     &domain.ReleaseError{Code: "ERR_RELEASE_PUSH_FAILED", Recoverable: true},
		CreatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 6, 16, 12, 1, 0, 0, time.UTC),
	}

	if _, err := m.writeReleaseManifest(rel); err != nil {
		t.Fatalf("writeReleaseManifest() error = %v", err)
	}

	gitMock.branchExistsFn = func(_ string, _ string) (bool, error) { return true, nil }

	_, err := m.RetryRelease(ctx, rel.ID)
	if !errors.Is(err, ErrReleaseRetryUnsafe) {
		t.Fatalf("RetryRelease() error = %v, want ErrReleaseRetryUnsafe", err)
	}

	persisted, err := m.loadReleaseManifest(rel.ID)
	if err != nil {
		t.Fatalf("loadReleaseManifest() error = %v", err)
	}
	if persisted.Status != domain.ReleaseStatusFailed {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, domain.ReleaseStatusFailed)
	}
	if persisted.Checkpoint != "push_branch" {
		t.Fatalf("persisted.Checkpoint = %q, want %q", persisted.Checkpoint, "push_branch")
	}
	if persisted.Error == nil || persisted.Error.Code != "ERR_RELEASE_PUSH_FAILED" {
		t.Fatalf("persisted.Error = %#v, want ERR_RELEASE_PUSH_FAILED", persisted.Error)
	}
}

func TestRetryRelease_Stage1Failure_RetriesToPrepared(t *testing.T) {
	ctx := context.Background()
	gitMock := &mockGitClient{}
	m, _ := newReleasePlanTestManager(t, gitMock)

	m.cfg.Release.CreateReleaseWorktrees = testBoolPtr(false)

	rel := domain.Release{
		ID:         "rel-1.2.3-20260616T120000",
		Status:     domain.ReleaseStatusFailed,
		Checkpoint: "push_branch",
		TaskIDs:    []string{"APP-1"},
		Services: []domain.ReleaseService{
			{
				Name:                "svc-api",
				Status:              domain.ReleaseStatusFailed,
				RepoPath:            filepath.Join(m.cfg.RootDir, "repo-api"),
				IntegrationBranch:   "develop",
				ReleaseBranch:       "release/1.2.3",
				Version:             "1.2.3",
				Tag:                 "v1.2.3",
				PreIntegrationRef:   "develop",
				PostIntegrationRef:  "develop",
				PostIntegrationSHA:  "develop-sha",
				ReleaseRef:          "release/1.2.3",
				ReleaseSHA:          "release/1.2.3-sha",
				PushedIntegration:   true,
				PushedReleaseBranch: true,
				FeatureBranches: []domain.ReleaseFeatureBranch{
					{TaskID: "APP-1", ServiceName: "svc-api", Branch: "feature/APP-1", Merged: true, MergeRef: "feature/APP-1"},
				},
			},
		},
		Error:     &domain.ReleaseError{Code: "ERR_RELEASE_PUSH_FAILED", Recoverable: true},
		CreatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 6, 16, 12, 1, 0, 0, time.UTC),
	}

	if _, err := m.writeReleaseManifest(rel); err != nil {
		t.Fatalf("writeReleaseManifest() error = %v", err)
	}

	gitMock.branchExistsFn = func(_ string, _ string) (bool, error) { return true, nil }
	gitMock.tagExistsRes = false
	gitMock.isAncestorFn = func(_, _, _ string) (bool, error) { return true, nil }

	out, err := m.RetryRelease(ctx, rel.ID)
	if err != nil {
		t.Fatalf("RetryRelease() error = %v", err)
	}

	if out.Status != domain.ReleaseStatusPrepared {
		t.Fatalf("out.Status = %q, want %q", out.Status, domain.ReleaseStatusPrepared)
	}
	if out.PreparedAt == nil {
		t.Fatalf("out.PreparedAt = nil, want non-nil")
	}
	if out.CompletedAt != nil {
		t.Fatalf("out.CompletedAt = non-nil, want nil")
	}
	if out.Error != nil {
		t.Fatalf("out.Error = %#v, want nil", out.Error)
	}

	persisted, err := m.loadReleaseManifest(rel.ID)
	if err != nil {
		t.Fatalf("loadReleaseManifest() error = %v", err)
	}
	if persisted.Status != domain.ReleaseStatusPrepared {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, domain.ReleaseStatusPrepared)
	}
	if persisted.Error != nil {
		t.Fatalf("persisted.Error = %#v, want nil", persisted.Error)
	}

	gitMock.mu.Lock()
	defer gitMock.mu.Unlock()
	if gitMock.createTagCalls != 0 {
		t.Fatalf("create tag calls = %d, want 0 (stage-1 retry)", gitMock.createTagCalls)
	}
	if gitMock.pushTagCalls != 0 {
		t.Fatalf("push tag calls = %d, want 0 (stage-1 retry)", gitMock.pushTagCalls)
	}
	if len(gitMock.mergeCalls) != 0 {
		t.Fatalf("merge calls = %d, want 0 (completed service)", len(gitMock.mergeCalls))
	}
	if len(gitMock.createBranchFromBranchCalls) != 0 {
		t.Fatalf("create branch calls = %d, want 0 (completed service)", len(gitMock.createBranchFromBranchCalls))
	}
}

func TestRetryRelease_Stage2Failure_RetriesToReleased(t *testing.T) {
	ctx := context.Background()
	gitMock := &mockGitClient{}
	m, _ := newReleasePlanTestManager(t, gitMock)

	preparedAt := time.Date(2026, 6, 16, 12, 30, 0, 0, time.UTC)
	rel := domain.Release{
		ID:         "rel-1.2.3-20260616T120000",
		Status:     domain.ReleaseStatusFailed,
		Checkpoint: "push_tag",
		TaskIDs:    []string{"APP-1"},
		PreparedAt: &preparedAt,
		Services: []domain.ReleaseService{
			{
				Name:                "svc-api",
				Status:              domain.ReleaseStatusFailed,
				RepoPath:            filepath.Join(m.cfg.RootDir, "repo-api"),
				IntegrationBranch:   "develop",
				ReleaseBranch:       "release/1.2.3",
				Version:             "1.2.3",
				Tag:                 "v1.2.3",
				PreIntegrationRef:   "develop",
				PostIntegrationRef:  "develop",
				PostIntegrationSHA:  "develop-sha",
				ReleaseRef:          "release/1.2.3",
				ReleaseSHA:          "release/1.2.3-sha",
				TagRef:              "v1.2.3",
				TagSHA:              "v1.2.3^{}-sha",
				PushedIntegration:   true,
				PushedReleaseBranch: true,
				PushedTag:           true,
				FeatureBranches: []domain.ReleaseFeatureBranch{
					{TaskID: "APP-1", ServiceName: "svc-api", Branch: "feature/APP-1", Merged: true, MergeRef: "feature/APP-1"},
				},
			},
		},
		Error:     &domain.ReleaseError{Code: "ERR_RELEASE_PUSH_FAILED", Recoverable: true},
		CreatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 6, 16, 12, 1, 0, 0, time.UTC),
	}

	if _, err := m.writeReleaseManifest(rel); err != nil {
		t.Fatalf("writeReleaseManifest() error = %v", err)
	}

	gitMock.branchExistsFn = func(_ string, _ string) (bool, error) { return true, nil }
	gitMock.tagExistsRes = true
	gitMock.isAncestorFn = func(_, _, _ string) (bool, error) { return true, nil }

	out, err := m.RetryRelease(ctx, rel.ID)
	if err != nil {
		t.Fatalf("RetryRelease() error = %v", err)
	}

	if out.Status != domain.ReleaseStatusReleased {
		t.Fatalf("out.Status = %q, want %q", out.Status, domain.ReleaseStatusReleased)
	}
	if out.CompletedAt == nil {
		t.Fatalf("out.CompletedAt = nil, want non-nil")
	}
	if out.Error != nil {
		t.Fatalf("out.Error = %#v, want nil", out.Error)
	}

	persisted, err := m.loadReleaseManifest(rel.ID)
	if err != nil {
		t.Fatalf("loadReleaseManifest() error = %v", err)
	}
	if persisted.Status != domain.ReleaseStatusReleased {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, domain.ReleaseStatusReleased)
	}
	if persisted.Error != nil {
		t.Fatalf("persisted.Error = %#v, want nil", persisted.Error)
	}

	gitMock.mu.Lock()
	defer gitMock.mu.Unlock()
	if len(gitMock.mergeCalls) != 0 {
		t.Fatalf("merge calls = %d, want 0 (stage-2 retry)", len(gitMock.mergeCalls))
	}
	if len(gitMock.createBranchFromBranchCalls) != 0 {
		t.Fatalf("create branch calls = %d, want 0 (stage-2 retry)", len(gitMock.createBranchFromBranchCalls))
	}
	if gitMock.createTagCalls != 0 {
		t.Fatalf("create tag calls = %d, want 0 (completed service)", gitMock.createTagCalls)
	}
}

func TestRetryRelease_PreparedAtDispatch(t *testing.T) {
	ctx := context.Background()

	t.Run("PreparedAt nil dispatches to stage-1 and ends at prepared", func(t *testing.T) {
		gitMock := &mockGitClient{}
		m, _ := newReleasePlanTestManager(t, gitMock)
		m.cfg.Release.CreateReleaseWorktrees = testBoolPtr(false)

		rel := domain.Release{
			ID:         "rel-stage1-20260616T120000",
			Status:     domain.ReleaseStatusFailed,
			Checkpoint: "push_branch",
			TaskIDs:    []string{"APP-1"},
			Services: []domain.ReleaseService{
				{
					Name:                "svc-api",
					Status:              domain.ReleaseStatusFailed,
					RepoPath:            filepath.Join(m.cfg.RootDir, "repo-api"),
					IntegrationBranch:   "develop",
					ReleaseBranch:       "release/1.2.3",
					Version:             "1.2.3",
					Tag:                 "v1.2.3",
					PreIntegrationRef:   "develop",
					PostIntegrationRef:  "develop",
					PostIntegrationSHA:  "develop-sha",
					ReleaseRef:          "release/1.2.3",
					ReleaseSHA:          "release/1.2.3-sha",
					PushedIntegration:   true,
					PushedReleaseBranch: true,
					FeatureBranches: []domain.ReleaseFeatureBranch{
						{TaskID: "APP-1", ServiceName: "svc-api", Branch: "feature/APP-1", Merged: true, MergeRef: "feature/APP-1"},
					},
				},
			},
			Error:     &domain.ReleaseError{Code: "ERR_RELEASE_PUSH_FAILED", Recoverable: true},
			CreatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 6, 16, 12, 1, 0, 0, time.UTC),
		}
		if _, err := m.writeReleaseManifest(rel); err != nil {
			t.Fatalf("writeReleaseManifest() error = %v", err)
		}

		gitMock.branchExistsFn = func(_ string, _ string) (bool, error) { return true, nil }
		gitMock.tagExistsRes = false
		gitMock.isAncestorFn = func(_, _, _ string) (bool, error) { return true, nil }

		out, err := m.RetryRelease(ctx, rel.ID)
		if err != nil {
			t.Fatalf("RetryRelease() error = %v", err)
		}
		if out.Status != domain.ReleaseStatusPrepared {
			t.Fatalf("out.Status = %q, want %q", out.Status, domain.ReleaseStatusPrepared)
		}
	})

	t.Run("PreparedAt set dispatches to stage-2 and ends at released", func(t *testing.T) {
		gitMock := &mockGitClient{}
		m, _ := newReleasePlanTestManager(t, gitMock)
		preparedAt := time.Date(2026, 6, 16, 12, 30, 0, 0, time.UTC)
		rel := domain.Release{
			ID:         "rel-stage2-20260616T120000",
			Status:     domain.ReleaseStatusFailed,
			Checkpoint: "push_tag",
			TaskIDs:    []string{"APP-1"},
			PreparedAt: &preparedAt,
			Services: []domain.ReleaseService{
				{
					Name:                "svc-api",
					Status:              domain.ReleaseStatusFailed,
					RepoPath:            filepath.Join(m.cfg.RootDir, "repo-api"),
					IntegrationBranch:   "develop",
					ReleaseBranch:       "release/1.2.3",
					Version:             "1.2.3",
					Tag:                 "v1.2.3",
					PreIntegrationRef:   "develop",
					PostIntegrationRef:  "develop",
					PostIntegrationSHA:  "develop-sha",
					ReleaseRef:          "release/1.2.3",
					ReleaseSHA:          "release/1.2.3-sha",
					TagRef:              "v1.2.3",
					TagSHA:              "v1.2.3^{}-sha",
					PushedIntegration:   true,
					PushedReleaseBranch: true,
					PushedTag:           true,
					FeatureBranches: []domain.ReleaseFeatureBranch{
						{TaskID: "APP-1", ServiceName: "svc-api", Branch: "feature/APP-1", Merged: true, MergeRef: "feature/APP-1"},
					},
				},
			},
			Error:     &domain.ReleaseError{Code: "ERR_RELEASE_PUSH_FAILED", Recoverable: true},
			CreatedAt: time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 6, 16, 12, 1, 0, 0, time.UTC),
		}
		if _, err := m.writeReleaseManifest(rel); err != nil {
			t.Fatalf("writeReleaseManifest() error = %v", err)
		}

		gitMock.branchExistsFn = func(_ string, _ string) (bool, error) { return true, nil }
		gitMock.tagExistsRes = true
		gitMock.isAncestorFn = func(_, _, _ string) (bool, error) { return true, nil }

		out, err := m.RetryRelease(ctx, rel.ID)
		if err != nil {
			t.Fatalf("RetryRelease() error = %v", err)
		}
		if out.Status != domain.ReleaseStatusReleased {
			t.Fatalf("out.Status = %q, want %q", out.Status, domain.ReleaseStatusReleased)
		}
	})
}

