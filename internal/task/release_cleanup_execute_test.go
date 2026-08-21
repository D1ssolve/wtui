package task

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestExecuteReleaseCleanup_SerialManifestLast(t *testing.T) {
	mgr, gitMock := cleanupTestManager(t, "released")
	plan, err := mgr.PlanReleaseCleanup(t.Context(), "rel-1", DefaultReleaseCleanupSelection())
	if err != nil {
		t.Fatal(err)
	}
	gitMock.removeWorktreeFn = func(_, path string, force bool) error {
		if force {
			t.Fatal("force removal used")
		}
		for i, entry := range gitMock.listWorktreesRes {
			if entry.Path == path {
				gitMock.listWorktreesRes = append(gitMock.listWorktreesRes[:i], gitMock.listWorktreesRes[i+1:]...)
				break
			}
		}
		return os.RemoveAll(path)
	}
	result, err := mgr.ExecuteReleaseCleanup(t.Context(), plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Completed) == 0 {
		t.Fatal("no progress results")
	}
	if _, err := os.Stat(mgr.releaseManifestPath("rel-1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest still exists: %v", err)
	}
}

func TestExecuteReleaseCleanup_StopsOnFirstFailureAndPreservesManifest(t *testing.T) {
	mgr, gitMock := cleanupTestManager(t, "released")
	plan, err := mgr.PlanReleaseCleanup(t.Context(), "rel-1", DefaultReleaseCleanupSelection())
	if err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("boom")
	gitMock.removeWorktreeErr = wantErr
	_, err = mgr.ExecuteReleaseCleanup(t.Context(), plan, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v", err)
	}
	if _, statErr := os.Stat(mgr.releaseManifestPath("rel-1")); statErr != nil {
		t.Fatalf("manifest removed: %v", statErr)
	}
	if gitMock.deleteBranchCalls != 0 {
		t.Fatal("continued after failure")
	}
}

func TestExecuteReleaseCleanup_TargetMovementBlocksBranchDeletion(t *testing.T) {
	const sourceSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const plannedTargetSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const movedTargetSHA = "cccccccccccccccccccccccccccccccccccccccc"
	for _, tc := range []struct {
		name   string
		kind   releaseCleanupStepKind
		remote bool
	}{
		{"local task", cleanupLocalTaskBranch, false},
		{"remote task", cleanupRemoteTaskBranch, true},
		{"local release", cleanupLocalReleaseBranch, false},
		{"remote release", cleanupRemoteReleaseBranch, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr, gitMock := cleanupTestManager(t, "released")
			deleted := false
			gitMock.listWorktreesRes = nil
			gitMock.resolveRefFn = func(_, _ string) (string, error) { return sourceSHA, nil }
			gitMock.remoteRefSHAFn = func(_, ref string) (string, error) {
				if ref == "refs/heads/develop" {
					return movedTargetSHA, nil
				}
				return sourceSHA, nil
			}
			gitMock.isAncestorFn = func(_, ancestor, descendant string) (bool, error) {
				return ancestor == sourceSHA && descendant != movedTargetSHA, nil
			}
			gitMock.deleteBranchIfUnchangedFn = func(_, _, _ string) error { deleted = true; return nil }
			gitMock.deleteRemoteBranchIfUnchangedFn = func(_, _, _ string) error { deleted = true; return nil }
			step := releaseCleanupStep{kind: tc.kind, repoPath: mgr.cfg.RootDir, branch: "feature/APP-1", expectedSHA: sourceSHA,
				targets: []releaseCleanupTarget{{ref: "refs/heads/develop", plannedSHA: plannedTargetSHA}}}
			if !tc.remote {
				gitMock.branchExistsRes = true
			}
			err := mgr.executeReleaseCleanupStep(t.Context(), ReleaseCleanupPlan{}, step)
			if err == nil {
				t.Fatal("target movement did not block deletion")
			}
			if deleted {
				t.Fatal("branch deleted after target lost ancestry")
			}
		})
	}
}

