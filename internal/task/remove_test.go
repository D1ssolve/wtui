package task

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/gitflow"
)

func removeTestFlow() *gitflow.ResolvedGitFlow {
	return &gitflow.ResolvedGitFlow{
		ProductionBranch:  "main",
		IntegrationBranch: "develop",
		DefaultBranchType: gitflow.BranchTypeFeature,
		BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
			gitflow.BranchTypeFeature: {Prefixes: []string{"feature/"}},
			gitflow.BranchTypeRelease: {Prefixes: []string{"release/"}},
			gitflow.BranchTypeHotfix:  {Prefixes: []string{"hotfix/"}},
		},
	}
}

func TestRemove_TaskOwnedHotfixBranch_AllowsDeletion(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	taskDir := filepath.Join(tasksRoot, "ITPR-728")
	svcDir := filepath.Join(taskDir, "databridge")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	fakeCommonDir := filepath.Join(rootDir, "databridge", ".git")
	if err := os.MkdirAll(fakeCommonDir, 0o755); err != nil {
		t.Fatalf("setup commonDir: %v", err)
	}

	var gotLocalBranch, gotLeaseSHA string
	localLeaseDeletes := 0
	gitMock := &mockGitClient{
		commonDirResult:      fakeCommonDir,
		worktreeBranchResult: "hotfix/ITPR-728",
		resolveRefFn: func(_, ref string) (string, error) {
			if ref != "refs/heads/hotfix/ITPR-728" {
				t.Errorf("ResolveRef ref = %q, want refs/heads/hotfix/ITPR-728", ref)
			}
			return strings.Repeat("a", 40), nil
		},
		deleteBranchFn: func(_, branch string) error {
			t.Errorf("DeleteBranch called for %q, want lease-based DeleteBranchIfUnchanged", branch)
			return nil
		},
		deleteBranchIfUnchangedFn: func(_, branch, expectedSHA string) error {
			localLeaseDeletes++
			gotLocalBranch, gotLeaseSHA = branch, expectedSHA
			return nil
		},
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)
	mgr.(*manager).flow = &gitflow.ResolvedGitFlow{
		ProductionBranch:  "main",
		IntegrationBranch: "develop",
		DefaultBranchType: gitflow.BranchTypeFeature,
		BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
			gitflow.BranchTypeHotfix: {
				Prefixes: []string{"hotfix/"},
			},
		},
	}

	opts := RemoveOptions{RemoveWorktrees: true, DeleteLocalBranches: true}
	if err := mgr.Remove(context.Background(), "ITPR-728", opts); err != nil {
		t.Fatalf("Remove returned unexpected error: %v", err)
	}

	gitMock.mu.Lock()
	worktreeCalls := len(gitMock.removeWorktreeCalls)
	branchCalls := gitMock.deleteBranchCalls
	gitMock.mu.Unlock()
	if worktreeCalls != 1 {
		t.Errorf("RemoveWorktree called %d times, want 1", worktreeCalls)
	}
	if branchCalls != 0 {
		t.Errorf("DeleteBranch called %d times, want 0 (lease-based deletion required)", branchCalls)
	}
	if localLeaseDeletes != 1 {
		t.Errorf("DeleteBranchIfUnchanged called %d times, want 1", localLeaseDeletes)
	}
	if gotLocalBranch != "hotfix/ITPR-728" {
		t.Errorf("deleted local branch = %q, want hotfix/ITPR-728", gotLocalBranch)
	}
	if gotLeaseSHA != strings.Repeat("a", 40) {
		t.Errorf("local expected SHA = %q, want captured resolve SHA", gotLeaseSHA)
	}
	if _, err := os.Stat(taskDir); !os.IsNotExist(err) {
		t.Errorf("task directory still exists after Remove: %v", err)
	}

	// Remote deletion for the same task-owned hotfix fails closed before any
	// mutation; the user retries without the remote option.
	err := mgr.Remove(context.Background(), "ITPR-728", RemoveOptions{RemoveWorktrees: true, DeleteRemoteBranches: true})
	if !errors.Is(err, ErrRemoteAtomicGuardUnsupported) {
		t.Errorf("Remove(remote) error = %v, want ErrRemoteAtomicGuardUnsupported", err)
	}
}

