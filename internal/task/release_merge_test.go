package task

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

type releaseMergeForgeClient struct {
	mu            sync.Mutex
	readiness     map[int]forge.MRReadiness
	preMerge      map[int]forge.MRReadiness
	postMerge     map[int]forge.MRReadiness
	readCount     map[int]int
	mergedNumbers map[int]bool
	readErrs      map[int]error
	mergeErrs     map[int]error
	mergeSHAs     map[int]string
	merges        []forge.MergeMRParams
}

func (f *releaseMergeForgeClient) Provider() forge.ForgeProvider    { return forge.ForgeProviderGitLab }
func (f *releaseMergeForgeClient) IsAvailable(context.Context) bool { return true }
func (*releaseMergeForgeClient) CreateMR(context.Context, forge.CreateMRParams) (forge.MRInfo, error) {
	return forge.MRInfo{}, nil
}
func (*releaseMergeForgeClient) MRStatus(context.Context, string, string) ([]forge.MRInfo, error) {
	return nil, nil
}
func (f *releaseMergeForgeClient) MRReadiness(_ context.Context, branch, _, _ string) (forge.MRReadiness, error) {
	return forge.MRReadiness{}, errors.New("branch readiness must not be used: " + branch)
}
func (f *releaseMergeForgeClient) MRReadinessByNumber(_ context.Context, number int, _, _ string) (forge.MRReadiness, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.mergedNumbers[number] {
		if r, ok := f.postMerge[number]; ok {
			return r, nil
		}
	}
	if f.readCount == nil {
		f.readCount = make(map[int]int)
	}
	f.readCount[number]++
	if f.readCount[number] > 1 {
		if r, ok := f.preMerge[number]; ok {
			return r, nil
		}
	}
	return f.readiness[number], f.readErrs[number]
}
func (f *releaseMergeForgeClient) MergeMR(_ context.Context, params forge.MergeMRParams) (forge.MRMergeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.merges = append(f.merges, params)
	if err := f.mergeErrs[params.Number]; err != nil {
		return forge.MRMergeResult{}, err
	}
	f.mergedNumbers[params.Number] = true
	return forge.MRMergeResult{Merged: true, MergeCommitSHA: f.mergeSHAs[params.Number]}, nil
}
func (*releaseMergeForgeClient) PipelineStatus(context.Context, string, string) ([]forge.PipelineStatus, error) {
	return nil, nil
}
func (*releaseMergeForgeClient) TriggerPipeline(context.Context, forge.TriggerPipelineParams) error {
	return nil
}
func (*releaseMergeForgeClient) ListIssues(context.Context, forge.ListIssuesParams) ([]forge.IssueInfo, error) {
	return nil, nil
}

func TestReleaseMerge_AllReadyMovesReleaseToMasterMerged(t *testing.T) {
	m, gitMock, client := newReleaseMergeTestManager(t)
	rule := m.flow.BranchTypes[gitflow.BranchTypeRelease]
	rule.MergeStrategy = gitflow.MergeStrategySquash
	m.flow.BranchTypes[gitflow.BranchTypeRelease] = rule
	client.readiness = map[int]forge.MRReadiness{
		1: openProductionMR(1, "api", "api-source"),
		2: openProductionMR(2, "worker", "worker-source"),
	}
	client.postMerge = map[int]forge.MRReadiness{
		1: mergedProductionMR(1, "api", "api-source", "api-merge"),
		2: mergedProductionMR(2, "worker", "worker-source", "worker-merge"),
	}
	client.mergeSHAs = map[int]string{1: "api-merge", 2: "worker-merge"}
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge,
		releaseMergeService("api", 1), releaseMergeService("worker", 2))

	got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatalf("MergeReleaseMRs() error = %v", err)
	}
	if got.Status != domain.ReleaseStatusMasterMerged || !slices.Equal(result.Merged, []string{"api", "worker"}) {
		t.Fatalf("release status = %q, result = %#v", got.Status, result)
	}
	if got.Services[0].AcceptedMergeSHA != "api-merge" || got.Services[1].AcceptedMergeSHA != "worker-merge" {
		t.Fatalf("services = %#v", got.Services)
	}
	if len(gitMock.fetchCalls) != 4 {
		t.Fatalf("fetch calls = %v, want pre-merge and acceptance verification fetches per service", gitMock.fetchCalls)
	}
	if client.merges[0].ExpectedHeadSHA != "api-source" || client.merges[1].ExpectedHeadSHA != "worker-source" {
		t.Fatalf("merges = %#v, want manifest source SHA pins", client.merges)
	}
	if client.merges[0].ExpectedTargetSHA != "origin/master-sha" || client.merges[1].ExpectedTargetSHA != "origin/master-sha" {
		t.Fatalf("merges = %#v, want freshly resolved production target SHA pins", client.merges)
	}
	if client.merges[0].Method != "squash" || client.merges[1].Method != "squash" {
		t.Fatalf("merges = %#v, want squash method", client.merges)
	}
	if client.merges[0].ExpectedTargetBranch != "master" || client.merges[1].ExpectedTargetBranch != "master" {
		t.Fatalf("merges = %#v, want production target branch binding", client.merges)
	}
}

