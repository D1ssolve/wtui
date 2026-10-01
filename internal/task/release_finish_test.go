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
	"github.com/D1ssolve/wtui/internal/git"
)

func newFinishTestManager(t *testing.T) (*manager, *mockGitClient) {
	t.Helper()
	gitMock := &mockGitClient{branchExistsRes: true}
	m, _ := newReleasePlanTestManager(t, gitMock)
	m.flow.ProductionBranch = "master"
	gitMock.isAncestorFn = func(_, _, _ string) (bool, error) { return false, nil }
	svcRepo := filepath.Join(m.cfg.RootDir, "repo-api")
	gitMock.commonDirFn = func(string) (string, error) {
		return filepath.Join(svcRepo, ".git"), nil
	}
	gitMock.listWorktreesFn = func(repoPath string) ([]git.WorktreeEntry, error) {
		if repoPath != svcRepo {
			return nil, nil
		}
		entryPath := filepath.Join(m.releasesRootDir(), "rel-1.2.3-20260616T120000", ".work", "svc-api-finalize-integration")
		return []git.WorktreeEntry{{Path: entryPath, Branch: "(detached)", HEAD: "HEAD-sha"}}, nil
	}
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
		case "origin/master", "v1.2.3^{}", "tag-object-sha^{commit}":
			return "accepted-sha", nil
		case "refs/tags/v1.2.3":
			return "tag-object-sha", nil
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
	if got := gitMock.pushTagCallList[0].ObjectOID; got != "tag-object-sha" {
		t.Fatalf("PushTag OID = %q, want captured unpeeled tag object tag-object-sha", got)
	}
}

func TestFinalizeRelease_UsesServiceTagDescription(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	matchingMaster(gitMock)
	svc := finalizeService(m)
	svc.TagDescription = "Summary\n\nDetailed change"
	release := writeRelease(t, m, domain.ReleaseStatusMasterMerged, svc)

	if _, err := m.FinalizeRelease(t.Context(), FinishReleaseParams{ReleaseID: release.ID}); err != nil {
		t.Fatalf("FinalizeRelease() error = %v", err)
	}
	if got := gitMock.createTagCallList[0].Message; got != svc.TagDescription {
		t.Fatalf("tag message = %q, want %q", got, svc.TagDescription)
	}
}

func TestRunFinishService_UsesPersistedTagDescription(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	gitMock.resolveRefFn = func(_ string, ref string) (string, error) {
		if ref == "tag-object-sha^{commit}" {
			return "accepted-sha", nil
		}
		return "tag-object-sha", nil
	}
	release := domain.Release{ID: "rel-20260826T120000"}
	svc := domain.ReleaseService{
		Tag:              "v1.2.3",
		TagDescription:   "Fix retry after timeout",
		AcceptedMergeSHA: "accepted-sha",
	}

	if err := m.runFinishService(t.Context(), &release, &svc, nil); err != nil {
		t.Fatalf("runFinishService() error = %v", err)
	}
	if got := gitMock.createTagCallList[0].Message; got != svc.TagDescription {
		t.Fatalf("tag message = %q, want %q", got, svc.TagDescription)
	}
}

func TestFinalizeRelease_PushURLMismatchBlocksIntegrationPush(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	matchingMaster(gitMock)
	gitMock.pushURLRes = "git@gitlab.com:group/someone-else.git"
	release := writeRelease(t, m, domain.ReleaseStatusMasterMerged, finalizeService(m))

	got, err := m.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if err == nil || !strings.Contains(err.Error(), "push URL") {
		t.Fatalf("FinalizeRelease() error = %v, want push URL mismatch", err)
	}
	if got.Status != domain.ReleaseStatusFailed {
		t.Fatalf("release status = %q, want failed", got.Status)
	}
	if len(gitMock.pushBranchExplicitCalls) != 0 || gitMock.pushTagCalls != 0 {
		t.Fatalf("pushed with mismatched push destination: integration=%v tags=%d", gitMock.pushBranchExplicitCalls, gitMock.pushTagCalls)
	}
}