func TestRemoveCleanupWorktree_CommonRepositoryMismatchBlocksRemoval(t *testing.T) {
	mgr, gitMock := cleanupTestManager(t, "released")
	step := releaseCleanupStep{kind: cleanupTaskWorktree, repoPath: mgr.cfg.RootDir, path: filepath.Join(mgr.cfg.TasksRoot, "APP-1", "svc"), branch: "feature/APP-1", expectedSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	gitMock.listWorktreesRes = gitMock.listWorktreesRes[:1]
	gitMock.commonDirFn = func(path string) (string, error) {
		if path == step.path {
			return filepath.Join(mgr.cfg.RootDir, "wrong.git"), nil
		}
		return filepath.Join(mgr.cfg.RootDir, "expected.git"), nil
	}
	if err := mgr.removeCleanupWorktree(context.Background(), step); err == nil {
		t.Fatal("common repository mismatch allowed removal")
	}
	if len(gitMock.removeWorktreeCalls) != 0 {
		t.Fatal("RemoveWorktree called for mismatched repository")
	}
}

func TestExecuteReleaseCleanup_TargetFingerprintDriftBlocksBeforeMutation(t *testing.T) {
	mgr, gitMock := cleanupTestManager(t, "released")
	plan, err := mgr.PlanReleaseCleanup(t.Context(), "rel-1", DefaultReleaseCleanupSelection())
	if err != nil {
		t.Fatal(err)
	}
	base := gitMock.remoteRefSHAFn
	gitMock.remoteRefSHAFn = func(repo, ref string) (string, error) {
		if ref == "refs/heads/develop" {
			return "dddddddddddddddddddddddddddddddddddddddd", nil
		}
		return base(repo, ref)
	}
	if _, err := mgr.ExecuteReleaseCleanup(t.Context(), plan, nil); !errors.Is(err, ErrReleaseCleanupBlocked) {
		t.Fatalf("error = %v", err)
	}
	if len(gitMock.removeWorktreeCalls) != 0 {
		t.Fatal("mutation occurred before fingerprint rejection")
	}
}

func TestExecuteReleaseCleanup_FinalManifestDigestMismatchPreservesRelease(t *testing.T) {
	mgr, _ := cleanupTestManager(t, "released")
	plan, err := mgr.PlanReleaseCleanup(t.Context(), "rel-1", DefaultReleaseCleanupSelection())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mgr.releaseManifestPath("rel-1"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	step := releaseCleanupStep{kind: cleanupReleaseDirectory, path: filepath.Dir(mgr.releaseManifestPath("rel-1"))}
	if err := mgr.executeReleaseCleanupStep(t.Context(), plan, step); err == nil {
		t.Fatal("digest mismatch allowed release removal")
	}
	if _, err := os.Stat(filepath.Dir(mgr.releaseManifestPath("rel-1"))); err != nil {
		t.Fatalf("release directory removed: %v", err)
	}
}

func TestExecuteReleaseCleanup_ReportsEveryStepBeyondChannelCapacity(t *testing.T) {
	mgr, plan := manyNoopCleanupSteps(t, 40)
	statusCh := make(chan string, 1)
	done := make(chan struct {
		result ReleaseCleanupResult
		err    error
	}, 1)
	go func() {
		result, err := mgr.ExecuteReleaseCleanup(t.Context(), plan, statusCh)
		done <- struct {
			result ReleaseCleanupResult
			err    error
		}{result, err}
	}()

	waitForBufferedStatus(t, statusCh)
	statuses := make([]string, 0, len(plan.steps))
	for range plan.steps {
		select {
		case line := <-statusCh:
			statuses = append(statuses, line)
		case <-time.After(2 * time.Second):
			t.Fatalf("received %d/%d status lines", len(statuses), len(plan.steps))
		}
	}
	completed := <-done
	if completed.err != nil {
		t.Fatal(completed.err)
	}
	if len(statuses) <= 32 || len(statuses) != len(completed.result.Completed) {
		t.Fatalf("statuses=%d completed=%d", len(statuses), len(completed.result.Completed))
	}
}

func TestExecuteReleaseCleanup_BlockedStatusSendHonorsCancellation(t *testing.T) {
	mgr, plan := manyNoopCleanupSteps(t, 40)
	statusCh := make(chan string, 1)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := mgr.ExecuteReleaseCleanup(ctx, plan, statusCh)
		done <- err
	}()

	waitForBufferedStatus(t, statusCh)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked cleanup status send ignored cancellation")
	}
}

func manyNoopCleanupSteps(t *testing.T, count int) (*manager, ReleaseCleanupPlan) {
	t.Helper()
	mgr, gitMock := cleanupTestManager(t, domain.ReleaseStatusReleased)
	release, err := mgr.loadReleaseManifest("rel-1")
	if err != nil {
		t.Fatal(err)
	}
	release.TaskIDs = nil
	release.Tasks = nil
	release.Services[0].FeatureBranches = nil
	const taskSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for i := 0; i < count; i++ {
		taskID := fmt.Sprintf("APP-%02d", i)
		worktree := filepath.Join(mgr.cfg.TasksRoot, taskID, "svc")
		release.TaskIDs = append(release.TaskIDs, taskID)
		release.Tasks = append(release.Tasks, domain.ReleaseTaskRef{TaskID: taskID, TaskDir: filepath.Join(mgr.cfg.TasksRoot, taskID), ServiceNames: []string{"svc"}})
		release.Services[0].FeatureBranches = append(release.Services[0].FeatureBranches, domain.ReleaseFeatureBranch{
			TaskID: taskID, ServiceName: "svc", Branch: "feature/" + taskID, WorktreePath: worktree, Merged: true, MergeRef: taskSHA,
		})
	}
	if _, err := mgr.writeReleaseManifest(release); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(mgr.cfg.TasksRoot); err != nil {
		t.Fatal(err)
	}
	gitMock.listWorktreesRes = gitMock.listWorktreesRes[1:]
	gitMock.branchExistsRes = false
	baseRemote := gitMock.remoteRefSHAFn
	gitMock.remoteRefSHAFn = func(repo, ref string) (string, error) {
		if strings.HasPrefix(ref, "refs/heads/feature/APP-") {
			return taskSHA, nil
		}
		return baseRemote(repo, ref)
	}
	selection := ReleaseCleanupSelection{RemoveTasks: true, DeleteLocalTaskBranches: true}
	plan, err := mgr.PlanReleaseCleanup(t.Context(), "rel-1", selection)
	if err != nil || len(plan.Preview().Blockers) != 0 {
		t.Fatalf("plan = %+v, err = %v", plan.Preview(), err)
	}
	if len(plan.steps) <= 32 {
		t.Fatalf("steps = %d, want >32", len(plan.steps))
	}
	return mgr, plan
}

func waitForBufferedStatus(t *testing.T, statusCh chan string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for len(statusCh) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(statusCh) == 0 {
		t.Fatal("cleanup emitted no first status")
	}
}