func TestReleaseMerge_FetchesFreshTargetTipBeforeMerge(t *testing.T) {
	m, gitMock, client := newReleaseMergeTestManager(t)
	client.readiness = map[int]forge.MRReadiness{1: openProductionMR(1, "api", "api-source")}
	client.postMerge = map[int]forge.MRReadiness{
		1: mergedProductionMR(1, "api", "api-source", "api-merge"),
	}
	client.mergeSHAs = map[int]string{1: "api-merge"}
	fetched := false
	gitMock.fetchFn = func(string) error {
		fetched = true
		return nil
	}
	gitMock.resolveRefFn = func(_ string, ref string) (string, error) {
		if ref == "origin/master" && !fetched {
			return "stale-production-tip", nil
		}
		return "fresh-production-tip", nil
	}
	gitMock.isAncestorFn = func(string, string, string) (bool, error) { return true, nil }
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, releaseMergeService("api", 1))

	got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatalf("MergeReleaseMRs() error = %v", err)
	}
	if got.Status != domain.ReleaseStatusMasterMerged || !slices.Equal(result.Merged, []string{"api"}) {
		t.Fatalf("release = %#v, result = %#v", got, result)
	}
	if len(client.merges) != 1 || client.merges[0].ExpectedTargetSHA != "fresh-production-tip" {
		t.Fatalf("merges = %#v, want merge bound to freshly resolved production tip", client.merges)
	}
}

func TestReleaseMerge_FreshTargetResolutionFailureFailsService(t *testing.T) {
	m, gitMock, client := newReleaseMergeTestManager(t)
	client.readiness = map[int]forge.MRReadiness{1: openProductionMR(1, "api", "api-source")}
	gitMock.resolveRefErr = errors.New("no such ref")
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, releaseMergeService("api", 1))

	got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatalf("MergeReleaseMRs() error = %v", err)
	}
	if len(client.merges) != 0 || !slices.Equal(result.Failed, []string{"api"}) || got.Services[0].AcceptedMergeSHA != "" {
		t.Fatalf("merges = %#v, result = %#v, service = %#v", client.merges, result, got.Services[0])
	}
	if got.Services[0].Error == nil || !got.Services[0].Error.Recoverable {
		t.Fatalf("service error = %#v, want retryable failure", got.Services[0].Error)
	}
}

