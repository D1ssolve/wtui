package task

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// Local branch steps never consult merge targets: retention is unconditional
// because nothing is deleted. Remote steps still fail closed on target drift.
func TestExecuteReleaseCleanup_TargetMovementBlocksOnlyRemoteDeletion(t *testing.T) {
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
			if tc.remote {
				if err == nil {
					t.Fatal("target movement did not block remote deletion")
				}
			} else if err != nil {
				t.Fatalf("retention step failed: %v", err)
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
	if _, err := mgr.removeCleanupWorktree(context.Background(), step); err == nil {
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
	if len(statuses) <= 32 || len(statuses) != len(completed.result.Completed)+len(completed.result.Retained) {
		t.Fatalf("statuses=%d completed=%d retained=%d", len(statuses), len(completed.result.Completed), len(completed.result.Retained))
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

func TestExecuteReleaseCleanup_UnknownTaskFilePreservesTaskDirectory(t *testing.T) {
	mgr, gitMock := cleanupTestManager(t, "released")
	gitMock.removeWorktreeFn = func(_, path string, _ bool) error {
		for i, entry := range gitMock.listWorktreesRes {
			if entry.Path == path {
				gitMock.listWorktreesRes = append(gitMock.listWorktreesRes[:i], gitMock.listWorktreesRes[i+1:]...)
				break
			}
		}
		return os.RemoveAll(path)
	}
	taskDir := filepath.Join(mgr.cfg.TasksRoot, "APP-1")
	notesPath := filepath.Join(taskDir, "notes.md")
	if err := os.WriteFile(notesPath, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := mgr.PlanReleaseCleanup(t.Context(), "rel-1", DefaultReleaseCleanupSelection())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Preview().Blockers) != 0 {
		t.Fatalf("plan blocked: %q", plan.Preview().Blockers)
	}

	_, err = mgr.ExecuteReleaseCleanup(t.Context(), plan, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown entries") {
		t.Fatalf("error = %v, want unknown entries retention", err)
	}
	if _, statErr := os.Stat(notesPath); statErr != nil {
		t.Fatalf("unknown file removed: %v", statErr)
	}
	if _, statErr := os.Stat(taskDir); statErr != nil {
		t.Fatalf("task directory removed despite unknown file: %v", statErr)
	}
}

func TestExecuteReleaseCleanup_UnknownReleaseFilePreservesReleaseDirectory(t *testing.T) {
	mgr, gitMock := cleanupTestManager(t, "released")
	gitMock.removeWorktreeFn = func(_, path string, _ bool) error {
		for i, entry := range gitMock.listWorktreesRes {
			if entry.Path == path {
				gitMock.listWorktreesRes = append(gitMock.listWorktreesRes[:i], gitMock.listWorktreesRes[i+1:]...)
				break
			}
		}
		return os.RemoveAll(path)
	}
	releaseDir := filepath.Join(mgr.releasesRootDir(), "rel-1")
	keepPath := filepath.Join(releaseDir, "keep.txt")
	if err := os.WriteFile(keepPath, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	selection := ReleaseCleanupSelection{RemoveRelease: true, DeleteLocalReleaseBranches: true}
	plan, err := mgr.PlanReleaseCleanup(t.Context(), "rel-1", selection)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Preview().Blockers) != 0 {
		t.Fatalf("plan blocked: %q", plan.Preview().Blockers)
	}

	_, err = mgr.ExecuteReleaseCleanup(t.Context(), plan, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown entries") {
		t.Fatalf("error = %v, want unknown entries retention", err)
	}
	if _, statErr := os.Stat(keepPath); statErr != nil {
		t.Fatalf("unknown file removed: %v", statErr)
	}
	if _, statErr := os.Stat(releaseDir); statErr != nil {
		t.Fatalf("release directory removed despite unknown file: %v", statErr)
	}
}

// Known-only directories still disappear: generated workspace files and
// plan-registered worktree directories are removed, then the empty root.
func TestExecuteReleaseCleanup_KnownOnlyTaskDirectoryRemoved(t *testing.T) {
	mgr, gitMock := cleanupTestManager(t, "released")
	gitMock.removeWorktreeFn = func(_, path string, _ bool) error {
		for i, entry := range gitMock.listWorktreesRes {
			if entry.Path == path {
				gitMock.listWorktreesRes = append(gitMock.listWorktreesRes[:i], gitMock.listWorktreesRes[i+1:]...)
				break
			}
		}
		return os.RemoveAll(path)
	}
	taskDir := filepath.Join(mgr.cfg.TasksRoot, "APP-1")
	if err := os.WriteFile(filepath.Join(taskDir, "APP-1.sln"), []byte("generated"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := mgr.PlanReleaseCleanup(t.Context(), "rel-1", DefaultReleaseCleanupSelection())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.ExecuteReleaseCleanup(t.Context(), plan, nil); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(taskDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("task directory still exists: %v", statErr)
	}
}

// A non-empty path recreated after git worktree removal must fail closed and
// remain: cleanup never recursively deletes a location it already validated.
func TestRemoveCleanupWorktree_RecreatedNonEmptyPathFailsClosed(t *testing.T) {
	const taskSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, tc := range []struct {
		name     string
		recreate func(t *testing.T, path string) string
	}{
		{"non-empty dir", func(t *testing.T, path string) string {
			inner := filepath.Join(path, "nested", "recreated.txt")
			if err := os.MkdirAll(filepath.Dir(inner), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(inner, []byte("recreated"), 0o600); err != nil {
				t.Fatal(err)
			}
			return inner
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr, gitMock := cleanupTestManager(t, "released")
			step := releaseCleanupStep{kind: cleanupTaskWorktree, repoPath: mgr.cfg.RootDir, path: filepath.Join(mgr.cfg.TasksRoot, "APP-1", "svc"), branch: "feature/APP-1", expectedSHA: taskSHA}
			gitMock.listWorktreesRes = gitMock.listWorktreesRes[:1]
			gitMock.removeWorktreeFn = func(_, path string, _ bool) error {
				if err := os.RemoveAll(path); err != nil {
					return err
				}
				tc.recreate(t, path)
				return nil
			}
			if _, err := mgr.removeCleanupWorktree(context.Background(), step); err == nil {
				t.Fatal("recreated non-empty path did not block cleanup")
			}
			if _, err := os.Stat(tc.recreate(t, step.path)); err != nil {
				t.Fatalf("recreated content removed: %v", err)
			}
		})
	}
}

// A single-file replacement after git worktree removal must fail closed and
// stay: cleanup never deletes a recreated path it did not validate.
func TestRemoveCleanupWorktree_RecreatedFileFailsClosedPreserved(t *testing.T) {
	mgr, gitMock := cleanupTestManager(t, "released")
	step := releaseCleanupStep{kind: cleanupTaskWorktree, repoPath: mgr.cfg.RootDir, path: filepath.Join(mgr.cfg.TasksRoot, "APP-1", "svc"), branch: "feature/APP-1", expectedSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	gitMock.listWorktreesRes = gitMock.listWorktreesRes[:1]
	gitMock.removeWorktreeFn = func(_, path string, _ bool) error {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		return os.WriteFile(path, []byte("recreated"), 0o600)
	}
	if _, err := mgr.removeCleanupWorktree(context.Background(), step); err == nil {
		t.Fatal("replacement file did not block cleanup")
	}
	if _, err := os.Lstat(step.path); err != nil {
		t.Fatalf("replacement file removed: %v", err)
	}
}

// A known entry replaced by a symlink fails closed: neither the link nor its
// target is touched, so the link dies never and the target survives.
func TestRemoveCleanupWorktree_RecreatedSymlinkFailsClosedPreservesTarget(t *testing.T) {
	mgr, gitMock := cleanupTestManager(t, "released")
	target := t.TempDir()
	keep := filepath.Join(target, "keep.txt")
	if err := os.WriteFile(keep, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	step := releaseCleanupStep{kind: cleanupTaskWorktree, repoPath: mgr.cfg.RootDir, path: filepath.Join(mgr.cfg.TasksRoot, "APP-1", "svc"), branch: "feature/APP-1", expectedSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	gitMock.listWorktreesRes = gitMock.listWorktreesRes[:1]
	gitMock.removeWorktreeFn = func(_, path string, _ bool) error {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		return os.Symlink(target, path)
	}
	if _, err := mgr.removeCleanupWorktree(context.Background(), step); err == nil {
		t.Fatal("symlink replacement did not block cleanup")
	}
	if _, err := os.Lstat(step.path); err != nil {
		t.Fatalf("symlink removed: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("symlink target removed: %v", err)
	}
}

// An entry recreated between the pruning scan and the empty-directory remove
// must fail closed: the recreated content and its parent survive.
func TestRemoveKnownEntriesOrPreserve_RecreatedKnownDirFailsClosed(t *testing.T) {
	root := t.TempDir()
	known := filepath.Join(root, "svc")
	if err := os.MkdirAll(known, 0o755); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	pruneScanHook = func(dir string) {
		if dir != root {
			return
		}
		once.Do(func() {
			if err := os.WriteFile(filepath.Join(known, "recreated.txt"), []byte("recreated"), 0o600); err != nil {
				t.Error(err)
			}
		})
	}
	t.Cleanup(func() { pruneScanHook = nil })

	if err := removeKnownEntriesOrPreserve(root, []string{known}); err == nil {
		t.Fatal("recreated known directory did not block pruning")
	}
	if _, err := os.Stat(filepath.Join(known, "recreated.txt")); err != nil {
		t.Fatalf("recreated content removed: %v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("root removed despite recreated content: %v", err)
	}
}

func TestRemoveKnownEntriesOrPreserve_KnownEntriesRemovedExactly(t *testing.T) {
	t.Run("file", func(t *testing.T) {
		root := t.TempDir()
		known := filepath.Join(root, "manifest.json")
		if err := os.WriteFile(known, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := removeKnownEntriesOrPreserve(root, []string{known}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("root still exists: %v", err)
		}
	})
	t.Run("symlink removes link only", func(t *testing.T) {
		root := t.TempDir()
		target := t.TempDir()
		keep := filepath.Join(target, "keep.txt")
		if err := os.WriteFile(keep, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "svc")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if err := removeKnownEntriesOrPreserve(root, []string{link}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(link); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("symlink still present: %v", err)
		}
		if _, err := os.Stat(keep); err != nil {
			t.Fatalf("symlink target removed: %v", err)
		}
	})
	t.Run("non-empty known dir fails closed", func(t *testing.T) {
		root := t.TempDir()
		known := filepath.Join(root, "svc")
		inner := filepath.Join(known, "leftover.txt")
		if err := os.MkdirAll(known, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(inner, []byte("leftover"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := removeKnownEntriesOrPreserve(root, []string{known}); err == nil {
			t.Fatal("non-empty known directory silently removed")
		}
		if _, err := os.Stat(inner); err != nil {
			t.Fatalf("known directory content removed: %v", err)
		}
	})
}

// A worktree path that reappears before parent directory cleanup is unknown
// user data: cleanup never validated it after recreation, so it is preserved
// with an error and the parent directory survives.
func TestExecuteReleaseCleanup_RecreatedWorktreePathPreservedByParentCleanup(t *testing.T) {
	mgr, gitMock := cleanupTestManager(t, "released")
	releaseWorktreePath := filepath.Join(mgr.releasesRootDir(), "rel-1", "services", "svc")
	worktreePath := filepath.Join(mgr.cfg.TasksRoot, "APP-1", "svc")
	gitMock.listWorktreesRes = gitMock.listWorktreesRes[1:] // task worktree absent at planning: no-op step
	gitMock.removeWorktreeFn = func(_, path string, _ bool) error {
		for i, entry := range gitMock.listWorktreesRes {
			if entry.Path == path {
				gitMock.listWorktreesRes = append(gitMock.listWorktreesRes[:i], gitMock.listWorktreesRes[i+1:]...)
				break
			}
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		if samePath(path, releaseWorktreePath) {
			return os.MkdirAll(worktreePath, 0o755)
		}
		return nil
	}
	if err := os.RemoveAll(worktreePath); err != nil {
		t.Fatal(err)
	}
	plan, err := mgr.PlanReleaseCleanup(t.Context(), "rel-1", DefaultReleaseCleanupSelection())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Preview().Blockers) != 0 {
		t.Fatalf("plan blocked: %q", plan.Preview().Blockers)
	}

	_, err = mgr.ExecuteReleaseCleanup(t.Context(), plan, nil)
	if err == nil || !strings.Contains(err.Error(), "present before parent cleanup") {
		t.Fatalf("error = %v, want preserved reappeared worktree path", err)
	}
	if _, statErr := os.Stat(worktreePath); statErr != nil {
		t.Fatalf("recreated worktree path removed: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(mgr.cfg.TasksRoot, "APP-1")); statErr != nil {
		t.Fatalf("task directory removed despite recreated worktree path: %v", statErr)
	}
}

// The same preservation applies to the release directory: a release worktree
// path reappearing before release directory cleanup is unknown and kept.
func TestExecuteReleaseCleanup_RecreatedReleaseWorktreePathPreservedByReleaseCleanup(t *testing.T) {
	mgr, gitMock := cleanupTestManager(t, "released")
	gitMock.listWorktreesRes = nil // worktrees already absent at planning: no-op steps
	releaseWorktreePath := filepath.Join(mgr.releasesRootDir(), "rel-1", "services", "svc")
	taskDir := filepath.Join(mgr.cfg.TasksRoot, "APP-1")
	for _, path := range []string{releaseWorktreePath, filepath.Join(taskDir, "svc")} {
		if err := os.RemoveAll(path); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := mgr.PlanReleaseCleanup(t.Context(), "rel-1", DefaultReleaseCleanupSelection())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Preview().Blockers) != 0 {
		t.Fatalf("plan blocked: %q", plan.Preview().Blockers)
	}
	var once sync.Once
	pruneScanHook = func(dir string) {
		if !samePath(dir, taskDir) {
			return
		}
		once.Do(func() {
			if err := os.MkdirAll(releaseWorktreePath, 0o755); err != nil {
				t.Error(err)
			}
		})
	}
	t.Cleanup(func() { pruneScanHook = nil })

	_, err = mgr.ExecuteReleaseCleanup(t.Context(), plan, nil)
	if err == nil || !strings.Contains(err.Error(), "present before parent cleanup") {
		t.Fatalf("error = %v, want preserved reappeared worktree path", err)
	}
	if _, statErr := os.Stat(releaseWorktreePath); statErr != nil {
		t.Fatalf("recreated release worktree path removed: %v", statErr)
	}
	if _, statErr := os.Stat(mgr.releaseManifestPath("rel-1")); statErr != nil {
		t.Fatalf("release manifest removed despite recreated worktree path: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Dir(mgr.releaseManifestPath("rel-1"))); statErr != nil {
		t.Fatalf("release directory removed despite recreated worktree path: %v", statErr)
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
