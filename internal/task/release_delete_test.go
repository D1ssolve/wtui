package task

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestReleaseDeletion_AllStagesPreserveTasks(t *testing.T) {
	for _, status := range []domain.ReleaseStatus{
		domain.ReleaseStatusDraft, domain.ReleaseStatusValidating, domain.ReleaseStatusMerging,
		domain.ReleaseStatusBranching, domain.ReleaseStatusPushing, domain.ReleaseStatusAwaitingTaskMerge,
		domain.ReleaseStatusIntegratingTasks, domain.ReleaseStatusTaskMergeBlocked, domain.ReleaseStatusTaskMergePartial,
		domain.ReleaseStatusPrepared, domain.ReleaseStatusAwaitingMasterMerge, domain.ReleaseStatusMasterMerged,
		domain.ReleaseStatusSyncingDevelop, domain.ReleaseStatusTagging, domain.ReleaseStatusReleased,
		domain.ReleaseStatusFailed, domain.ReleaseStatusRejected,
	} {
		t.Run(string(status), func(t *testing.T) {
			mgr, g := cleanupTestManager(t, status)
			release, err := mgr.loadReleaseManifest("rel-1")
			if err != nil {
				t.Fatal(err)
			}
			// A draft owns no refs yet; names in its plan may already exist.
			if status == domain.ReleaseStatusDraft {
				release.Services[0].ReleaseSHA = ""
				release.Services[0].AcceptedMergeSHA = ""
				release.Services[0].PushedTag = false
				release.Services[0].PushedReleaseBranch = false
				release.Services[0].ReleaseWorktreePath = ""
				if err := os.RemoveAll(filepath.Join(release.Dir, "services")); err != nil {
					t.Fatal(err)
				}
				g.listWorktreesRes = g.listWorktreesRes[:1]
				if _, err := mgr.writeReleaseManifest(release); err != nil {
					t.Fatal(err)
				}
			}
			g.remoteURLRes = "https://example.com/team/svc.git"
			g.tagExistsRes = true
			baseResolve := g.resolveRefFn
			g.resolveRefFn = func(repo, ref string) (string, error) {
				if ref == "refs/tags/v1.0.0" {
					return "dddddddddddddddddddddddddddddddddddddddd", nil
				}
				if ref == "dddddddddddddddddddddddddddddddddddddddd^{commit}" {
					return "cccccccccccccccccccccccccccccccccccccccc", nil
				}
				return baseResolve(repo, ref)
			}
			baseRemote := g.remoteRefSHAFn
			g.remoteRefSHAFn = func(repo, ref string) (string, error) {
				if ref == "refs/tags/v1.0.0" {
					return "dddddddddddddddddddddddddddddddddddddddd", nil
				}
				return baseRemote(repo, ref)
			}
			g.remoteRefSHAAtFn = func(repo, _ string, ref string) (string, error) {
				return g.remoteRefSHAFn(repo, ref)
			}
			g.removeWorktreeFn = func(_, path string, force bool) error {
				if force || path == filepath.Join(mgr.cfg.TasksRoot, "APP-1", "svc") {
					t.Fatal("task or dirty worktree removal authorized")
				}
				g.listWorktreesRes = g.listWorktreesRes[:1]
				return os.RemoveAll(path)
			}
			plan, err := mgr.PlanReleaseCleanup(t.Context(), "rel-1", ReleaseDeletionSelection())
			if err != nil || len(plan.Preview().Blockers) != 0 {
				t.Fatalf("deletion preview = %+v, %v", plan.Preview(), err)
			}
			for _, step := range plan.steps {
				if step.kind == cleanupTaskWorktree || step.kind == cleanupTaskDirectory || step.kind == cleanupLocalTaskBranch || step.kind == cleanupRemoteTaskBranch {
					t.Fatalf("deletion touches a task: %+v", step)
				}
			}
			if _, err := mgr.ExecuteReleaseCleanup(t.Context(), plan, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(release.Dir); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("release directory remains: %v", err)
			}
			if _, err := os.Stat(filepath.Join(mgr.cfg.TasksRoot, "APP-1", "svc")); err != nil {
				t.Fatalf("task worktree lost: %v", err)
			}
		})
	}
}