func TestFinalizeRelease_PushURLMismatchBlocksTagPush(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	matchingMaster(gitMock)
	*m.cfg.Release.PushIntegration = false
	gitMock.pushURLRes = "git@gitlab.com:group/someone-else.git"
	release := writeRelease(t, m, domain.ReleaseStatusMasterMerged, finalizeService(m))

	got, err := m.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if err == nil || !strings.Contains(err.Error(), "push URL") {
		t.Fatalf("FinalizeRelease() error = %v, want push URL mismatch", err)
	}
	if got.Status != domain.ReleaseStatusFailed || gitMock.pushTagCalls != 0 {
		t.Fatalf("release = %#v, pushTagCalls = %d: tag must not publish to a mismatched push destination", got, gitMock.pushTagCalls)
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
	svc.IntegrationWorktreePath = filepath.Join(m.releasesRootDir(), "rel-1.2.3-20260616T120000", ".work", "svc-api-finalize-integration")
	release := writeRelease(t, m, domain.ReleaseStatusMasterMerged, svc)

	got, err := m.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if !errors.Is(err, removeErr) {
		t.Fatalf("FinalizeRelease() error = %v, want removal error", err)
	}
	if got.Status != domain.ReleaseStatusFailed || len(gitMock.addWorktreeCalls) != 0 {
		t.Fatalf("release status = %q, add worktree calls = %#v", got.Status, gitMock.addWorktreeCalls)
	}
}

func TestFinalizeRelease_TagMovedBeforePush_NotPublished(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	gitMock.resolveRefFn = func(_ string, ref string) (string, error) {
		switch ref {
		case "origin/master", "v1.2.3^{}":
			return "accepted-sha", nil
		case "tag-object-sha^{commit}":
			return "sha-moved", nil
		default:
			return "tag-object-sha", nil
		}
	}
	release := writeRelease(t, m, domain.ReleaseStatusMasterMerged, finalizeService(m))

	got, err := m.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if err == nil {
		t.Fatal("FinalizeRelease() error = nil, want tag moved error")
	}
	if got.Status != domain.ReleaseStatusFailed || gitMock.pushTagCalls != 0 {
		t.Fatalf("release = %#v, pushTagCalls = %d: moved tag must not publish", got, gitMock.pushTagCalls)
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

func TestRunFinishService_ExistingTagNotRecreated_PushesCapturedOID(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	gitMock.tagExistsRes = true
	gitMock.resolveRefFn = func(_ string, ref string) (string, error) {
		if ref == "tag-object-sha^{commit}" {
			return "accepted-sha", nil
		}
		return "tag-object-sha", nil
	}
	release := domain.Release{ID: "rel-20260826T120000"}
	svc := domain.ReleaseService{
		Tag:              "v1.2.3",
		AcceptedMergeSHA: "accepted-sha",
	}

	if err := m.runFinishService(t.Context(), &release, &svc, nil); err != nil {
		t.Fatalf("runFinishService() error = %v", err)
	}
	if gitMock.createTagCalls != 0 {
		t.Fatalf("CreateTag calls = %d, want 0: existing matching tag must not move", gitMock.createTagCalls)
	}
	if gitMock.pushTagCalls != 1 || gitMock.pushTagCallList[0].ObjectOID != "tag-object-sha" {
		t.Fatalf("PushTag calls = %#v, want 1 push with captured OID", gitMock.pushTagCallList)
	}
	if svc.TagSHA != "accepted-sha" {
		t.Fatalf("TagSHA = %q, want peeled commit accepted-sha", svc.TagSHA)
	}
}

func TestRunFinishService_TagPeelMismatchFailsWithoutPush(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	gitMock.resolveRefFn = func(_ string, ref string) (string, error) {
		if ref == "tag-object-sha^{commit}" {
			return "sha-wrong", nil
		}
		return "tag-object-sha", nil
	}
	release := domain.Release{ID: "rel-20260826T120000"}
	svc := domain.ReleaseService{
		Tag:              "v1.2.3",
		AcceptedMergeSHA: "accepted-sha",
	}

	if err := m.runFinishService(t.Context(), &release, &svc, nil); err == nil {
		t.Fatal("runFinishService() error = nil, want peel mismatch error")
	}
	if gitMock.pushTagCalls != 0 || svc.PushedTag {
		t.Fatalf("pushTagCalls = %d, PushedTag = %v: mismatched tag must not publish", gitMock.pushTagCalls, svc.PushedTag)
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

func ownedIntegrationPath(m *manager, svc domain.ReleaseService) string {
	return filepath.Join(m.releasesRootDir(), "rel-1.2.3-20260616T120000", ".work", svc.Name+"-finalize-integration")
}

func TestFinalizeRelease_RetainedWorktreeOutsideOwnedRootBlocks(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	matchingMaster(gitMock)
	svc := finalizeService(m)
	svc.IntegrationWorktreePath = filepath.Join(m.cfg.RootDir, "elsewhere", "integration")
	release := writeRelease(t, m, domain.ReleaseStatusMasterMerged, svc)

	got, err := m.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if err == nil || !strings.Contains(err.Error(), "outside the release-owned directory") {
		t.Fatalf("FinalizeRelease() error = %v, want outside owned root rejection", err)
	}
	_ = got
	if len(gitMock.removeWorktreeCalls) != 0 || len(gitMock.addWorktreeCalls) != 0 {
		t.Fatalf("remove calls = %#v, add calls = %#v: unowned path must not be touched", gitMock.removeWorktreeCalls, gitMock.addWorktreeCalls)
	}
	stored, getErr := m.GetRelease(context.Background(), release.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if stored.Services[0].IntegrationWorktreePath != svc.IntegrationWorktreePath {
		t.Fatalf("manifest path = %q, want preserved %q", stored.Services[0].IntegrationWorktreePath, svc.IntegrationWorktreePath)
	}
}

func TestFinalizeRelease_RetainedWorktreeUnexpectedNameBlocks(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	matchingMaster(gitMock)
	svc := finalizeService(m)
	svc.IntegrationWorktreePath = filepath.Join(m.releasesRootDir(), "rel-1.2.3-20260616T120000", ".work", "other-service-finalize-integration")
	release := writeRelease(t, m, domain.ReleaseStatusMasterMerged, svc)

	_, err := m.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if err == nil || !strings.Contains(err.Error(), "does not name an owned worktree") {
		t.Fatalf("FinalizeRelease() error = %v, want owned-name rejection", err)
	}
	if len(gitMock.removeWorktreeCalls) != 0 {
		t.Fatalf("remove calls = %#v: misnamed path must not be touched", gitMock.removeWorktreeCalls)
	}
}

func TestFinalizeRelease_RetainedWorktreeNotRegisteredBlocks(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	matchingMaster(gitMock)
	gitMock.listWorktreesFn = func(string) ([]git.WorktreeEntry, error) { return nil, nil }
	svc := finalizeService(m)
	svc.IntegrationWorktreePath = ownedIntegrationPath(m, svc)
	release := writeRelease(t, m, domain.ReleaseStatusMasterMerged, svc)

	_, err := m.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if err == nil || !strings.Contains(err.Error(), "exactly one registered worktree") {
		t.Fatalf("FinalizeRelease() error = %v, want registration rejection", err)
	}
	if len(gitMock.removeWorktreeCalls) != 0 {
		t.Fatalf("remove calls = %#v: unregistered path must not be touched", gitMock.removeWorktreeCalls)
	}
}

func TestFinalizeRelease_RetainedWorktreeLockedBlocks(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	matchingMaster(gitMock)
	svc := finalizeService(m)
	lockedPath := ownedIntegrationPath(m, svc)
	gitMock.listWorktreesFn = func(repoPath string) ([]git.WorktreeEntry, error) {
		if repoPath != svc.RepoPath {
			return nil, nil
		}
		return []git.WorktreeEntry{{Path: lockedPath, Branch: "(detached)", HEAD: "HEAD-sha", Locked: true}}, nil
	}
	svc.IntegrationWorktreePath = lockedPath
	release := writeRelease(t, m, domain.ReleaseStatusMasterMerged, svc)

	_, err := m.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if err == nil || !strings.Contains(err.Error(), "is locked") {
		t.Fatalf("FinalizeRelease() error = %v, want locked rejection", err)
	}
	if len(gitMock.removeWorktreeCalls) != 0 {
		t.Fatalf("remove calls = %#v: locked worktree must not be removed", gitMock.removeWorktreeCalls)
	}
}

func TestFinalizeRelease_RetainedWorktreeWrongRepositoryBlocks(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	matchingMaster(gitMock)
	gitMock.commonDirFn = func(string) (string, error) {
		return filepath.Join(m.cfg.RootDir, "someone-else", ".git"), nil
	}
	svc := finalizeService(m)
	svc.IntegrationWorktreePath = ownedIntegrationPath(m, svc)
	release := writeRelease(t, m, domain.ReleaseStatusMasterMerged, svc)

	_, err := m.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if err == nil || !strings.Contains(err.Error(), "different repository") {
		t.Fatalf("FinalizeRelease() error = %v, want repository rejection", err)
	}
	if len(gitMock.removeWorktreeCalls) != 0 {
		t.Fatalf("remove calls = %#v: foreign worktree must not be removed", gitMock.removeWorktreeCalls)
	}
}

func TestFinalizeRelease_RetainedWorktreeHeadMismatchBlocks(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	matchingMaster(gitMock)
	svc := finalizeService(m)
	svc.PostIntegrationSHA = "manifest-head"
	mismatchedPath := ownedIntegrationPath(m, svc)
	gitMock.listWorktreesFn = func(repoPath string) ([]git.WorktreeEntry, error) {
		if repoPath != svc.RepoPath {
			return nil, nil
		}
		return []git.WorktreeEntry{{Path: mismatchedPath, Branch: "(detached)", HEAD: "surprise-head"}}, nil
	}
	svc.IntegrationWorktreePath = mismatchedPath
	release := writeRelease(t, m, domain.ReleaseStatusMasterMerged, svc)

	_, err := m.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if err == nil || !strings.Contains(err.Error(), "want manifest-head") {
		t.Fatalf("FinalizeRelease() error = %v, want HEAD mismatch rejection", err)
	}
	if len(gitMock.removeWorktreeCalls) != 0 {
		t.Fatalf("remove calls = %#v: moved worktree must not be removed", gitMock.removeWorktreeCalls)
	}
}

func TestFinalizeRelease_RetainedWorktreeDirtyBlocks(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	matchingMaster(gitMock)
	gitMock.repoStatusFn = func(string) (git.RawStatus, error) {
		return git.RawStatus{UntrackedPaths: []string{"wip.txt"}}, nil
	}
	svc := finalizeService(m)
	svc.IntegrationWorktreePath = ownedIntegrationPath(m, svc)
	release := writeRelease(t, m, domain.ReleaseStatusMasterMerged, svc)

	_, err := m.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if err == nil || !strings.Contains(err.Error(), "is dirty") {
		t.Fatalf("FinalizeRelease() error = %v, want dirty rejection", err)
	}
	if len(gitMock.removeWorktreeCalls) != 0 {
		t.Fatalf("remove calls = %#v: dirty worktree must not be removed", gitMock.removeWorktreeCalls)
	}
}

func TestFinalizeRelease_RetainedWorktreeRemovedNonForce(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	matchingMaster(gitMock)
	svc := finalizeService(m)
	svc.IntegrationWorktreePath = ownedIntegrationPath(m, svc)
	release := writeRelease(t, m, domain.ReleaseStatusMasterMerged, svc)

	got, err := m.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if err != nil {
		t.Fatalf("FinalizeRelease() error = %v", err)
	}
	if len(gitMock.removeWorktreeCalls) != 2 {
		t.Fatalf("remove calls = %#v, want retained + fresh worktree removals", gitMock.removeWorktreeCalls)
	}
	for _, call := range gitMock.removeWorktreeCalls {
		if call.Force {
			t.Fatalf("remove call = %#v: force removal is forbidden", call)
		}
	}
	if got.Services[0].IntegrationWorktreePath != "" {
		t.Fatalf("IntegrationWorktreePath = %q, want cleaned after validated removal", got.Services[0].IntegrationWorktreePath)
	}
	if len(gitMock.addWorktreeCalls) != 1 {
		t.Fatalf("AddWorktree calls = %#v, want fresh integration worktree after removal", gitMock.addWorktreeCalls)
	}
}

func TestFinalizeRelease_RemovalLeavesPathBehindBlocks(t *testing.T) {
	m, gitMock := newFinishTestManager(t)
	matchingMaster(gitMock)
	svc := finalizeService(m)
	leftoverPath := ownedIntegrationPath(m, svc)
	svc.IntegrationWorktreePath = leftoverPath
	if err := os.MkdirAll(leftoverPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(leftoverPath, "replacement.txt"), []byte("user data"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Removal succeeds at the git layer but the path is repopulated behind us.
	gitMock.removeWorktreeFn = func(_, _ string, _ bool) error { return nil }
	release := writeRelease(t, m, domain.ReleaseStatusMasterMerged, svc)

	_, err := m.FinalizeRelease(context.Background(), FinishReleaseParams{ReleaseID: release.ID})
	if err == nil || !strings.Contains(err.Error(), "still present after removal") {
		t.Fatalf("FinalizeRelease() error = %v, want leftover rejection", err)
	}
	if _, statErr := os.Stat(filepath.Join(leftoverPath, "replacement.txt")); statErr != nil {
		t.Fatalf("replacement content lost: %v", statErr)
	}
	stored, getErr := m.GetRelease(context.Background(), release.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if stored.Services[0].IntegrationWorktreePath != leftoverPath {
		t.Fatalf("manifest path = %q, want preserved leftover path", stored.Services[0].IntegrationWorktreePath)
	}
}
