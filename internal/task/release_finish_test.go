package task

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/domain"
)

func newFinishTestManager(t *testing.T) (*manager, *mockGitClient) {
	t.Helper()
	gitMock := &mockGitClient{branchExistsRes: true, commonDirResult: "/git/common"}
	m, _ := newReleasePlanTestManager(t, gitMock)
	m.flow.ProductionBranch = "master"
	gitMock.isAncestorFn = func(_, _, _ string) (bool, error) { return false, nil }
	return m, gitMock
}

func writeRelease(t *testing.T, m *manager, status domain.ReleaseStatus, svc domain.ReleaseService) domain.Release {
	t.Helper()
	if svc.Version == "" {
		svc.Version = "1.2.3"
	}
	if svc.Tag == "" {
		svc.Tag = formatReleaseTag(m.cfg, svc.Version)
	}
	if svc.ReleaseBranch == "" {
		svc.ReleaseBranch = releaseBranchName(m.flow, svc.Version)
	}
	if svc.IntegrationBranch == "" {
		svc.IntegrationBranch = m.flow.IntegrationBranch
	}

	fixed := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	release := domain.Release{
		ID:         "rel-1.2.3-20260616T120000",
		Status:     status,
		Checkpoint: string(status),
		Version:    svc.Version,
		Tag:        svc.Tag,
		TaskIDs:    []string{"FIN-1"},
		Services:   []domain.ReleaseService{svc},
		CreatedAt:  fixed,
		UpdatedAt:  fixed,
	}
	release, err := m.writeReleaseManifest(release)
	if err != nil {
		t.Fatalf("writeReleaseManifest error = %v", err)
	}
	return release
}

func finalizeService(m *manager) domain.ReleaseService {
	return domain.ReleaseService{
		Name:              "svc-api",
		RepoPath:          filepath.Join(m.cfg.RootDir, "repo-api"),
		Version:           "1.2.3",
		ReleaseBranch:     "release/1.2.3",
		IntegrationBranch: "develop",
		AcceptedMergeSHA:  "accepted-sha",
		Status:            domain.ReleaseStatusMasterMerged,
	}
}

func matchingMaster(gitMock *mockGitClient) {
	gitMock.resolveRefFn = func(_ string, ref string) (string, error) {
		switch ref {
		case "origin/master", "v1.2.3^{}":
			return "accepted-sha", nil
		default:
			return ref + "-sha", nil
		}
	}
}

func TestFinalizeRelease_HappyPath_MergesDevelopAndTagsAcceptedMasterSHA(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	matchingMaster(gitMock)
	release := writeRelease(t, m, domain.ReleaseStatusMasterMerged, finalizeService(m))

	got, err := m.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if err != nil {
		t.Fatalf("FinalizeRelease() error = %v", err)
	}
	if got.Status != domain.ReleaseStatusReleased || got.CompletedAt == nil || !got.Services[0].PushedTag {
		t.Fatalf("release = %#v", got)
	}
	if len(gitMock.mergeFFOnlyCalls) != 1 || gitMock.mergeFFOnlyCalls[0].Ref != "origin/develop" {
		t.Fatalf("MergeFFOnly calls = %#v", gitMock.mergeFFOnlyCalls)
	}
	if len(gitMock.mergeCalls) != 1 || gitMock.mergeCalls[0].Branch != "release/1.2.3" {
		t.Fatalf("Merge calls = %#v", gitMock.mergeCalls)
	}
	if len(gitMock.createTagCallList) != 1 || gitMock.createTagCallList[0].Target != "accepted-sha" {
		t.Fatalf("CreateTag calls = %#v", gitMock.createTagCallList)
	}
	if len(gitMock.pushBranchExplicitCalls) != 1 || gitMock.pushTagCalls != 1 {
		t.Fatalf("push integration = %#v, push tags = %d", gitMock.pushBranchExplicitCalls, gitMock.pushTagCalls)
	}
}

func TestFinalizeRelease_MasterMoved_FailsWithoutTag(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	gitMock.resolveRefRes = "new-master-sha"
	release := writeRelease(t, m, domain.ReleaseStatusMasterMerged, finalizeService(m))

	got, err := m.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if !errors.Is(err, ErrReleaseMasterMoved) || gitMock.createTagCalls != 0 {
		t.Fatalf("error = %v, CreateTag calls = %d", err, gitMock.createTagCalls)
	}
	if got.Status != domain.ReleaseStatusFailed || got.Error == nil || got.Error.Code != "ERR_RELEASE_MASTER_MOVED" {
		t.Fatalf("release = %#v", got)
	}
}

func TestFinalizeRelease_DevelopAlreadyContainsRelease_SkipsMerge(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	matchingMaster(gitMock)
	gitMock.isAncestorFn = func(_, _, _ string) (bool, error) { return true, nil }
	release := writeRelease(t, m, domain.ReleaseStatusMasterMerged, finalizeService(m))

	_, err := m.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if err != nil || len(gitMock.mergeCalls) != 0 || gitMock.createTagCalls != 1 {
		t.Fatalf("error = %v, Merge calls = %#v, CreateTag calls = %d", err, gitMock.mergeCalls, gitMock.createTagCalls)
	}
}