func TestReleaseDeletion_DirtyWorktreeBlocksBeforeMutation(t *testing.T) {
	mgr, g := cleanupTestManager(t, domain.ReleaseStatusFailed)
	g.isDirtyRes = true
	g.remoteURLRes = "https://example.com/team/svc.git"
	g.remoteRefSHAAtFn = func(repo, _ string, ref string) (string, error) {
		return g.remoteRefSHAFn(repo, ref)
	}
	plan, err := mgr.PlanReleaseCleanup(t.Context(), "rel-1", ReleaseDeletionSelection())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.ExecuteReleaseCleanup(t.Context(), plan, nil); !errors.Is(err, ErrReleaseCleanupBlocked) {
		t.Fatalf("deletion error = %v", err)
	}
	if len(g.removeWorktreeCalls) != 0 {
		t.Fatal("blocked deletion mutated worktrees")
	}
	if _, err := os.Stat(mgr.releaseManifestPath("rel-1")); err != nil {
		t.Fatalf("manifest lost: %v", err)
	}
}

func TestReleaseDeletion_SharedRefsBlockDeletion(t *testing.T) {
	mgr, g := cleanupTestManager(t, domain.ReleaseStatusReleased)
	g.remoteURLRes = "https://example.com/team/svc.git"
	g.remoteRefSHAAtFn = func(repo, _ string, ref string) (string, error) {
		return g.remoteRefSHAFn(repo, ref)
	}
	release, err := mgr.loadReleaseManifest("rel-1")
	if err != nil {
		t.Fatal(err)
	}
	release.ID = "rel-2"
	if _, err := mgr.writeReleaseManifest(release); err != nil {
		t.Fatal(err)
	}
	plan, err := mgr.PlanReleaseCleanup(t.Context(), "rel-1", ReleaseDeletionSelection())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Preview().Blockers) == 0 {
		t.Fatal("release deletion would remove another release's refs")
	}
	if _, err := mgr.ExecuteReleaseCleanup(t.Context(), plan, nil); !errors.Is(err, ErrReleaseCleanupBlocked) {
		t.Fatalf("shared refs deletion error = %v", err)
	}
}

func TestReleaseDeletion_DirectoryRacePreservesManifest(t *testing.T) {
	mgr, g := cleanupTestManager(t, domain.ReleaseStatusReleased)
	g.listWorktreesRes = nil
	releaseDir := filepath.Dir(mgr.releaseManifestPath("rel-1"))
	if err := os.RemoveAll(filepath.Join(releaseDir, "services")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(mgr.releaseManifestPath("rel-1"))
	if err != nil {
		t.Fatal(err)
	}
	pruneScanHook = func(dir string) {
		if dir == releaseDir {
			if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("keep"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Cleanup(func() { pruneScanHook = nil })
	// Inject after validation, just as pruning starts scanning the directory.
	if err := validateReleaseDeletionDirectory(releaseDir, nil); err != nil {
		t.Fatal(err)
	}
	plan := ReleaseCleanupPlan{preview: ReleaseCleanupPreview{ReleaseID: "rel-1", Selection: ReleaseDeletionSelection()}, manifestDigest: sha256.Sum256(data)}
	if err := mgr.executeReleaseCleanupStep(t.Context(), plan, releaseCleanupStep{kind: cleanupReleaseDirectory, path: releaseDir}); err == nil {
		t.Fatal("expected preservation error")
	}
	got, err := os.ReadFile(mgr.releaseManifestPath("rel-1"))
	if err != nil || string(got) != string(data) {
		t.Fatalf("retry manifest lost after pruning failure: %s, %v", got, err)
	}
}
