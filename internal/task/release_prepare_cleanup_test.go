package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
)

// wireMockOwnedWorktreeCleanup models worktree registration for the owned
// cleanup helper: ListWorktrees additionally reports worktrees added through
// AddWorktree/AddDetachedWorktree (minus removed ones) as detached, and
// CommonDir falls back to the prior mock behavior. headFn supplies each
// added entry's HEAD because tests simulate HEAD identity differently.
func wireMockOwnedWorktreeCleanup(gitMock *mockGitClient, headFn func(m *mockGitClient, call addWorktreeCall) string) {
	prevListFn := gitMock.listWorktreesFn
	gitMock.listWorktreesFn = func(repoPath string) ([]git.WorktreeEntry, error) {
		var base []git.WorktreeEntry
		var err error
		if prevListFn != nil {
			base, err = prevListFn(repoPath)
		} else {
			base, err = gitMock.listWorktreesRes, gitMock.listWorktreesErr
		}
		if err != nil {
			return nil, err
		}
		gitMock.mu.Lock()
		defer gitMock.mu.Unlock()
		removed := make(map[string]bool, len(gitMock.removeWorktreeCalls))
		for _, call := range gitMock.removeWorktreeCalls {
			removed[call.WorktreePath] = true
		}
		seen := make(map[string]bool, len(base))
		for _, entry := range base {
			seen[filepath.Clean(entry.Path)] = true
		}
		entries := append([]git.WorktreeEntry(nil), base...)
		for _, call := range gitMock.addWorktreeCalls {
			cleaned := filepath.Clean(call.Dest)
			if call.RepoPath != repoPath || removed[cleaned] || seen[cleaned] {
				continue
			}
			seen[cleaned] = true
			entries = append(entries, git.WorktreeEntry{Path: call.Dest, Branch: "(detached)", HEAD: headFn(gitMock, call)})
		}
		return entries, nil
	}

	prevCommonFn := gitMock.commonDirFn
	gitMock.commonDirFn = func(path string) (string, error) {
		gitMock.mu.Lock()
		defer gitMock.mu.Unlock()
		for _, call := range gitMock.addWorktreeCalls {
			if call.Dest == path {
				return filepath.Join(call.RepoPath, ".git"), nil
			}
		}
		if prevCommonFn != nil {
			return prevCommonFn(path)
		}
		return gitMock.commonDirResult, gitMock.commonDirErr
	}
}

// mockHeadFromCreateRef resolves the entry HEAD from the ref the worktree was
// created from, matching failures that happen before HEAD is re-read.
func mockHeadFromCreateRef(m *mockGitClient, call addWorktreeCall) string {
	if m.resolveRefFn != nil {
		head, _ := m.resolveRefFn(call.RepoPath, call.Branch)
		return head
	}
	if m.resolveRefErr != nil {
		return ""
	}
	if m.resolveRefRes != "" {
		return m.resolveRefRes
	}
	return call.Branch + "-sha"
}

// mockHeadFromWorktreeHEAD resolves the entry HEAD the way
// resolveReleaseRefSHA(integrationPath, "HEAD") reports it after prepare.
func mockHeadFromWorktreeHEAD(m *mockGitClient, call addWorktreeCall) string {
	if m.resolveRefFn != nil {
		head, _ := m.resolveRefFn(call.Dest, "HEAD")
		return head
	}
	if m.resolveRefErr != nil {
		return ""
	}
	if m.resolveRefRes != "" {
		return m.resolveRefRes
	}
	return "HEAD-sha"
}

type ownedCleanupFakeGit struct {
	git.Client
	entries     []git.WorktreeEntry
	commonDir   string
	status      git.RawStatus
	removeErr   error
	removeCalls int
	removeForce bool
}

func (f *ownedCleanupFakeGit) ListWorktrees(_ context.Context, _ string) ([]git.WorktreeEntry, error) {
	return f.entries, nil
}

func (f *ownedCleanupFakeGit) CommonDir(_ context.Context, _ string) (string, error) {
	return f.commonDir, nil
}

func (f *ownedCleanupFakeGit) RepoStatus(_ context.Context, _ string) (git.RawStatus, error) {
	return f.status, nil
}

func (f *ownedCleanupFakeGit) RemoveWorktree(_ context.Context, _, _ string, force bool) error {
	f.removeCalls++
	f.removeForce = force
	return f.removeErr
}