func TestReleaseMerge_BlockedServiceLeavesPartialReleaseRetryable(t *testing.T) {
	m, _, client := newReleaseMergeTestManager(t)
	client.readiness = map[int]forge.MRReadiness{
		1: openProductionMR(1, "api", "api-source"),
		2: {Number: 2, State: "open", SourceBranch: "release/worker", TargetBranch: "master", Blockers: []string{"checks failing"}},
	}
	client.postMerge = map[int]forge.MRReadiness{
		1: mergedProductionMR(1, "api", "api-source", "api-merge"),
	}
	client.mergeSHAs = map[int]string{1: "api-merge"}
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge,
		releaseMergeService("api", 1), releaseMergeService("worker", 2))

	got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatalf("MergeReleaseMRs() error = %v", err)
	}
	if got.Status != domain.ReleaseStatusAwaitingMasterMerge || !slices.Equal(result.Merged, []string{"api"}) || !slices.Equal(result.Skipped, []string{"worker"}) {
		t.Fatalf("release status = %q, result = %#v", got.Status, result)
	}
	if got.Services[0].ProductionMR.State != "merged" || got.Services[0].AcceptedMergeSHA != "api-merge" {
		t.Fatalf("merged service = %#v", got.Services[0])
	}
	if got.Services[1].ProductionMR.State != "open" || got.Services[1].AcceptedMergeSHA != "" {
		t.Fatalf("blocked service changed = %#v", got.Services[1])
	}
	persisted, loadErr := m.GetRelease(t.Context(), release.ID)
	if loadErr != nil || persisted.Services[0].AcceptedMergeSHA != "api-merge" {
		t.Fatalf("persisted = %#v, error = %v", persisted, loadErr)
	}
}

func TestReleaseMerge_HeadDriftFailsServiceAndContinues(t *testing.T) {
	m, _, client := newReleaseMergeTestManager(t)
	headDrift := errors.New("head SHA changed")
	client.readiness = map[int]forge.MRReadiness{
		1: openProductionMR(1, "api", "api-source"),
		2: openProductionMR(2, "worker", "worker-source"),
	}
	client.mergeErrs = map[int]error{1: headDrift}
	client.postMerge = map[int]forge.MRReadiness{
		2: mergedProductionMR(2, "worker", "worker-source", "worker-merge"),
	}
	client.mergeSHAs = map[int]string{2: "worker-merge"}
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge,
		releaseMergeService("api", 1), releaseMergeService("worker", 2))

	got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatalf("MergeReleaseMRs() error = %v", err)
	}
	if got.Status != domain.ReleaseStatusAwaitingMasterMerge || !slices.Equal(result.Failed, []string{"api"}) || !slices.Equal(result.Merged, []string{"worker"}) {
		t.Fatalf("release status = %q, result = %#v", got.Status, result)
	}
	if len(client.merges) != 2 || got.Services[0].ProductionMR.State != "open" || got.Services[1].ProductionMR.State != "merged" {
		t.Fatalf("merges = %#v, services = %#v", client.merges, got.Services)
	}
}

func TestReleaseMerge_AlreadyMergedServiceIsSkipped(t *testing.T) {
	m, _, client := newReleaseMergeTestManager(t)
	client.readiness = map[int]forge.MRReadiness{1: mergedProductionMR(1, "api", "api-source", "accepted")}
	svc := releaseMergeService("api", 1)
	svc.ProductionMR.State = "merged"
	svc.AcceptedMergeSHA = "accepted"
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, svc)

	got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatalf("MergeReleaseMRs() error = %v", err)
	}
	if got.Status != domain.ReleaseStatusMasterMerged || !slices.Equal(result.Skipped, []string{"api"}) || len(client.merges) != 0 {
		t.Fatalf("release status = %q, result = %#v, merges = %#v", got.Status, result, client.merges)
	}
}