func TestFinalizeRelease_PushIntegrationDisabledKeepsDetachedMerge(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	matchingMaster(gitMock)
	*m.cfg.Release.PushIntegration = false
	release := writeRelease(t, m, domain.ReleaseStatusMasterMerged, finalizeService(m))

	got, err := m.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got.Services[0].IntegrationWorktreePath == "" {
		t.Fatal("IntegrationWorktreePath empty, detached merge would become unreachable")
	}
	if got.Services[0].PostIntegrationRef == "" || got.Services[0].PostIntegrationRef != got.Services[0].PostIntegrationSHA {
		t.Fatalf("PostIntegrationRef = %q, PostIntegrationSHA = %q; want retained detached SHA", got.Services[0].PostIntegrationRef, got.Services[0].PostIntegrationSHA)
	}
	if len(gitMock.removeWorktreeCalls) != 0 || len(gitMock.pushBranchExplicitCalls) != 0 {
		t.Fatalf("remove calls = %#v, push calls = %#v", gitMock.removeWorktreeCalls, gitMock.pushBranchExplicitCalls)
	}
}

func TestFinalizeRelease_RetainedWorktreeRemovalFailureStops(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	matchingMaster(gitMock)
	removeErr := errors.New("remove retained worktree")
	gitMock.removeWorktreeErr = removeErr
	svc := finalizeService(m)
	svc.IntegrationWorktreePath = "/old/integration-worktree"
	release := writeRelease(t, m, domain.ReleaseStatusMasterMerged, svc)

	got, err := m.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if !errors.Is(err, removeErr) {
		t.Fatalf("FinalizeRelease() error = %v, want removal error", err)
	}
	if got.Status != domain.ReleaseStatusFailed || len(gitMock.addWorktreeCalls) != 0 {
		t.Fatalf("release status = %q, add worktree calls = %#v", got.Status, gitMock.addWorktreeCalls)
	}
}

func TestFinalizeRelease_ExistingTagAtAcceptedSHA_IsIdempotent(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	matchingMaster(gitMock)
	gitMock.tagExistsRes = true
	release := writeRelease(t, m, domain.ReleaseStatusMasterMerged, finalizeService(m))

	got, err := m.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if err != nil || gitMock.createTagCalls != 0 || gitMock.pushTagCalls != 1 || got.Services[0].TagSHA != "accepted-sha" {
		t.Fatalf("error = %v, release = %#v, create = %d, push = %d", err, got, gitMock.createTagCalls, gitMock.pushTagCalls)
	}
}

func TestFinalizeRelease_ExistingTagAtDifferentSHA_IsUnsafe(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	matchingMaster(gitMock)
	gitMock.tagExistsRes = true
	gitMock.resolveRefFn = func(_ string, ref string) (string, error) {
		if ref == "origin/master" {
			return "accepted-sha", nil
		}
		return "different-sha", nil
	}
	release := writeRelease(t, m, domain.ReleaseStatusMasterMerged, finalizeService(m))

	got, err := m.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if !errors.Is(err, ErrReleaseRetryUnsafe) || gitMock.createTagCalls != 0 || got.Status != domain.ReleaseStatusFailed {
		t.Fatalf("error = %v, CreateTag calls = %d, status = %s", err, gitMock.createTagCalls, got.Status)
	}
}

func TestFinalizeRelease_DevelopConflict_AbortsFailsAndCleansWorktree(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	matchingMaster(gitMock)
	gitMock.mergeErr = errors.New("conflict")
	gitMock.operationStateFn = func(string) ([]domain.RepoState, error) { return []domain.RepoState{domain.RepoStateConflicted}, nil }
	release := writeRelease(t, m, domain.ReleaseStatusMasterMerged, finalizeService(m))

	got, err := m.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if !errors.Is(err, ErrReleaseMergeConflict) || len(gitMock.mergeAbortCalls) != 1 || len(gitMock.removeWorktreeCalls) != 1 {
		t.Fatalf("error = %v, abort = %#v, remove = %#v", err, gitMock.mergeAbortCalls, gitMock.removeWorktreeCalls)
	}
	if got.Status != domain.ReleaseStatusFailed || gitMock.createTagCalls != 0 || got.Services[0].IntegrationWorktreePath != "" {
		t.Fatalf("release = %#v, CreateTag calls = %d", got, gitMock.createTagCalls)
	}
}

func TestFinalizeRelease_LegacyManifestRejected(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	release := writeRelease(t, m, domain.ReleaseStatusMasterMerged, finalizeService(m))
	release.ManifestVersion = 1
	data, _ := json.Marshal(release)
	if err := os.WriteFile(m.releaseManifestPath(release.ID), data, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := m.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if !errors.Is(err, ErrReleaseLegacyManifest) || len(gitMock.fetchCalls) != 0 {
		t.Fatalf("error = %v, fetch calls = %#v", err, gitMock.fetchCalls)
	}
}

func TestFinalizeRelease_WrongStatusRejected(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	release := writeRelease(t, m, domain.ReleaseStatusPrepared, finalizeService(m))

	_, err := m.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if !errors.Is(err, ErrReleaseInvalidStatusTransition) || len(gitMock.fetchCalls) != 0 {
		t.Fatalf("error = %v, fetch calls = %#v", err, gitMock.fetchCalls)
	}
}