func ownedCleanupFixture(t *testing.T) (*manager, *domain.Release, *domain.ReleaseService, *ownedCleanupFakeGit) {
	t.Helper()
	root := t.TempDir()
	releaseDir := filepath.Join(root, "releases", "REL-1")
	repoPath := filepath.Join(root, "svc-api")
	workPath := filepath.Join(releaseDir, ".work", "svc-api-integration")
	fake := &ownedCleanupFakeGit{
		entries:   []git.WorktreeEntry{{Path: workPath, Branch: "(detached)", HEAD: "abc123"}},
		commonDir: filepath.Join(repoPath, ".git"),
	}
	m := &manager{git: fake}
	release := &domain.Release{ID: "REL-1", Dir: releaseDir}
	svc := &domain.ReleaseService{Name: "svc-api", RepoPath: repoPath}
	return m, release, svc, fake
}

func TestRemoveOwnedIntegrationWorktree_TamperedPathOutsideWork_BlocksAndPreserves(t *testing.T) {
	m, release, svc, fake := ownedCleanupFixture(t)
	tampered := filepath.Join(t.TempDir(), "elsewhere", "svc-api-integration")

	err := m.removeOwnedIntegrationWorktree(context.Background(), release, svc, tampered, "")

	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("error = %v, want outside-work-dir block", err)
	}
	if fake.removeCalls != 0 {
		t.Fatalf("RemoveWorktree calls = %d, want 0", fake.removeCalls)
	}
}

func TestRemoveOwnedIntegrationWorktree_WrongBaseName_BlocksAndPreserves(t *testing.T) {
	m, release, svc, fake := ownedCleanupFixture(t)
	wrongName := filepath.Join(release.Dir, ".work", "other-integration")

	err := m.removeOwnedIntegrationWorktree(context.Background(), release, svc, wrongName, "")

	if err == nil || !strings.Contains(err.Error(), "does not name an owned worktree") {
		t.Fatalf("error = %v, want ownership name block", err)
	}
	if fake.removeCalls != 0 {
		t.Fatalf("RemoveWorktree calls = %d, want 0", fake.removeCalls)
	}
}

func TestRemoveOwnedIntegrationWorktree_UnregisteredPath_BlocksAndPreserves(t *testing.T) {
	m, release, svc, fake := ownedCleanupFixture(t)
	fake.entries = nil
	workPath := filepath.Join(release.Dir, ".work", "svc-api-integration")

	err := m.removeOwnedIntegrationWorktree(context.Background(), release, svc, workPath, "")

	if err == nil || !strings.Contains(err.Error(), "exactly one registered worktree") {
		t.Fatalf("error = %v, want registration block", err)
	}
	if fake.removeCalls != 0 {
		t.Fatalf("RemoveWorktree calls = %d, want 0", fake.removeCalls)
	}
}

func TestRemoveOwnedIntegrationWorktree_DuplicateRegistration_BlocksAndPreserves(t *testing.T) {
	m, release, svc, fake := ownedCleanupFixture(t)
	workPath := filepath.Join(release.Dir, ".work", "svc-api-integration")
	fake.entries = []git.WorktreeEntry{{Path: workPath, Branch: "(detached)"}, {Path: workPath, Branch: "(detached)"}}

	err := m.removeOwnedIntegrationWorktree(context.Background(), release, svc, workPath, "")

	if err == nil || !strings.Contains(err.Error(), "exactly one registered worktree") {
		t.Fatalf("error = %v, want duplicate registration block", err)
	}
	if fake.removeCalls != 0 {
		t.Fatalf("RemoveWorktree calls = %d, want 0", fake.removeCalls)
	}
}

func TestRemoveOwnedIntegrationWorktree_WrongCommonDir_BlocksAndPreserves(t *testing.T) {
	m, release, svc, fake := ownedCleanupFixture(t)
	fake.commonDir = filepath.Join(t.TempDir(), "other-repo", ".git")
	workPath := filepath.Join(release.Dir, ".work", "svc-api-integration")

	err := m.removeOwnedIntegrationWorktree(context.Background(), release, svc, workPath, "")

	if err == nil || !strings.Contains(err.Error(), "different repository") {
		t.Fatalf("error = %v, want common-dir block", err)
	}
	if fake.removeCalls != 0 {
		t.Fatalf("RemoveWorktree calls = %d, want 0", fake.removeCalls)
	}
}

func TestRemoveOwnedIntegrationWorktree_UnexpectedHEAD_BlocksAndPreserves(t *testing.T) {
	m, release, svc, fake := ownedCleanupFixture(t)
	workPath := filepath.Join(release.Dir, ".work", "svc-api-integration")

	err := m.removeOwnedIntegrationWorktree(context.Background(), release, svc, workPath, "deadbeef")

	if err == nil || !strings.Contains(err.Error(), "HEAD") {
		t.Fatalf("error = %v, want HEAD mismatch block", err)
	}
	if fake.removeCalls != 0 {
		t.Fatalf("RemoveWorktree calls = %d, want 0", fake.removeCalls)
	}
}