func TestRemove_DeleteLocalBranches_LeaseRejected_RetainsBranchAndTaskDir(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	taskDir := filepath.Join(tasksRoot, "ITPR-729")
	svcDir := filepath.Join(taskDir, "databridge")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	fakeCommonDir := filepath.Join(rootDir, "databridge", ".git")
	if err := os.MkdirAll(fakeCommonDir, 0o755); err != nil {
		t.Fatalf("setup commonDir: %v", err)
	}

	gitMock := &mockGitClient{
		commonDirResult:      fakeCommonDir,
		worktreeBranchResult: "feature/ITPR-729",
		resolveRefFn: func(_, _ string) (string, error) {
			return strings.Repeat("b", 40), nil
		},
		deleteBranchIfUnchangedFn: func(_, _, _ string) error {
			return errors.New("update-ref: branch moved")
		},
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)
	mgr.(*manager).flow = removeTestFlow()

	opts := RemoveOptions{RemoveWorktrees: true, DeleteLocalBranches: true}
	err := mgr.Remove(context.Background(), "ITPR-729", opts)
	if err == nil {
		t.Fatal("Remove returned nil, want lease rejection error")
	}
	if !strings.Contains(err.Error(), "delete branch") {
		t.Errorf("error = %v, want it to mention branch deletion", err)
	}

	gitMock.mu.Lock()
	branchCalls := gitMock.deleteBranchCalls
	gitMock.mu.Unlock()
	if branchCalls != 0 {
		t.Errorf("DeleteBranch called %d times after lease rejection, want 0", branchCalls)
	}
	if _, statErr := os.Stat(taskDir); statErr != nil {
		t.Errorf("task directory must be preserved after lease rejection: %v", statErr)
	}
}

func TestRemove_DeleteLocalBranches_ResolveError_BlocksMutation(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	taskDir := filepath.Join(tasksRoot, "ITPR-730")
	svcDir := filepath.Join(taskDir, "databridge")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	fakeCommonDir := filepath.Join(rootDir, "databridge", ".git")
	if err := os.MkdirAll(fakeCommonDir, 0o755); err != nil {
		t.Fatalf("setup commonDir: %v", err)
	}

	gitMock := &mockGitClient{
		commonDirResult:      fakeCommonDir,
		worktreeBranchResult: "feature/ITPR-730",
		resolveRefFn: func(_, _ string) (string, error) {
			return "", errors.New("rev-parse failed")
		},
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)
	mgr.(*manager).flow = removeTestFlow()

	opts := RemoveOptions{RemoveWorktrees: true, DeleteLocalBranches: true}
	err := mgr.Remove(context.Background(), "ITPR-730", opts)
	if err == nil {
		t.Fatal("Remove returned nil, want SHA resolution error")
	}

	gitMock.mu.Lock()
	worktreeCalls := len(gitMock.removeWorktreeCalls)
	gitMock.mu.Unlock()
	if worktreeCalls != 0 {
		t.Errorf("RemoveWorktree called %d times after resolve failure, want 0", worktreeCalls)
	}
	if _, statErr := os.Stat(taskDir); statErr != nil {
		t.Errorf("task directory must be preserved after resolve failure: %v", statErr)
	}
}

func TestRemove_DeleteLocalBranches_EmptySHABlocksMutation(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	taskDir := filepath.Join(tasksRoot, "ITPR-731")
	svcDir := filepath.Join(taskDir, "databridge")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	fakeCommonDir := filepath.Join(rootDir, "databridge", ".git")
	if err := os.MkdirAll(fakeCommonDir, 0o755); err != nil {
		t.Fatalf("setup commonDir: %v", err)
	}

	gitMock := &mockGitClient{
		commonDirResult:      fakeCommonDir,
		worktreeBranchResult: "feature/ITPR-731",
		resolveRefFn: func(_, _ string) (string, error) {
			return "", nil
		},
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)
	mgr.(*manager).flow = removeTestFlow()

	opts := RemoveOptions{RemoveWorktrees: true, DeleteLocalBranches: true}
	err := mgr.Remove(context.Background(), "ITPR-731", opts)
	if err == nil {
		t.Fatal("Remove returned nil, want error for empty branch SHA")
	}

	gitMock.mu.Lock()
	worktreeCalls := len(gitMock.removeWorktreeCalls)
	gitMock.mu.Unlock()
	if worktreeCalls != 0 {
		t.Errorf("RemoveWorktree called %d times with empty SHA, want 0", worktreeCalls)
	}
	if _, statErr := os.Stat(taskDir); statErr != nil {
		t.Errorf("task directory must be preserved with empty SHA: %v", statErr)
	}
}