func TestReleaseMerge_AlreadyMergedRecoversAcceptedSHA(t *testing.T) {
	for _, tc := range []struct {
		name      string
		readiness forge.MRReadiness
		targetSHA string
		wantSHA   string
	}{
		{name: "fast-forward", readiness: mergedProductionMR(1, "api", "api-source", ""), targetSHA: "api-source", wantSHA: "api-source"},
		{name: "squash", readiness: mergedProductionMR(1, "api", "api-source", "squash-sha"), targetSHA: "later-master", wantSHA: "squash-sha"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, gitMock, client := newReleaseMergeTestManager(t)
			client.readiness = map[int]forge.MRReadiness{1: tc.readiness}
			gitMock.resolveRefFn = func(_ string, ref string) (string, error) {
				if ref == "origin/master" {
					return tc.targetSHA, nil
				}
				return ref + "-sha", nil
			}
			svc := releaseMergeService("api", 1)
			svc.Status = domain.ReleaseStatusFailed
			svc.Error = &domain.ReleaseError{Code: "ERR_RELEASE_MERGE_FAILED", Message: "old merge error"}
			release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, svc)

			got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != domain.ReleaseStatusMasterMerged || got.Services[0].AcceptedMergeSHA != tc.wantSHA || got.Services[0].Error != nil || !slices.Equal(result.Skipped, []string{"api"}) {
				t.Fatalf("release = %#v, result = %#v", got, result)
			}
		})
	}
}

func TestReleaseMerge_AlreadyMergedFFTargetMovedFailsClosed(t *testing.T) {
	m, gitMock, client := newReleaseMergeTestManager(t)
	client.readiness = map[int]forge.MRReadiness{1: mergedProductionMR(1, "api", "api-source", "")}
	gitMock.resolveRefFn = func(_ string, ref string) (string, error) {
		if ref == "origin/master" {
			return "later-master", nil
		}
		return ref + "-sha", nil
	}
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, releaseMergeService("api", 1))

	got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.Failed, []string{"api"}) || got.Services[0].AcceptedMergeSHA != "" || got.Status != domain.ReleaseStatusAwaitingMasterMerge {
		t.Fatalf("release = %#v, result = %#v", got, result)
	}
}

func TestReleaseMerge_RecoveryFailureDoesNotFailFollowingMergedService(t *testing.T) {
	m, _, client := newReleaseMergeTestManager(t)
	client.readiness = map[int]forge.MRReadiness{
		1: mergedProductionMR(1, "api", "", ""),
		2: mergedProductionMR(2, "worker", "worker-source", "worker-merge"),
	}
	api := releaseMergeService("api", 1)
	worker := releaseMergeService("worker", 2)
	worker.ProductionMR.State = "merged"
	worker.AcceptedMergeSHA = "worker-merge"
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, api, worker)

	got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.Failed, []string{"api"}) || !slices.Equal(result.Skipped, []string{"worker"}) || got.Services[1].Status != domain.ReleaseStatusMasterMerged {
		t.Fatalf("release = %#v, result = %#v", got, result)
	}
}

func TestReleaseMerge_EmptyMergeSHAFailsWithoutGuessingProductionTip(t *testing.T) {
	m, gitMock, client := newReleaseMergeTestManager(t)
	client.readiness = map[int]forge.MRReadiness{7: openProductionMR(7, "api", "api-source")}
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, releaseMergeService("api", 7))

	got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatalf("MergeReleaseMRs() error = %v", err)
	}
	if !slices.Equal(result.Failed, []string{"api"}) || got.Services[0].AcceptedMergeSHA != "" || got.Services[0].Status != domain.ReleaseStatusFailed || got.Services[0].Error == nil || !got.Services[0].Error.Recoverable || len(gitMock.fetchCalls) != 2 {
		t.Fatalf("result = %#v, service = %#v, fetches = %v", result, got.Services[0], gitMock.fetchCalls)
	}
}

func TestReleaseMerge_UnpinnedHeadDriftSkipsMerge(t *testing.T) {
	m, _, client := newReleaseMergeTestManager(t)
	readiness := openProductionMR(9, "api", "api-source")
	readiness.SupportsSHAPin = false
	client.readiness = map[int]forge.MRReadiness{9: readiness}
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, releaseMergeService("api", 9))

	got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatalf("MergeReleaseMRs() error = %v", err)
	}
	if len(client.merges) != 0 || !slices.Equal(result.Failed, []string{"api"}) || got.Services[0].AcceptedMergeSHA != "" {
		t.Fatalf("merges = %#v, result = %#v, service = %#v", client.merges, result, got.Services[0])
	}
}