func TestRemoveOwnedIntegrationWorktree_Locked_BlocksAndPreserves(t *testing.T) {
	m, release, svc, fake := ownedCleanupFixture(t)
	workPath := filepath.Join(release.Dir, ".work", "svc-api-integration")
	fake.entries = []git.WorktreeEntry{{Path: workPath, Branch: "(detached)", HEAD: "abc123", Locked: true}}

	err := m.removeOwnedIntegrationWorktree(context.Background(), release, svc, workPath, "abc123")

	if err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("error = %v, want locked block", err)
	}
	if fake.removeCalls != 0 {
		t.Fatalf("RemoveWorktree calls = %d, want 0", fake.removeCalls)
	}
}

func TestRemoveOwnedIntegrationWorktree_NonDetached_BlocksAndPreserves(t *testing.T) {
	m, release, svc, fake := ownedCleanupFixture(t)
	workPath := filepath.Join(release.Dir, ".work", "svc-api-integration")
	fake.entries = []git.WorktreeEntry{{Path: workPath, Branch: "refs/heads/develop", HEAD: "abc123"}}

	err := m.removeOwnedIntegrationWorktree(context.Background(), release, svc, workPath, "abc123")

	if err == nil || !strings.Contains(err.Error(), "detached") {
		t.Fatalf("error = %v, want detached block", err)
	}
	if fake.removeCalls != 0 {
		t.Fatalf("RemoveWorktree calls = %d, want 0", fake.removeCalls)
	}
}

func TestRemoveOwnedIntegrationWorktree_Dirty_BlocksAndPreserves(t *testing.T) {
	m, release, svc, fake := ownedCleanupFixture(t)
	workPath := filepath.Join(release.Dir, ".work", "svc-api-integration")
	fake.status = git.RawStatus{UntrackedPaths: []string{"local.txt"}}

	err := m.removeOwnedIntegrationWorktree(context.Background(), release, svc, workPath, "abc123")

	if err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("error = %v, want dirty block", err)
	}
	if fake.removeCalls != 0 {
		t.Fatalf("RemoveWorktree calls = %d, want 0", fake.removeCalls)
	}
}

func TestRemoveOwnedIntegrationWorktree_ChangedEntries_BlocksAndPreserves(t *testing.T) {
	m, release, svc, fake := ownedCleanupFixture(t)
	workPath := filepath.Join(release.Dir, ".work", "svc-api-integration")
	fake.status = git.RawStatus{ChangedEntries: []git.StatusEntry{{Path: "README.md"}}}

	err := m.removeOwnedIntegrationWorktree(context.Background(), release, svc, workPath, "abc123")

	if err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("error = %v, want dirty block", err)
	}
	if fake.removeCalls != 0 {
		t.Fatalf("RemoveWorktree calls = %d, want 0", fake.removeCalls)
	}
}

func TestRemoveOwnedIntegrationWorktree_Success_RemovesNonForce(t *testing.T) {
	m, release, svc, fake := ownedCleanupFixture(t)
	workPath := filepath.Join(release.Dir, ".work", "svc-api-integration")

	if err := m.removeOwnedIntegrationWorktree(context.Background(), release, svc, workPath, "abc123"); err != nil {
		t.Fatalf("removeOwnedIntegrationWorktree() error: %v", err)
	}
	if fake.removeCalls != 1 {
		t.Fatalf("RemoveWorktree calls = %d, want 1", fake.removeCalls)
	}
	if fake.removeForce {
		t.Fatal("RemoveWorktree called with force, want non-force")
	}
}

func TestRemoveOwnedIntegrationWorktree_RemoveFailure_PreservesAndSurfaces(t *testing.T) {
	m, release, svc, fake := ownedCleanupFixture(t)
	fake.removeErr = errors.New("simulated removal failure")
	workPath := filepath.Join(release.Dir, ".work", "svc-api-integration")

	err := m.removeOwnedIntegrationWorktree(context.Background(), release, svc, workPath, "abc123")

	if err == nil || !strings.Contains(err.Error(), "simulated removal failure") {
		t.Fatalf("error = %v, want removal failure surfaced", err)
	}
	if _, statErr := os.Stat(workPath); !os.IsNotExist(statErr) {
		t.Fatalf("stat(%q) err = %v, want path untouched on disk", workPath, statErr)
	}
}