func TestRemove_DeleteLocalBranches_SHACapturedBeforeWorktreeRemoval(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	taskDir := filepath.Join(tasksRoot, "ITPR-732")
	svcDir := filepath.Join(taskDir, "databridge")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	fakeCommonDir := filepath.Join(rootDir, "databridge", ".git")
	if err := os.MkdirAll(fakeCommonDir, 0o755); err != nil {
		t.Fatalf("setup commonDir: %v", err)
	}

	const capturedSHA = "cccccccccccccccccccccccccccccccccccccccc"
	var order []string
	gitMock := &mockGitClient{
		commonDirResult:      fakeCommonDir,
		worktreeBranchResult: "feature/ITPR-732",
		resolveRefFn: func(_, _ string) (string, error) {
			order = append(order, "resolve")
			return capturedSHA, nil
		},
		removeWorktreeFn: func(_, worktreePath string, _ bool) error {
			order = append(order, "worktree")
			return os.RemoveAll(worktreePath)
		},
		deleteBranchIfUnchangedFn: func(_, _, expectedSHA string) error {
			order = append(order, "delete")
			if expectedSHA != capturedSHA {
				t.Errorf("expectedSHA = %q, want SHA captured before worktree removal %q", expectedSHA, capturedSHA)
			}
			return nil
		},
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)
	mgr.(*manager).flow = removeTestFlow()

	opts := RemoveOptions{RemoveWorktrees: true, DeleteLocalBranches: true}
	if err := mgr.Remove(context.Background(), "ITPR-732", opts); err != nil {
		t.Fatalf("Remove returned unexpected error: %v", err)
	}

	if len(order) != 3 || order[0] != "resolve" || order[1] != "worktree" || order[2] != "delete" {
		t.Errorf("call order = %v, want [resolve worktree delete]", order)
	}
}

func TestIsRemoveProtectedBranch(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})
	m := mgr.(*manager)
	m.flow = &gitflow.ResolvedGitFlow{
		ProductionBranch:  "main",
		IntegrationBranch: "develop",
		DefaultBranchType: gitflow.BranchTypeFeature,
		BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
			gitflow.BranchTypeFeature: {Prefixes: []string{"feature/"}},
			gitflow.BranchTypeRelease: {Prefixes: []string{"release/"}},
			gitflow.BranchTypeHotfix:  {Prefixes: []string{"hotfix/"}},
		},
	}

	cases := []struct {
		branch        string
		taskID        string
		wantProtected bool
	}{
		{"main", "ITPR-728", true},
		{"develop", "ITPR-728", true},
		{"release/1.2.3", "ITPR-728", true},
		{"release/ITPR-728", "ITPR-728", true},
		{"hotfix/ITPR-728", "ITPR-728", false},
		{"feature/ITPR-728", "ITPR-728", false},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/%s", tc.branch, tc.taskID), func(t *testing.T) {
			got := m.isRemoveProtectedBranch(context.Background(), tc.branch, tc.taskID)
			if got != tc.wantProtected {
				t.Errorf("isRemoveProtectedBranch(%q, %q) = %v, want %v",
					tc.branch, tc.taskID, got, tc.wantProtected)
			}
		})
	}
}

func TestIsRemoveProtectedBranch_CustomHotfixPrefix_AllowsDeletion(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})
	m := mgr.(*manager)
	m.flow = &gitflow.ResolvedGitFlow{
		ProductionBranch:  "main",
		IntegrationBranch: "develop",
		DefaultBranchType: gitflow.BranchTypeFeature,
		BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
			gitflow.BranchTypeRelease: {Prefixes: []string{"release/"}},
			gitflow.BranchTypeHotfix:  {Prefixes: []string{"hf/", "hotfix/"}},
		},
	}

	if m.isRemoveProtectedBranch(context.Background(), "hf/ITPR-728", "ITPR-728") {
		t.Error("custom hotfix prefix hf/ITPR-728 should be removable")
	}
	if m.isRemoveProtectedBranch(context.Background(), "hotfix/ITPR-728", "ITPR-728") {
		t.Error("default hotfix prefix hotfix/ITPR-728 should be removable")
	}
	if !m.isRemoveProtectedBranch(context.Background(), "release/ITPR-728", "ITPR-728") {
		t.Error("release branch matching task ID should stay protected")
	}
}