func TestReleaseMerge_UnsupportedTargetBindingSkipsMerge(t *testing.T) {
	m, _, client := newReleaseMergeTestManager(t)
	readiness := openProductionMR(9, "api", "api-source")
	readiness.SupportsTargetBinding = false
	client.readiness = map[int]forge.MRReadiness{9: readiness}
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, releaseMergeService("api", 9))

	got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatalf("MergeReleaseMRs() error = %v", err)
	}
	if len(client.merges) != 0 || !slices.Equal(result.Failed, []string{"api"}) || got.Services[0].AcceptedMergeSHA != "" {
		t.Fatalf("merges = %#v, result = %#v, service = %#v", client.merges, result, got.Services[0])
	}
	if got.Services[0].Error == nil || !strings.Contains(got.Services[0].Error.Cause, "target-bound") {
		t.Fatalf("service error = %#v, want target-binding rejection", got.Services[0].Error)
	}
}

func TestReleaseMerge_MovedMergedHeadFailsPostflightClosed(t *testing.T) {
	m, _, client := newReleaseMergeTestManager(t)
	client.readiness = map[int]forge.MRReadiness{1: openProductionMR(1, "api", "api-source")}
	client.mergeSHAs = map[int]string{1: "api-merge"}
	moved := mergedProductionMR(1, "api", "changed-sha", "api-merge")
	client.postMerge = map[int]forge.MRReadiness{1: moved}
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, releaseMergeService("api", 1))

	got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatalf("MergeReleaseMRs() error = %v", err)
	}
	if len(client.merges) != 1 || !slices.Equal(result.Failed, []string{"api"}) {
		t.Fatalf("merges = %#v, result = %#v", client.merges, result)
	}
	if svc := got.Services[0]; svc.AcceptedMergeSHA != "" || svc.Status != domain.ReleaseStatusFailed || svc.Error == nil || !svc.Error.Recoverable {
		t.Fatalf("service = %#v, want retryable failure without accepted SHA", svc)
	}
	if got.Status != domain.ReleaseStatusAwaitingMasterMerge {
		t.Fatalf("release status = %q", got.Status)
	}
}

func TestReleaseMerge_MissingMergeSHARecoveryRejectsMovedHead(t *testing.T) {
	m, gitMock, client := newReleaseMergeTestManager(t)
	client.readiness = map[int]forge.MRReadiness{1: openProductionMR(1, "api", "api-source")}
	client.mergeSHAs = map[int]string{1: ""}
	moved := mergedProductionMR(1, "api", "changed-sha", "")
	client.postMerge = map[int]forge.MRReadiness{1: moved}
	gitMock.resolveRefFn = func(_ string, ref string) (string, error) {
		if ref == "origin/master" {
			return "changed-sha", nil
		}
		return ref + "-sha", nil
	}
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, releaseMergeService("api", 1))

	got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatalf("MergeReleaseMRs() error = %v", err)
	}
	if !slices.Equal(result.Failed, []string{"api"}) || got.Services[0].AcceptedMergeSHA != "" || got.Services[0].Status != domain.ReleaseStatusFailed {
		t.Fatalf("release = %#v, result = %#v", got, result)
	}
}

func TestReleaseMerge_AlreadyMergedRejectsMovedHead(t *testing.T) {
	m, _, client := newReleaseMergeTestManager(t)
	client.readiness = map[int]forge.MRReadiness{1: mergedProductionMR(1, "api", "changed-sha", "accepted")}
	svc := releaseMergeService("api", 1)
	svc.ProductionMR.State = "merged"
	svc.AcceptedMergeSHA = "accepted"
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, svc)

	got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatalf("MergeReleaseMRs() error = %v", err)
	}
	if !slices.Equal(result.Failed, []string{"api"}) || got.Services[0].AcceptedMergeSHA != "" || got.Services[0].Status != domain.ReleaseStatusFailed || len(client.merges) != 0 {
		t.Fatalf("release = %#v, result = %#v, merges = %#v", got, result, client.merges)
	}
}

