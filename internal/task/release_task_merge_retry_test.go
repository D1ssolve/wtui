package task

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
)

func TestPlanReleaseTaskMergeRetry_RejectsForgedMergedWhenMROpen(t *testing.T) {
	// Given
	gitMock := &mockGitClient{remoteURLRes: "git@github.com:org/repo.git"}
	m, _ := newReleasePlanTestManager(t, gitMock)
	enableReleasePrepareTaskMerge(t, m)
	f := newReleaseTaskMergeForge()
	f.readiness[1] = forge.MRReadiness{Number: 1, State: "open", SourceBranch: "feature/APP-1", TargetBranch: "develop", HeadSHA: "head-1", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitHub: f}
	release := writeTaskMergeRetryRelease(t, m, domain.ReleaseStatusTaskMergePartial, domain.ReleaseFeatureBranch{TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", WorktreePath: filepath.Join(m.cfg.TasksRoot, "APP-1", "api"), TaskMergeStatus: taskMergeStatusMerged, TaskMergeMRNumber: 1, TaskMergeHeadSHA: "head-1", MergeRef: "d1"})

	// When
	_, err := m.PlanReleaseTaskMergeRetry(context.Background(), release.ID)

	// Then
	if err == nil {
		t.Fatalf("PlanReleaseTaskMergeRetry() error=nil, want forged merged rejected")
	}
}

func TestRetryReleaseTaskMerges_SkipsProvenMergedAndMergesNext(t *testing.T) {
	// Given
	targetTip := "d1"
	gitMock := &mockGitClient{remoteURLRes: "git@github.com:org/repo.git", resolveRefFn: func(_, ref string) (string, error) {
		if ref == "origin/develop" || ref == "HEAD" || ref == "release/1.2.3" || ref == "origin/release/1.2.3" {
			return targetTip, nil
		}
		return ref + "-sha", nil
	}}
	m, _ := newReleasePlanTestManager(t, gitMock)
	setTaskWorktreeHeads(gitMock, map[string]string{
		filepath.Join(m.cfg.TasksRoot, "APP-1", "api"): "head-1",
		filepath.Join(m.cfg.TasksRoot, "APP-2", "api"): "head-2",
	})
	enableReleasePrepareTaskMerge(t, m)
	f := newReleaseTaskMergeForge()
	f.readiness[1] = forge.MRReadiness{Number: 1, State: "merged", SourceBranch: "feature/APP-1", TargetBranch: "develop", HeadSHA: "head-1", MergedSHA: "d1", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true}
	f.readiness[2] = forge.MRReadiness{Number: 2, State: "open", SourceBranch: "feature/APP-2", TargetBranch: "develop", HeadSHA: "head-2", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true}
	f.afterMerge = func(number int) {
		if number == 2 {
			targetTip = "d2"
			f.readiness[2] = forge.MRReadiness{Number: 2, State: "merged", SourceBranch: "feature/APP-2", TargetBranch: "develop", HeadSHA: "head-2", MergedSHA: "d2", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true}
		}
	}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitHub: f}
	release := writeTaskMergeRetryRelease(t, m, domain.ReleaseStatusTaskMergePartial,
		domain.ReleaseFeatureBranch{TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", WorktreePath: filepath.Join(m.cfg.TasksRoot, "APP-1", "api"), TaskMergeStatus: taskMergeStatusMerged, TaskMergeMRNumber: 1, TaskMergeHeadSHA: "head-1", MergeRef: "d1"},
		domain.ReleaseFeatureBranch{TaskID: "APP-2", ServiceName: "api", Branch: "feature/APP-2", WorktreePath: filepath.Join(m.cfg.TasksRoot, "APP-2", "api"), TaskMergeStatus: taskMergeStatusPending, TaskMergeMRNumber: 2, TaskMergeHeadSHA: "head-2", TaskMergeTargetSHA: "d1"},
	)

	// When
	plan, err := m.PlanReleaseTaskMergeRetry(context.Background(), release.ID)
	if err != nil {
		t.Fatalf("PlanReleaseTaskMergeRetry() error = %v", err)
	}
	got, err := m.RetryReleaseTaskMerges(context.Background(), release.ID, &plan)

	// Then
	if err != nil {
		t.Fatalf("RetryReleaseTaskMerges() error = %v", err)
	}
	if len(f.mergeNumbers) != 1 || f.mergeNumbers[0] != 2 {
		t.Fatalf("merge numbers = %#v, want only MR 2", f.mergeNumbers)
	}
	if got.Services[0].PostIntegrationSHA != "d2" {
		t.Fatalf("PostIntegrationSHA = %q, want d2", got.Services[0].PostIntegrationSHA)
	}
}

func TestRetryReleaseTaskMerges_ReconcilesUnknownRemoteMergedWithoutDuplicate(t *testing.T) {
	// Given
	targetTip := "d1"
	gitMock := &mockGitClient{remoteURLRes: "git@github.com:org/repo.git", resolveRefFn: func(_, ref string) (string, error) {
		if ref == "origin/develop" || ref == "HEAD" || ref == "release/1.2.3" || ref == "origin/release/1.2.3" {
			return targetTip, nil
		}
		return ref + "-sha", nil
	}}
	m, _ := newReleasePlanTestManager(t, gitMock)
	setTaskWorktreeHeads(gitMock, map[string]string{filepath.Join(m.cfg.TasksRoot, "APP-1", "api"): "head-1"})
	enableReleasePrepareTaskMerge(t, m)
	f := newReleaseTaskMergeForge()
	f.readiness[1] = forge.MRReadiness{Number: 1, State: "merged", SourceBranch: "feature/APP-1", TargetBranch: "develop", HeadSHA: "head-1", MergedSHA: "d1", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitHub: f}
	release := writeTaskMergeRetryRelease(t, m, domain.ReleaseStatusTaskMergePartial,
		domain.ReleaseFeatureBranch{TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", WorktreePath: filepath.Join(m.cfg.TasksRoot, "APP-1", "api"), TaskMergeStatus: taskMergeStatusUnknown, TaskMergeMRNumber: 1, TaskMergeHeadSHA: "head-1", TaskMergeExpectedTarget: "d0"},
	)

	// When
	plan, err := m.PlanReleaseTaskMergeRetry(context.Background(), release.ID)
	if err != nil {
		t.Fatalf("PlanReleaseTaskMergeRetry() error = %v", err)
	}
	got, err := m.RetryReleaseTaskMerges(context.Background(), release.ID, &plan)

	// Then
	if err != nil {
		t.Fatalf("RetryReleaseTaskMerges() error = %v", err)
	}
	if f.mergeCalls != 0 {
		t.Fatalf("merge calls = %d, want none", f.mergeCalls)
	}
	if got.Services[0].FeatureBranches[0].TaskMergeStatus != taskMergeStatusMerged || got.Services[0].FeatureBranches[0].MergeRef != "d1" {
		t.Fatalf("feature branch = %#v", got.Services[0].FeatureBranches[0])
	}
}

func TestPlanReleaseTaskMergeRetry_BlocksUnknownOpenWhenTargetMoved(t *testing.T) {
	// Given
	gitMock := &mockGitClient{remoteURLRes: "git@github.com:org/repo.git", resolveRefFn: func(_, ref string) (string, error) {
		if ref == "origin/develop" {
			return "d2", nil
		}
		return ref + "-sha", nil
	}}
	m, _ := newReleasePlanTestManager(t, gitMock)
	setTaskWorktreeHeads(gitMock, map[string]string{filepath.Join(m.cfg.TasksRoot, "APP-1", "api"): "head-1"})
	enableReleasePrepareTaskMerge(t, m)
	f := newReleaseTaskMergeForge()
	f.readiness[1] = forge.MRReadiness{Number: 1, State: "open", SourceBranch: "feature/APP-1", TargetBranch: "develop", HeadSHA: "head-1", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitHub: f}
	release := writeTaskMergeRetryRelease(t, m, domain.ReleaseStatusTaskMergePartial,
		domain.ReleaseFeatureBranch{TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", WorktreePath: filepath.Join(m.cfg.TasksRoot, "APP-1", "api"), TaskMergeStatus: taskMergeStatusUnknown, TaskMergeMRNumber: 1, TaskMergeHeadSHA: "head-1", TaskMergeExpectedTarget: "d1"},
	)

	// When
	plan, err := m.PlanReleaseTaskMergeRetry(context.Background(), release.ID)

	// Then
	if err != nil {
		t.Fatalf("PlanReleaseTaskMergeRetry() error = %v", err)
	}
	if len(plan.Rows) != 1 || plan.Rows[0].Ready || plan.Rows[0].Status != taskMergeStatusUnknown {
		t.Fatalf("retry row = %#v, want blocked unknown", plan.Rows)
	}
}

func TestPlanReleaseTaskMergeRetry_AwaitingCompleteMetadata_PreviewsFromPersistedState(t *testing.T) {
	// Given
	gitMock := &mockGitClient{remoteURLRes: "git@github.com:org/repo.git", resolveRefFn: func(_, ref string) (string, error) {
		if ref == "origin/develop" {
			return "d1", nil
		}
		return ref + "-sha", nil
	}}
	m, _ := newReleasePlanTestManager(t, gitMock)
	setTaskWorktreeHeads(gitMock, map[string]string{filepath.Join(m.cfg.TasksRoot, "APP-1", "api"): "head-1"})
	enableReleasePrepareTaskMerge(t, m)
	f := newReleaseTaskMergeForge()
	f.readiness[1] = forge.MRReadiness{Number: 1, State: "open", SourceBranch: "feature/APP-1", TargetBranch: "develop", HeadSHA: "head-1", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitHub: f}
	release := writeTaskMergeRetryRelease(t, m, domain.ReleaseStatusAwaitingTaskMerge,
		domain.ReleaseFeatureBranch{TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", WorktreePath: filepath.Join(m.cfg.TasksRoot, "APP-1", "api"), TaskMergeStatus: taskMergeStatusPending, TaskMergeMRNumber: 1, TaskMergeHeadSHA: "head-1", TaskMergeTargetSHA: "d1"},
	)

	// When
	plan, err := m.PlanReleaseTaskMergeRetry(context.Background(), release.ID)

	// Then
	if err != nil {
		t.Fatalf("PlanReleaseTaskMergeRetry() error = %v", err)
	}
	if len(plan.Rows) != 1 || !plan.Rows[0].Ready || plan.Rows[0].MRNumber != 1 || plan.Rows[0].HeadSHA != "head-1" || plan.Rows[0].TargetSHA != "d1" {
		t.Fatalf("retry rows = %#v, want single ready row from persisted state", plan.Rows)
	}
}

func TestPlanReleaseTaskMergeRetry_AwaitingWithoutMetadata_ReplansFromFreshForgeState(t *testing.T) {
	// Given
	gitMock := &mockGitClient{remoteURLRes: "git@github.com:org/repo.git", resolveRefFn: func(_, ref string) (string, error) {
		if ref == "origin/develop" {
			return "d0", nil
		}
		return ref + "-sha", nil
	}}
	m, _ := newReleasePlanTestManager(t, gitMock)
	setTaskWorktreeHeads(gitMock, map[string]string{filepath.Join(m.cfg.TasksRoot, "APP-1", "api"): "fresh-head"})
	enableReleasePrepareTaskMerge(t, m)
	f := newReleaseTaskMergeForge()
	f.readiness[7] = forge.MRReadiness{Number: 7, State: "open", SourceBranch: "feature/APP-1", TargetBranch: "develop", HeadSHA: "fresh-head", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitHub: f}
	release := writeTaskMergeRetryRelease(t, m, domain.ReleaseStatusAwaitingTaskMerge,
		domain.ReleaseFeatureBranch{TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", WorktreePath: filepath.Join(m.cfg.TasksRoot, "APP-1", "api")},
	)

	// When
	plan, err := m.PlanReleaseTaskMergeRetry(context.Background(), release.ID)

	// Then
	if err != nil {
		t.Fatalf("PlanReleaseTaskMergeRetry() error = %v", err)
	}
	if len(plan.Rows) != 1 || !plan.Rows[0].Ready || plan.Rows[0].MRNumber != 7 || plan.Rows[0].HeadSHA != "fresh-head" || plan.Rows[0].TargetSHA != "d0" {
		t.Fatalf("retry rows = %#v, want single fresh planned row", plan.Rows)
	}
}

func TestPlanReleaseTaskMergeRetry_AwaitingIncompleteMetadata_FailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name     string
		branches []domain.ReleaseFeatureBranch
	}{
		{name: "mixed metadata", branches: []domain.ReleaseFeatureBranch{
			{TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", TaskMergeStatus: taskMergeStatusPending, TaskMergeMRNumber: 1, TaskMergeHeadSHA: "head-1", TaskMergeTargetSHA: "d1"},
			{TaskID: "APP-2", ServiceName: "api", Branch: "feature/APP-2"},
		}},
		{name: "attempted without proof", branches: []domain.ReleaseFeatureBranch{
			{TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", TaskMergeStatus: taskMergeStatusAttempting, TaskMergeMRNumber: 1, TaskMergeHeadSHA: "head-1", TaskMergeTargetSHA: "d1"},
		}},
		{name: "MR number without head", branches: []domain.ReleaseFeatureBranch{
			{TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", TaskMergeMRNumber: 1},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			gitMock := &mockGitClient{remoteURLRes: "git@github.com:org/repo.git"}
			m, _ := newReleasePlanTestManager(t, gitMock)
			enableReleasePrepareTaskMerge(t, m)
			f := newReleaseTaskMergeForge()
			f.readiness[1] = forge.MRReadiness{Number: 1, State: "open", SourceBranch: "feature/APP-1", TargetBranch: "develop", HeadSHA: "head-1", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true}
			m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitHub: f}
			release := writeTaskMergeRetryRelease(t, m, domain.ReleaseStatusAwaitingTaskMerge, tc.branches...)

			// When
			_, err := m.PlanReleaseTaskMergeRetry(context.Background(), release.ID)

			// Then
			if !errors.Is(err, ErrReleaseRetryUnsafe) {
				t.Fatalf("PlanReleaseTaskMergeRetry() error = %v, want ErrReleaseRetryUnsafe", err)
			}
		})
	}
}

func TestRetryReleaseTaskMerges_AwaitingFreshPlan_ExecutesAfterConfirmation(t *testing.T) {
	// Given
	targetTip := "d0"
	gitMock := &mockGitClient{remoteURLRes: "git@github.com:org/repo.git", resolveRefFn: func(_, ref string) (string, error) {
		if ref == "origin/develop" || ref == "HEAD" || ref == "release/1.2.3" || ref == "origin/release/1.2.3" {
			return targetTip, nil
		}
		return ref + "-sha", nil
	}}
	m, _ := newReleasePlanTestManager(t, gitMock)
	setTaskWorktreeHeads(gitMock, map[string]string{filepath.Join(m.cfg.TasksRoot, "APP-1", "api"): "fresh-head"})
	enableReleasePrepareTaskMerge(t, m)
	f := newReleaseTaskMergeForge()
	f.readiness[7] = forge.MRReadiness{Number: 7, State: "open", SourceBranch: "feature/APP-1", TargetBranch: "develop", HeadSHA: "fresh-head", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true}
	f.afterMerge = func(number int) {
		if number == 7 {
			targetTip = "d1"
			f.readiness[7] = forge.MRReadiness{Number: 7, State: "merged", SourceBranch: "feature/APP-1", TargetBranch: "develop", HeadSHA: "fresh-head", MergedSHA: "d1", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true}
		}
	}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitHub: f}
	release := writeTaskMergeRetryRelease(t, m, domain.ReleaseStatusAwaitingTaskMerge,
		domain.ReleaseFeatureBranch{TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", WorktreePath: filepath.Join(m.cfg.TasksRoot, "APP-1", "api")},
	)

	// When
	plan, err := m.PlanReleaseTaskMergeRetry(context.Background(), release.ID)
	if err != nil {
		t.Fatalf("PlanReleaseTaskMergeRetry() error = %v", err)
	}
	got, err := m.RetryReleaseTaskMerges(context.Background(), release.ID, &plan)

	// Then
	if err != nil {
		t.Fatalf("RetryReleaseTaskMerges() error = %v", err)
	}
	if len(f.mergeNumbers) != 1 || f.mergeNumbers[0] != 7 {
		t.Fatalf("merge numbers = %#v, want only MR 7", f.mergeNumbers)
	}
	if len(f.mergeExpectedHeads) != 1 || f.mergeExpectedHeads[0] != "fresh-head" {
		t.Fatalf("merge expected heads = %#v, want fresh-head", f.mergeExpectedHeads)
	}
	if got.Services[0].PostIntegrationSHA != "d1" {
		t.Fatalf("PostIntegrationSHA = %q, want d1", got.Services[0].PostIntegrationSHA)
	}
	if got.Services[0].FeatureBranches[0].TaskMergeMRNumber != 7 || got.Services[0].FeatureBranches[0].TaskMergeHeadSHA != "fresh-head" {
		t.Fatalf("feature branch metadata not persisted from confirmed plan: %#v", got.Services[0].FeatureBranches[0])
	}
}

func TestRetryReleaseTaskMerges_SuccessfulRetry_ClearsStaleFailureState(t *testing.T) {
	// Given
	targetTip := "d1"
	gitMock := &mockGitClient{remoteURLRes: "git@github.com:org/repo.git", resolveRefFn: func(_, ref string) (string, error) {
		if ref == "origin/develop" || ref == "HEAD" || ref == "release/1.2.3" || ref == "origin/release/1.2.3" {
			return targetTip, nil
		}
		return ref + "-sha", nil
	}}
	m, _ := newReleasePlanTestManager(t, gitMock)
	setTaskWorktreeHeads(gitMock, map[string]string{filepath.Join(m.cfg.TasksRoot, "APP-1", "api"): "head-1"})
	enableReleasePrepareTaskMerge(t, m)
	f := newReleaseTaskMergeForge()
	f.readiness[1] = forge.MRReadiness{Number: 1, State: "merged", SourceBranch: "feature/APP-1", TargetBranch: "develop", HeadSHA: "head-1", MergedSHA: "d1", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitHub: f}

	createdAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	startedAt := time.Date(2026, 1, 2, 3, 5, 0, 0, time.UTC)
	completedAt := time.Date(2026, 1, 2, 3, 6, 0, 0, time.UTC)
	staleErr := &domain.ReleaseError{Code: "ERR_STALE", Message: "stale failure"}

	release := writeTaskMergeRetryRelease(t, m, domain.ReleaseStatusTaskMergePartial,
		domain.ReleaseFeatureBranch{TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", WorktreePath: filepath.Join(m.cfg.TasksRoot, "APP-1", "api"), TaskMergeStatus: taskMergeStatusMerged, TaskMergeMRNumber: 1, TaskMergeHeadSHA: "head-1", MergeRef: "d1"},
	)
	release.CreatedAt = createdAt
	release.StartedAt = &startedAt
	release.CompletedAt = &completedAt
	release.Error = staleErr
	release.Services[0].Error = staleErr
	release, err := m.writeReleaseManifest(release)
	if err != nil {
		t.Fatalf("writeReleaseManifest() error = %v", err)
	}

	// When
	plan, err := m.PlanReleaseTaskMergeRetry(context.Background(), release.ID)
	if err != nil {
		t.Fatalf("PlanReleaseTaskMergeRetry() error = %v", err)
	}
	got, err := m.RetryReleaseTaskMerges(context.Background(), release.ID, &plan)

	// Then
	if err != nil {
		t.Fatalf("RetryReleaseTaskMerges() error = %v", err)
	}
	if got.Status != domain.ReleaseStatusPrepared {
		t.Fatalf("Status = %s, want prepared", got.Status)
	}
	if got.PreparedAt == nil || got.PreparedAt.Location() != time.UTC {
		t.Fatalf("PreparedAt = %v, want non-nil UTC", got.PreparedAt)
	}
	if got.Error != nil {
		t.Fatalf("Error = %v, want nil", got.Error)
	}
	if got.CompletedAt != nil {
		t.Fatalf("CompletedAt = %v, want nil", got.CompletedAt)
	}
	if !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("CreatedAt = %v, want %v", got.CreatedAt, createdAt)
	}
	if got.StartedAt == nil || !got.StartedAt.Equal(startedAt) {
		t.Fatalf("StartedAt = %v, want %v", got.StartedAt, startedAt)
	}
	if got.Services[0].Error != nil {
		t.Fatalf("service Error = %v, want nil", got.Services[0].Error)
	}

	// And
	reloaded, err := m.GetRelease(context.Background(), release.ID)
	if err != nil {
		t.Fatalf("GetRelease() error = %v", err)
	}
	if reloaded.PreparedAt == nil {
		t.Fatalf("reloaded PreparedAt = nil, want non-nil")
	}
	if reloaded.Error != nil {
		t.Fatalf("reloaded Error = %v, want nil", reloaded.Error)
	}
	if reloaded.CompletedAt != nil {
		t.Fatalf("reloaded CompletedAt = %v, want nil", reloaded.CompletedAt)
	}
	if !reloaded.CreatedAt.Equal(createdAt) {
		t.Fatalf("reloaded CreatedAt = %v, want %v", reloaded.CreatedAt, createdAt)
	}
	if reloaded.Services[0].Error != nil {
		t.Fatalf("reloaded service Error = %v, want nil", reloaded.Services[0].Error)
	}
}

func writeTaskMergeRetryRelease(t *testing.T, m *manager, status domain.ReleaseStatus, branches ...domain.ReleaseFeatureBranch) domain.Release {
	t.Helper()
	release := domain.Release{
		ID: "rel-retry-" + string(status), Status: status, Checkpoint: string(status), TaskIDs: []string{"APP-1", "APP-2"},
		Services:  []domain.ReleaseService{{Name: "api", RepoPath: filepath.Join(m.cfg.RootDir, "repo-api"), IntegrationBranch: "develop", ReleaseBranch: "release/1.2.3", Version: "1.2.3", Tag: "v1.2.3", FeatureBranches: branches}},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	written, err := m.writeReleaseManifest(release)
	if err != nil {
		t.Fatalf("writeReleaseManifest() error = %v", err)
	}
	return written
}