func TestIsRemoveProtectedBranch_IdenticalReleaseHotfixPrefix_ReleaseStaysProtected(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})
	m := mgr.(*manager)
	m.flow = &gitflow.ResolvedGitFlow{
		ProductionBranch:  "main",
		IntegrationBranch: "develop",
		DefaultBranchType: gitflow.BranchTypeFeature,
		BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
			gitflow.BranchTypeRelease: {Prefixes: []string{"release/"}},
			gitflow.BranchTypeHotfix:  {Prefixes: []string{"release/"}},
		},
	}

	if !m.isRemoveProtectedBranch(context.Background(), "release/ITPR-728", "ITPR-728") {
		t.Error("release/ITPR-728 must stay protected when hotfix prefix is identical")
	}
	if !m.isRemoveProtectedBranch(context.Background(), "release/1.2.3", "ITPR-728") {
		t.Error("release/1.2.3 must stay protected when hotfix prefix overlaps")
	}
}

func TestIsRemoveProtectedBranch_OverlappingHotfixUnderReleaseNamespace_Protected(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})
	m := mgr.(*manager)
	m.flow = &gitflow.ResolvedGitFlow{
		ProductionBranch:  "main",
		IntegrationBranch: "develop",
		DefaultBranchType: gitflow.BranchTypeFeature,
		BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
			gitflow.BranchTypeRelease: {Prefixes: []string{"release/"}},
			gitflow.BranchTypeHotfix:  {Prefixes: []string{"release/hotfix/"}},
		},
	}

	if !m.isRemoveProtectedBranch(context.Background(), "release/hotfix/ITPR-728", "ITPR-728") {
		t.Error("release/hotfix/ITPR-728 must stay protected: release prefix matches before hotfix exception")
	}
	if !m.isRemoveProtectedBranch(context.Background(), "release/hotfix/OTHER-1", "ITPR-728") {
		t.Error("branch under release prefix of another task must stay protected")
	}
}

func TestIsRemoveProtectedBranch_OtherTaskBranches_Refused(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})
	m := mgr.(*manager)
	m.flow = removeTestFlow()

	for _, branch := range []string{"feature/OTHER-1", "hotfix/OTHER-1", "release/OTHER-1"} {
		if !m.isRemoveProtectedBranch(context.Background(), branch, "ITPR-728") {
			t.Errorf("isRemoveProtectedBranch(%q, ITPR-728) = false, want true (other-task branch)", branch)
		}
	}
}

func TestIsRemoveProtectedBranch_UnrecordedSuffixedNames_FailClosed(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})
	m := mgr.(*manager)
	m.flow = removeTestFlow()

	for _, branch := range []string{"feature/ITPR-728-extra", "hotfix/ITPR-728-rc1", "random-branch", "ITPR-728"} {
		if !m.isRemoveProtectedBranch(context.Background(), branch, "ITPR-728") {
			t.Errorf("isRemoveProtectedBranch(%q, ITPR-728) = false, want true (not an exact task-owned name)", branch)
		}
	}
}

func TestIsRemoveProtectedBranch_DetachedOrBlank_FailsClosed(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	mgr := newTestManager(t, tasksRoot, rootDir, &mockGitClient{})
	m := mgr.(*manager)
	m.flow = removeTestFlow()

	for _, branch := range []string{"", "   ", "HEAD", "(HEAD)"} {
		if !m.isRemoveProtectedBranch(context.Background(), branch, "ITPR-728") {
			t.Errorf("isRemoveProtectedBranch(%q, ITPR-728) = false, want true (detached/blank)", branch)
		}
	}
}