func TestReleaseMerge_MergedHeadSHAMissingFailsClosed(t *testing.T) {
	m, _, client := newReleaseMergeTestManager(t)
	client.readiness = map[int]forge.MRReadiness{1: mergedProductionMR(1, "api", "", "accepted")}
	svc := releaseMergeService("api", 1)
	svc.ProductionMR.State = "merged"
	svc.AcceptedMergeSHA = "accepted"
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, svc)

	got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatalf("MergeReleaseMRs() error = %v", err)
	}
	if !slices.Equal(result.Failed, []string{"api"}) || got.Services[0].AcceptedMergeSHA != "" || got.Services[0].Status != domain.ReleaseStatusFailed {
		t.Fatalf("release = %#v, result = %#v", got, result)
	}
}

func TestReleaseMerge_WrongStatusRejected(t *testing.T) {
	m, _, client := newReleaseMergeTestManager(t)
	release := writePromoteRelease(t, m, domain.ReleaseStatusPrepared, releaseMergeService("api", 1))

	if _, err := m.InspectReleaseMerge(t.Context(), release.ID); !errors.Is(err, ErrReleaseInvalidStatusTransition) {
		t.Fatalf("InspectReleaseMerge() error = %v", err)
	}
	if _, _, err := m.MergeReleaseMRs(t.Context(), release.ID, nil); !errors.Is(err, ErrReleaseInvalidStatusTransition) {
		t.Fatalf("MergeReleaseMRs() error = %v", err)
	}
	if len(client.merges) != 0 {
		t.Fatalf("merges = %#v", client.merges)
	}
}

func TestReleaseMerge_RetargetedMRMetadataBlocks(t *testing.T) {
	m, _, client := newReleaseMergeTestManager(t)
	retargeted := mergedProductionMR(1, "api", "api-source", "api-merge")
	retargeted.TargetBranch = "develop"
	client.readiness = map[int]forge.MRReadiness{1: retargeted}
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, releaseMergeService("api", 1))

	got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatalf("MergeReleaseMRs() error = %v", err)
	}
	if got.Status != domain.ReleaseStatusAwaitingMasterMerge || !slices.Equal(result.Skipped, []string{"api"}) || len(client.merges) != 0 {
		t.Fatalf("release = %#v, result = %#v, merges = %#v", got, result, client.merges)
	}
}

func TestReleaseMerge_PreflightHeadDriftBlocks(t *testing.T) {
	m, _, client := newReleaseMergeTestManager(t)
	drifted := openProductionMR(1, "api", "changed-sha")
	client.readiness = map[int]forge.MRReadiness{1: drifted}
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, releaseMergeService("api", 1))

	inspection, err := m.InspectReleaseMerge(t.Context(), release.ID)
	if err != nil {
		t.Fatalf("InspectReleaseMerge() error = %v", err)
	}
	if inspection.Services[0].Status != "blocked" {
		t.Fatalf("status = %q, want blocked: %#v", inspection.Services[0].Status, inspection.Services[0])
	}

	got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatalf("MergeReleaseMRs() error = %v", err)
	}
	if got.Status != domain.ReleaseStatusAwaitingMasterMerge || !slices.Equal(result.Skipped, []string{"api"}) || len(client.merges) != 0 {
		t.Fatalf("release = %#v, result = %#v, merges = %#v", got, result, client.merges)
	}
}

func TestReleaseMerge_HeadDriftAfterInspectionSkipsMerge(t *testing.T) {
	m, _, client := newReleaseMergeTestManager(t)
	client.readiness = map[int]forge.MRReadiness{1: openProductionMR(1, "api", "api-source")}
	client.preMerge = map[int]forge.MRReadiness{1: openProductionMR(1, "api", "changed-sha")}
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, releaseMergeService("api", 1))

	got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatalf("MergeReleaseMRs() error = %v", err)
	}
	if !slices.Equal(result.Failed, []string{"api"}) || len(client.merges) != 0 {
		t.Fatalf("release = %#v, result = %#v, merges = %#v", got, result, client.merges)
	}
}

