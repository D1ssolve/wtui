package task

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestClassifyCleanupDependency_StatusPolicy(t *testing.T) {
	active := []domain.ReleaseStatus{
		domain.ReleaseStatusDraft,
		domain.ReleaseStatusValidating,
		domain.ReleaseStatusMerging,
		domain.ReleaseStatusBranching,
		domain.ReleaseStatusPushing,
		domain.ReleaseStatusAwaitingTaskMerge,
		domain.ReleaseStatusIntegratingTasks,
		domain.ReleaseStatusTaskMergeBlocked,
		domain.ReleaseStatusTaskMergePartial,
		domain.ReleaseStatusPrepared,
		domain.ReleaseStatusAwaitingMasterMerge,
		domain.ReleaseStatusMasterMerged,
		domain.ReleaseStatusSyncingDevelop,
		domain.ReleaseStatusTagging,
	}
	for _, status := range active {
		got := classifyCleanupDependency(domain.Release{Status: status})
		if !got.blocks || got.released {
			t.Fatalf("status %q = %+v, want active blocker without released proof", status, got)
		}
	}

	released := classifyCleanupDependency(domain.Release{Status: domain.ReleaseStatusReleased})
	if released.blocks || !released.released {
		t.Fatalf("released = %+v, want proof without blocking", released)
	}

	rejected := classifyCleanupDependency(domain.Release{Status: domain.ReleaseStatusRejected})
	if rejected.blocks || rejected.released {
		t.Fatalf("rejected = %+v, want terminal: no block, no proof", rejected)
	}

	recoverableFailed := classifyCleanupDependency(domain.Release{
		Status: domain.ReleaseStatusFailed,
		Error:  &domain.ReleaseError{Recoverable: true},
	})
	if !recoverableFailed.blocks || recoverableFailed.released {
		t.Fatalf("recoverable failed = %+v, want blocker without released proof", recoverableFailed)
	}

	terminalFailed := classifyCleanupDependency(domain.Release{
		Status: domain.ReleaseStatusFailed,
		Error:  &domain.ReleaseError{Recoverable: false},
	})
	if terminalFailed.blocks || terminalFailed.released {
		t.Fatalf("nonrecoverable failed = %+v, want terminal: no block, no proof", terminalFailed)
	}

	malformedFailed := classifyCleanupDependency(domain.Release{Status: domain.ReleaseStatusFailed})
	if !malformedFailed.blocks || malformedFailed.released {
		t.Fatalf("failed without error metadata = %+v, want fail-closed blocker", malformedFailed)
	}

	for _, status := range []domain.ReleaseStatus{"", "bogus"} {
		unknown := classifyCleanupDependency(domain.Release{Status: status})
		if !unknown.blocks || unknown.released {
			t.Fatalf("unknown status %q = %+v, want fail-closed blocker", status, unknown)
		}
	}
}

func TestPlanTaskCleanup_RejectedReleaseDoesNotBlock(t *testing.T) {
	mgr, _ := taskCleanupTestManager(t)
	writeTaskCleanupStatusReleaseManifest(t, mgr, domain.ReleaseStatusRejected, nil)

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Blocked() {
		t.Fatalf("rejected release blocked cleanup: %q", plan.Preview().Blockers)
	}
	if len(plan.steps) == 0 {
		t.Fatal("expected authorized local steps")
	}
}

func TestPlanTaskCleanup_NonRecoverableFailedReleaseDoesNotBlock(t *testing.T) {
	mgr, _ := taskCleanupTestManager(t)
	writeTaskCleanupStatusReleaseManifest(t, mgr, domain.ReleaseStatusFailed, &domain.ReleaseError{Code: "X", Recoverable: false})

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Blocked() {
		t.Fatalf("nonrecoverable failed release blocked cleanup: %q", plan.Preview().Blockers)
	}
}

func TestPlanTaskCleanup_RecoverableFailedReleaseBlocks(t *testing.T) {
	mgr, _ := taskCleanupTestManager(t)
	writeTaskCleanupStatusReleaseManifest(t, mgr, domain.ReleaseStatusFailed, &domain.ReleaseError{Code: "X", Recoverable: true})

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Blocked() || len(plan.steps) != 0 {
		t.Fatalf("blocked = %v, steps = %+v", plan.Blocked(), plan.steps)
	}
	if !strings.Contains(strings.Join(plan.Preview().Blockers, "\n"), "non-released") {
		t.Fatalf("blockers = %q", plan.Preview().Blockers)
	}
}

func TestPlanTaskCleanup_UnknownReleaseStatusFailsClosed(t *testing.T) {
	mgr, _ := taskCleanupTestManager(t)
	writeTaskCleanupStatusReleaseManifest(t, mgr, "bogus", nil)

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Blocked() || len(plan.steps) != 0 {
		t.Fatalf("blocked = %v, steps = %+v", plan.Blocked(), plan.steps)
	}
}

func TestPlanTaskCleanup_RejectedReleaseNeverSuppliesReleasedProof(t *testing.T) {
	mgr, _ := taskCleanupTestManager(t)
	useIntegrationOnlyFeatureFlow(mgr)
	writeTaskCleanupStatusReleaseManifest(t, mgr, domain.ReleaseStatusRejected, nil)

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Blocked() || len(plan.steps) != 0 {
		t.Fatalf("blocked = %v, steps = %+v", plan.Blocked(), plan.steps)
	}
	if !strings.Contains(strings.Join(plan.Preview().Blockers, "\n"), "released manifest") {
		t.Fatalf("blockers = %q", plan.Preview().Blockers)
	}
}

// writeTaskCleanupStatusReleaseManifest writes an involved release manifest
// with an explicit status/error so dependency policy can be exercised directly.
func writeTaskCleanupStatusReleaseManifest(t *testing.T, mgr *manager, status domain.ReleaseStatus, releaseErr *domain.ReleaseError) {
	t.Helper()
	release := domain.Release{
		ManifestVersion: releaseManifestVersion, ID: "rel-1", Dir: filepath.Join(mgr.releasesRootDir(), "rel-1"), Status: status, TaskIDs: []string{"APP-1"},
		Tasks: []domain.ReleaseTaskRef{{TaskID: "APP-1", TaskDir: filepath.Join(mgr.cfg.TasksRoot, "APP-1"), ServiceNames: []string{"svc"}}},
		Services: []domain.ReleaseService{{
			Name: "svc", RepoPath: filepath.Join(mgr.cfg.RootDir, "svc"), IntegrationBranch: "develop", ReleaseBranch: "release/1.0.0", Tag: "v1.0.0", ReleaseSHA: taskCleanupReleaseSHA, AcceptedMergeSHA: taskCleanupAccepted, PushedTag: true,
			FeatureBranches: []domain.ReleaseFeatureBranch{{TaskID: "APP-1", ServiceName: "svc", Branch: "feature/APP-1", WorktreePath: filepath.Join(mgr.cfg.TasksRoot, "APP-1", "svc"), Merged: true, MergeRef: taskCleanupTaskSHA}},
		}},
		Error: releaseErr,
	}
	if _, err := mgr.writeReleaseManifest(release); err != nil {
		t.Fatal(err)
	}
}
