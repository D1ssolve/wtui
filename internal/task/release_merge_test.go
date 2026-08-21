package task

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

type releaseMergeForgeClient struct {
	readiness map[int]forge.MRReadiness
	readErrs  map[int]error
	mergeErrs map[int]error
	mergeSHAs map[int]string
	merges    []forge.MergeMRParams
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
	return f.readiness[number], f.readErrs[number]
}
func (f *releaseMergeForgeClient) MergeMR(_ context.Context, params forge.MergeMRParams) (forge.MRMergeResult, error) {
	f.merges = append(f.merges, params)
	if err := f.mergeErrs[params.Number]; err != nil {
		return forge.MRMergeResult{}, err
	}
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
		1: {Number: 1, State: "open", HeadSHA: "api-source", Ready: true, SupportsSHAPin: true},
		2: {Number: 2, State: "open", HeadSHA: "worker-source", Ready: true, SupportsSHAPin: true},
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
	if len(gitMock.fetchCalls) != 0 {
		t.Fatalf("fetch calls = %v, want no fallback branch guess", gitMock.fetchCalls)
	}
	if client.merges[0].ExpectedHeadSHA != "api-source" || client.merges[1].ExpectedHeadSHA != "worker-source" {
		t.Fatalf("merges = %#v, want manifest source SHA pins", client.merges)
	}
	if client.merges[0].Method != "squash" || client.merges[1].Method != "squash" {
		t.Fatalf("merges = %#v, want squash method", client.merges)
	}
}

func TestReleaseMerge_BlockedServiceLeavesPartialReleaseRetryable(t *testing.T) {
	m, _, client := newReleaseMergeTestManager(t)
	client.readiness = map[int]forge.MRReadiness{
		1: {Number: 1, State: "open", Ready: true, SupportsSHAPin: true},
		2: {Number: 2, State: "open", Blockers: []string{"checks failing"}},
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
		1: {Number: 1, State: "open", Ready: true, SupportsSHAPin: true},
		2: {Number: 2, State: "open", Ready: true, SupportsSHAPin: true},
	}
	client.mergeErrs = map[int]error{1: headDrift}
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
	client.readiness = map[int]forge.MRReadiness{1: {Number: 1, State: "merged"}}
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
		{name: "fast-forward", readiness: forge.MRReadiness{Number: 1, State: "merged", HeadSHA: "api-source"}, targetSHA: "api-source", wantSHA: "api-source"},
		{name: "squash", readiness: forge.MRReadiness{Number: 1, State: "merged", HeadSHA: "api-source", MergedSHA: "squash-sha"}, targetSHA: "later-master", wantSHA: "squash-sha"},
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
	client.readiness = map[int]forge.MRReadiness{1: {Number: 1, State: "merged", HeadSHA: "api-source", TargetBranch: "master"}}
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
		1: {Number: 1, State: "merged"},
		2: {Number: 2, State: "merged"},
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
	client.readiness = map[int]forge.MRReadiness{7: {Number: 7, State: "open", HeadSHA: "api-source", Ready: true, SupportsSHAPin: true}}
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, releaseMergeService("api", 7))

	got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatalf("MergeReleaseMRs() error = %v", err)
	}
	if !slices.Equal(result.Failed, []string{"api"}) || got.Services[0].AcceptedMergeSHA != "" || got.Services[0].Status != domain.ReleaseStatusFailed || got.Services[0].Error == nil || !got.Services[0].Error.Recoverable || len(gitMock.fetchCalls) != 1 {
		t.Fatalf("result = %#v, service = %#v, fetches = %v", result, got.Services[0], gitMock.fetchCalls)
	}
}

func TestReleaseMerge_UnpinnedHeadDriftSkipsMerge(t *testing.T) {
	m, _, client := newReleaseMergeTestManager(t)
	client.readiness = map[int]forge.MRReadiness{9: {Number: 9, State: "open", HeadSHA: "changed", Ready: true, SupportsSHAPin: false}}
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, releaseMergeService("api", 9))

	got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatalf("MergeReleaseMRs() error = %v", err)
	}
	if len(client.merges) != 0 || !slices.Equal(result.Failed, []string{"api"}) || got.Services[0].AcceptedMergeSHA != "" {
		t.Fatalf("merges = %#v, result = %#v, service = %#v", client.merges, result, got.Services[0])
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

func newReleaseMergeTestManager(t *testing.T) (*manager, *mockGitClient, *releaseMergeForgeClient) {
	t.Helper()
	gitMock := &mockGitClient{remoteURLRes: "git@gitlab.com:group/repo.git"}
	m, _ := newReleasePlanTestManager(t, gitMock)
	m.flow.ProductionBranch = "master"
	client := &releaseMergeForgeClient{}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitLab: client}
	return m, gitMock, client
}

func releaseMergeService(name string, number int) domain.ReleaseService {
	return domain.ReleaseService{
		Name:          name,
		RepoPath:      "/repos/" + name,
		ReleaseBranch: "release/" + name,
		Status:        domain.ReleaseStatusAwaitingMasterMerge,
		ProductionMR:  &domain.ProductionMRRef{Number: number, URL: "mr", SourceSHA: name + "-source", State: "open"},
	}
}