func TestReleaseMerge_MissingRecordedSourceSHABlocks(t *testing.T) {
	m, _, client := newReleaseMergeTestManager(t)
	client.readiness = map[int]forge.MRReadiness{1: openProductionMR(1, "api", "api-source")}
	svc := releaseMergeService("api", 1)
	svc.ProductionMR.SourceSHA = ""
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, svc)

	inspection, err := m.InspectReleaseMerge(t.Context(), release.ID)
	if err != nil {
		t.Fatalf("InspectReleaseMerge() error = %v", err)
	}
	if inspection.Services[0].Status != "blocked" {
		t.Fatalf("status = %q, want blocked: %#v", inspection.Services[0].Status, inspection.Services[0])
	}
	if len(client.merges) != 0 {
		t.Fatalf("merges = %#v", client.merges)
	}
}

func TestReleaseMerge_ReturnedSHAConflictFailsServiceRetryable(t *testing.T) {
	m, _, client := newReleaseMergeTestManager(t)
	client.readiness = map[int]forge.MRReadiness{1: openProductionMR(1, "api", "api-source")}
	client.mergeSHAs = map[int]string{1: "returned-sha"}
	client.postMerge = map[int]forge.MRReadiness{
		1: mergedProductionMR(1, "api", "api-source", "authoritative-sha"),
	}
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, releaseMergeService("api", 1))

	got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatalf("MergeReleaseMRs() error = %v", err)
	}
	if len(client.merges) != 1 || !slices.Equal(result.Failed, []string{"api"}) {
		t.Fatalf("merges = %#v, result = %#v", client.merges, result)
	}
	svc := got.Services[0]
	if svc.AcceptedMergeSHA != "" || svc.Status != domain.ReleaseStatusFailed || svc.Error == nil || !svc.Error.Recoverable {
		t.Fatalf("service = %#v, want retryable failure without accepted SHA", svc)
	}
	if got.Status != domain.ReleaseStatusAwaitingMasterMerge {
		t.Fatalf("release status = %q", got.Status)
	}
}

func TestReleaseMerge_MergedSHAOutsideProductionFailsThenRetryAccepts(t *testing.T) {
	m, gitMock, client := newReleaseMergeTestManager(t)
	client.readiness = map[int]forge.MRReadiness{1: mergedProductionMR(1, "api", "api-source", "api-merge")}
	svc := releaseMergeService("api", 1)
	svc.ProductionMR.State = "merged"
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, svc)

	contained := false
	gitMock.isAncestorFn = func(_, _, _ string) (bool, error) { return contained, nil }

	got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatalf("MergeReleaseMRs() error = %v", err)
	}
	if !slices.Equal(result.Failed, []string{"api"}) || got.Services[0].AcceptedMergeSHA != "" || got.Services[0].Error == nil || !got.Services[0].Error.Recoverable {
		t.Fatalf("release = %#v, result = %#v", got, result)
	}

	contained = true
	got, result, err = m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatalf("retry MergeReleaseMRs() error = %v", err)
	}
	if got.Status != domain.ReleaseStatusMasterMerged || got.Services[0].AcceptedMergeSHA != "api-merge" || !slices.Equal(result.Skipped, []string{"api"}) {
		t.Fatalf("release = %#v, result = %#v", got, result)
	}
}

func TestReleaseMerge_AdvancedProductionTipAccepted(t *testing.T) {
	m, gitMock, client := newReleaseMergeTestManager(t)
	client.readiness = map[int]forge.MRReadiness{1: mergedProductionMR(1, "api", "api-source", "api-merge")}
	svc := releaseMergeService("api", 1)
	svc.ProductionMR.State = "merged"
	svc.AcceptedMergeSHA = "api-merge"
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, svc)
	gitMock.resolveRefFn = func(_ string, ref string) (string, error) {
		if ref == "origin/master" {
			return "advanced-production-tip", nil
		}
		return ref + "-sha", nil
	}

	got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatalf("MergeReleaseMRs() error = %v", err)
	}
	if got.Status != domain.ReleaseStatusMasterMerged || got.Services[0].AcceptedMergeSHA != "api-merge" || !slices.Equal(result.Skipped, []string{"api"}) {
		t.Fatalf("release = %#v, result = %#v", got, result)
	}
}

