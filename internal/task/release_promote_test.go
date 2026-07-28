package task

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
)

type promoteForgeClient struct {
	creates   []forge.CreateMRParams
	createErr error
}

func (f *promoteForgeClient) Provider() forge.ForgeProvider    { return forge.ForgeProviderGitLab }
func (f *promoteForgeClient) IsAvailable(context.Context) bool { return true }
func (f *promoteForgeClient) CreateMR(_ context.Context, params forge.CreateMRParams) (forge.MRInfo, error) {
	f.creates = append(f.creates, params)
	if f.createErr != nil {
		return forge.MRInfo{}, f.createErr
	}
	return forge.MRInfo{Number: len(f.creates), URL: "https://gitlab.com/mr/" + params.SourceBranch, State: "open"}, nil
}
func (*promoteForgeClient) MRStatus(context.Context, string, string) ([]forge.MRInfo, error) {
	return nil, nil
}
func (*promoteForgeClient) MRReadiness(context.Context, string, string, string) (forge.MRReadiness, error) {
	return forge.MRReadiness{}, nil
}
func (*promoteForgeClient) MRReadinessByNumber(context.Context, int, string, string) (forge.MRReadiness, error) {
	return forge.MRReadiness{}, nil
}
func (*promoteForgeClient) MergeMR(context.Context, forge.MergeMRParams) (forge.MRMergeResult, error) {
	return forge.MRMergeResult{}, nil
}
func (*promoteForgeClient) PipelineStatus(context.Context, string, string) ([]forge.PipelineStatus, error) {
	return nil, nil
}
func (*promoteForgeClient) TriggerPipeline(context.Context, forge.TriggerPipelineParams) error {
	return nil
}
func (*promoteForgeClient) ListIssues(context.Context, forge.ListIssuesParams) ([]forge.IssueInfo, error) {
	return nil, nil
}

func TestPromoteRelease_CreatesProductionMRsAndPersists(t *testing.T) {
	m, gitMock, client := newPromoteTestManager(t)
	release := writePromoteRelease(t, m, domain.ReleaseStatusPrepared,
		promoteService("api", "/repos/api", "release/1.2.3"),
		promoteService("worker", "/repos/worker", "release/1.2.3"),
	)

	got, err := m.PromoteRelease(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatalf("PromoteRelease() error = %v", err)
	}
	if got.Status != domain.ReleaseStatusAwaitingMasterMerge || len(client.creates) != 2 {
		t.Fatalf("status = %q, creates = %d; want awaiting_master_merge, 2", got.Status, len(client.creates))
	}
	for i, svc := range got.Services {
		if svc.ProductionMR == nil || svc.ProductionMR.Number == 0 || svc.ProductionMR.SourceSHA != svc.ReleaseBranch+"-sha" || svc.ProductionMR.State != "open" {
			t.Fatalf("service[%d].ProductionMR = %#v", i, svc.ProductionMR)
		}
		params := client.creates[i]
		if params.SourceBranch != svc.ReleaseBranch || params.TargetBranch != "master" || params.Repo != "gitlab.com/group/repo" {
			t.Fatalf("create[%d] = %#v", i, params)
		}
	}
	if len(gitMock.resolveRefCalls) != 2 {
		t.Fatalf("ResolveRef calls = %v, want 2", gitMock.resolveRefCalls)
	}
	persisted, err := m.GetRelease(t.Context(), release.ID)
	if err != nil || persisted.Status != domain.ReleaseStatusAwaitingMasterMerge || persisted.Services[1].ProductionMR == nil {
		t.Fatalf("persisted = %#v, error = %v", persisted, err)
	}
}

func TestPromoteRelease_SkipsPersistedOpenMR(t *testing.T) {
	m, _, client := newPromoteTestManager(t)
	svc := promoteService("api", "/repos/api", "release/1.2.3")
	svc.ProductionMR = &domain.ProductionMRRef{Number: 42, URL: "existing", SourceSHA: "existing-sha", State: "open"}
	release := writePromoteRelease(t, m, domain.ReleaseStatusPrepared, svc)

	got, err := m.PromoteRelease(t.Context(), release.ID, nil)
	if err != nil {
		t.Fatalf("PromoteRelease() error = %v", err)
	}
	if len(client.creates) != 0 || got.Services[0].ProductionMR.Number != 42 {
		t.Fatalf("creates = %d, ProductionMR = %#v", len(client.creates), got.Services[0].ProductionMR)
	}
}

func TestPromoteRelease_RejectsLegacyManifest(t *testing.T) {
	m, _, client := newPromoteTestManager(t)
	release := writePromoteRelease(t, m, domain.ReleaseStatusPrepared, promoteService("api", "/repos/api", "release/1.2.3"))
	release.ManifestVersion = 1
	data, err := json.Marshal(release)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.releaseManifestPath(release.ID), data, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = m.PromoteRelease(t.Context(), release.ID, nil)
	if !errors.Is(err, ErrReleaseLegacyManifest) || len(client.creates) != 0 {
		t.Fatalf("error = %v, creates = %d", err, len(client.creates))
	}
}

func TestPromoteRelease_RejectsNonPreparedStatus(t *testing.T) {
	m, _, client := newPromoteTestManager(t)
	release := writePromoteRelease(t, m, domain.ReleaseStatusDraft, promoteService("api", "/repos/api", "release/1.2.3"))

	_, err := m.PromoteRelease(t.Context(), release.ID, nil)
	if !errors.Is(err, ErrReleaseInvalidStatusTransition) || len(client.creates) != 0 {
		t.Fatalf("error = %v, creates = %d", err, len(client.creates))
	}
}

func TestPromoteRelease_ForgeFailureMarksReleaseFailed(t *testing.T) {
	m, _, client := newPromoteTestManager(t)
	client.createErr = errors.New("forge unavailable")
	release := writePromoteRelease(t, m, domain.ReleaseStatusPrepared, promoteService("api", "/repos/api", "release/1.2.3"))

	got, err := m.PromoteRelease(t.Context(), release.ID, nil)
	if err == nil || got.Status != domain.ReleaseStatusFailed || got.Error == nil || got.Services[0].Status != domain.ReleaseStatusFailed {
		t.Fatalf("release = %#v, error = %v", got, err)
	}
	persisted, loadErr := m.GetRelease(t.Context(), release.ID)
	if loadErr != nil || persisted.Status != domain.ReleaseStatusFailed || persisted.Error == nil {
		t.Fatalf("persisted = %#v, error = %v", persisted, loadErr)
	}
}

func newPromoteTestManager(t *testing.T) (*manager, *mockGitClient, *promoteForgeClient) {
	t.Helper()
	gitMock := &mockGitClient{remoteURLRes: "git@gitlab.com:group/repo.git"}
	m, _ := newReleasePlanTestManager(t, gitMock)
	m.flow.ProductionBranch = "master"
	client := &promoteForgeClient{}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitLab: client}
	return m, gitMock, client
}

func writePromoteRelease(t *testing.T, m *manager, status domain.ReleaseStatus, services ...domain.ReleaseService) domain.Release {
	t.Helper()
	release := domain.Release{ID: "rel-promote-1", Status: status, Checkpoint: string(status), Version: "1.2.3", Services: services}
	written, err := m.writeReleaseManifest(release)
	if err != nil {
		t.Fatal(err)
	}
	return written
}

func promoteService(name, repoPath, branch string) domain.ReleaseService {
	return domain.ReleaseService{Name: name, RepoPath: repoPath, ReleaseBranch: branch, Version: "1.2.3", Status: domain.ReleaseStatusPrepared}
}