func TestRemove_OtherTaskFeatureBranch_Remains(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	taskDir := filepath.Join(tasksRoot, "ITPR-740")
	svcDir := filepath.Join(taskDir, "databridge")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	fakeCommonDir := filepath.Join(rootDir, "databridge", ".git")
	if err := os.MkdirAll(fakeCommonDir, 0o755); err != nil {
		t.Fatalf("setup commonDir: %v", err)
	}

	leaseDeletes := 0
	remoteDeletes := 0
	gitMock := &mockGitClient{
		commonDirResult:      fakeCommonDir,
		worktreeBranchResult: "feature/OTHER-9",
		deleteBranchIfUnchangedFn: func(_, _, _ string) error {
			leaseDeletes++
			return nil
		},
		deleteRemoteBranchIfUnchangedFn: func(_, _, _ string) error {
			remoteDeletes++
			return nil
		},
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)
	mgr.(*manager).flow = removeTestFlow()

	opts := RemoveOptions{RemoveWorktrees: true, DeleteLocalBranches: true}
	err := mgr.Remove(context.Background(), "ITPR-740", opts)
	if err == nil {
		t.Fatal("Remove returned nil, want refusal to delete other-task branch")
	}
	if !strings.Contains(err.Error(), "feature/OTHER-9") {
		t.Errorf("error = %v, want it to name the refused branch", err)
	}

	gitMock.mu.Lock()
	worktreeCalls := len(gitMock.removeWorktreeCalls)
	gitMock.mu.Unlock()
	if worktreeCalls != 0 {
		t.Errorf("RemoveWorktree called %d times, want 0", worktreeCalls)
	}
	if leaseDeletes != 0 {
		t.Errorf("DeleteBranchIfUnchanged called %d times, want 0", leaseDeletes)
	}
	if remoteDeletes != 0 {
		t.Errorf("DeleteRemoteBranchIfUnchanged called %d times, want 0", remoteDeletes)
	}
	if _, statErr := os.Stat(taskDir); statErr != nil {
		t.Errorf("task directory must be preserved: %v", statErr)
	}
}

func TestRemove_ArbitraryBranch_Remains(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	taskDir := filepath.Join(tasksRoot, "ITPR-741")
	svcDir := filepath.Join(taskDir, "databridge")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	fakeCommonDir := filepath.Join(rootDir, "databridge", ".git")
	if err := os.MkdirAll(fakeCommonDir, 0o755); err != nil {
		t.Fatalf("setup commonDir: %v", err)
	}

	gitMock := &mockGitClient{
		commonDirResult:      fakeCommonDir,
		worktreeBranchResult: "experiment/wip",
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)
	mgr.(*manager).flow = removeTestFlow()

	opts := RemoveOptions{RemoveWorktrees: true, DeleteLocalBranches: true}
	if err := mgr.Remove(context.Background(), "ITPR-741", opts); err == nil {
		t.Fatal("Remove returned nil, want refusal to delete arbitrary branch")
	}

	gitMock.mu.Lock()
	worktreeCalls := len(gitMock.removeWorktreeCalls)
	gitMock.mu.Unlock()
	if worktreeCalls != 0 {
		t.Errorf("RemoveWorktree called %d times, want 0", worktreeCalls)
	}
	if _, statErr := os.Stat(taskDir); statErr != nil {
		t.Errorf("task directory must be preserved: %v", statErr)
	}
}

func TestRemove_ForceDoesNotBypassUnauthorizedBranch(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")

	taskDir := filepath.Join(tasksRoot, "ITPR-742")
	svcDir := filepath.Join(taskDir, "databridge")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	fakeCommonDir := filepath.Join(rootDir, "databridge", ".git")
	if err := os.MkdirAll(fakeCommonDir, 0o755); err != nil {
		t.Fatalf("setup commonDir: %v", err)
	}

	gitMock := &mockGitClient{
		commonDirResult: fakeCommonDir,
		getWorktreeBranchFn: func(_ string) (string, error) {
			return "feature/OTHER-10", nil
		},
	}
	mgr := newTestManager(t, tasksRoot, rootDir, gitMock)
	mgr.(*manager).flow = removeTestFlow()

	opts := RemoveOptions{RemoveWorktrees: true, Force: true, DeleteLocalBranches: true}
	if err := mgr.Remove(context.Background(), "ITPR-742", opts); err == nil {
		t.Fatal("Remove returned nil, want Force to not bypass branch authorization")
	}

	gitMock.mu.Lock()
	worktreeCalls := len(gitMock.removeWorktreeCalls)
	gitMock.mu.Unlock()
	if worktreeCalls != 0 {
		t.Errorf("RemoveWorktree called %d times with Force, want 0", worktreeCalls)
	}
	if _, statErr := os.Stat(taskDir); statErr != nil {
		t.Errorf("task directory must be preserved: %v", statErr)
	}
}