func TestReleaseMerge_OriginRetargetWithMatchingMRNumberAndSHAsFails(t *testing.T) {
	m, gitMock, client := newReleaseMergeTestManager(t)
	// The forge happens to serve an MR with the same number, source, head,
	// and target from a different repository; only the origin retargeted.
	client.readiness = map[int]forge.MRReadiness{1: openProductionMR(1, "api", "api-source")}
	gitMock.remoteURLRes = "git@gitlab.com:group/other-repo.git"
	svc := releaseMergeService("api", 1)
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, svc)

	got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatalf("MergeReleaseMRs() error = %v", err)
	}
	if !slices.Equal(result.Failed, []string{"api"}) || len(client.merges) != 0 {
		t.Fatalf("release = %#v, result = %#v, merges = %v; want api failed without merge", got, result, client.merges)
	}
	inspection, err := m.InspectReleaseMerge(t.Context(), release.ID)
	if err != nil {
		t.Fatalf("InspectReleaseMerge() error = %v", err)
	}
	if inspection.Services[0].Status != "failed" || !strings.Contains(strings.Join(inspection.Services[0].Blockers, "; "), "repository") {
		t.Fatalf("inspection = %+v, want failed with repository identity blocker", inspection.Services[0])
	}
}

func TestReleaseMerge_LegacyMissingRepositoryIdentityFailsClosed(t *testing.T) {
	m, _, client := newReleaseMergeTestManager(t)
	client.readiness = map[int]forge.MRReadiness{1: openProductionMR(1, "api", "api-source")}
	svc := releaseMergeService("api", 1)
	svc.ProductionMR.Repo = ""
	svc.ProductionMR.ProviderHost = ""
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, svc)

	inspection, err := m.InspectReleaseMerge(t.Context(), release.ID)
	if err != nil {
		t.Fatalf("InspectReleaseMerge() error = %v", err)
	}
	if inspection.Services[0].Status != "failed" || !strings.Contains(strings.Join(inspection.Services[0].Blockers, "; "), "identity missing") {
		t.Fatalf("inspection = %+v, want failed closed on missing repository identity", inspection.Services[0])
	}
	if len(client.merges) != 0 {
		t.Fatalf("MergeMR calls = %v, want 0 for legacy identity", client.merges)
	}
}

func newReleaseMergeTestManager(t *testing.T) (*manager, *mockGitClient, *releaseMergeForgeClient) {
	t.Helper()
	gitMock := &mockGitClient{remoteURLRes: "git@gitlab.com:group/repo.git"}
	m, _ := newReleasePlanTestManager(t, gitMock)
	m.flow.ProductionBranch = "master"
	client := &releaseMergeForgeClient{mergedNumbers: map[int]bool{}, readCount: map[int]int{}}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitLab: client}
	return m, gitMock, client
}

func releaseMergeService(name string, number int) domain.ReleaseService {
	return domain.ReleaseService{
		Name:          name,
		RepoPath:      "/repos/" + name,
		ReleaseBranch: "release/" + name,
		Status:        domain.ReleaseStatusAwaitingMasterMerge,
		ProductionMR: &domain.ProductionMRRef{
			Number:       number,
			URL:          "mr",
			SourceSHA:    name + "-source",
			State:        "open",
			Repo:         "gitlab.com/group/repo",
			ProviderHost: "gitlab.com",
		},
	}
}

func openProductionMR(number int, name, head string) forge.MRReadiness {
	return forge.MRReadiness{
		Number: number, State: "open", SourceBranch: "release/" + name, TargetBranch: "master",
		HeadSHA: head, Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true,
	}
}

func mergedProductionMR(number int, name, head, mergedSHA string) forge.MRReadiness {
	return forge.MRReadiness{
		Number: number, State: "merged", SourceBranch: "release/" + name, TargetBranch: "master",
		HeadSHA: head, MergedSHA: mergedSHA,
	}
}
