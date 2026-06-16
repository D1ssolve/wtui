package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func TestPromoteToRelease_HappyPath_TwoServices(t *testing.T) {
	fx := setupPromoteFixture(t, "ZA-553", []string{"api", "worker"}, true)

	res, err := fx.mgr.PromoteToRelease(context.Background(), PromoteToReleaseParams{
		TaskID: "ZA-553",
		Versions: map[string]string{
			"api":    "1.2.3",
			"worker": "2.0.0",
		},
	})
	if err != nil {
		t.Fatalf("PromoteToRelease error: %v", err)
	}

	if res.ID != "ZA-553-release" {
		t.Fatalf("result.ID = %q, want %q", res.ID, "ZA-553-release")
	}
	if res.ParentID != "ZA-553" {
		t.Fatalf("result.ParentID = %q, want %q", res.ParentID, "ZA-553")
	}
	if res.Phase != "release" {
		t.Fatalf("result.Phase = %q, want %q", res.Phase, "release")
	}

	fx.git.mu.Lock()
	createCalls := append([]createBranchFromBranchCall(nil), fx.git.createBranchFromBranchCalls...)
	pushCalls := append([]pushBranchExplicitCall(nil), fx.git.pushBranchExplicitCalls...)
	addCalls := append([]addWorktreeCall(nil), fx.git.addWorktreeCalls...)
	fx.git.mu.Unlock()

	if len(createCalls) != 2 {
		t.Fatalf("CreateBranchFromBranch calls = %d, want 2", len(createCalls))
	}
	if len(pushCalls) != 2 {
		t.Fatalf("PushBranchExplicit calls = %d, want 2", len(pushCalls))
	}
	if len(addCalls) != 2 {
		t.Fatalf("AddWorktree calls = %d, want 2", len(addCalls))
	}
}

func TestPromoteToRelease_CreateBranchAlreadyExists_Continues(t *testing.T) {
	f := setupPromoteFixture(t, "ZA-553", []string{"api", "worker"}, true)
	f.git.createBranchFromBranchErr = &git.ExecError{
		Argv:     []string{"git", "branch", "release/1.2.3", "develop"},
		ExitCode: 128,
		Stderr:   "fatal: a branch named 'release/1.2.3' already exists",
	}

	res, err := f.mgr.PromoteToRelease(context.Background(), PromoteToReleaseParams{
		TaskID: "ZA-553",
		Versions: map[string]string{
			"api":    "1.2.3",
			"worker": "1.2.3",
		},
	})
	if err != nil {
		t.Fatalf("PromoteToRelease error: %v", err)
	}
	if res.ID != "ZA-553-release" {
		t.Fatalf("result.ID = %q, want %q", res.ID, "ZA-553-release")
	}

	f.git.mu.Lock()
	pushCalls := append([]pushBranchExplicitCall(nil), f.git.pushBranchExplicitCalls...)
	addCalls := append([]addWorktreeCall(nil), f.git.addWorktreeCalls...)
	f.git.mu.Unlock()

	if len(pushCalls) != 2 {
		t.Fatalf("PushBranchExplicit calls = %d, want 2", len(pushCalls))
	}
	if len(addCalls) != 2 {
		t.Fatalf("AddWorktree calls = %d, want 2", len(addCalls))
	}
}

func TestPromoteToRelease_Preconditions_NoMutations(t *testing.T) {
	tests := []struct {
		name    string
		arrange func(t *testing.T, fx *promoteFixture) PromoteToReleaseParams
		assert  func(t *testing.T, err error)
	}{
		{
			name: "task id invalid",
			arrange: func(_ *testing.T, _ *promoteFixture) PromoteToReleaseParams {
				return PromoteToReleaseParams{TaskID: "", Versions: map[string]string{"api": "1.2.3", "worker": "1.2.3"}}
			},
			assert: func(t *testing.T, err error) {
				if !errors.Is(err, ErrTaskNotFound) {
					t.Fatalf("error = %v, want ErrTaskNotFound", err)
				}
			},
		},
		{
			name: "source task dir missing",
			arrange: func(_ *testing.T, _ *promoteFixture) PromoteToReleaseParams {
				return PromoteToReleaseParams{TaskID: "MISSING", Versions: map[string]string{"api": "1.2.3", "worker": "1.2.3"}}
			},
			assert: func(t *testing.T, err error) {
				if !errors.Is(err, ErrTaskNotFound) {
					t.Fatalf("error = %v, want ErrTaskNotFound", err)
				}
			},
		},
		{
			name: "source task not feature phase",
			arrange: func(_ *testing.T, fx *promoteFixture) PromoteToReleaseParams {
				fx.git.listWorktreesRes = []git.WorktreeEntry{{Path: fx.sourcePaths["api"], Branch: "refs/heads/release/1.2.3"}, {Path: fx.sourcePaths["worker"], Branch: "refs/heads/release/1.2.3"}}
				return PromoteToReleaseParams{TaskID: "ZA-553", Versions: map[string]string{"api": "1.2.3", "worker": "1.2.3"}}
			},
			assert: func(t *testing.T, err error) {
				if !errors.Is(err, ErrPromoteSourceNotFeature) {
					t.Fatalf("error = %v, want ErrPromoteSourceNotFeature", err)
				}
			},
		},
		{
			name: "source task has parent",
			arrange: func(t *testing.T, fx *promoteFixture) PromoteToReleaseParams {
				childID := "ZA-553-release"
				if err := os.MkdirAll(filepath.Join(fx.tasksRoot, childID, "api"), 0o755); err != nil {
					t.Fatalf("setup child task: %v", err)
				}
				if err := os.MkdirAll(filepath.Join(fx.tasksRoot, childID, "worker"), 0o755); err != nil {
					t.Fatalf("setup child task: %v", err)
				}
				fx.git.listWorktreesRes = []git.WorktreeEntry{{Path: filepath.Join(fx.tasksRoot, childID, "api"), Branch: "refs/heads/feature/ZA-553-release"}, {Path: filepath.Join(fx.tasksRoot, childID, "worker"), Branch: "refs/heads/feature/ZA-553-release"}}
				return PromoteToReleaseParams{TaskID: childID, Versions: map[string]string{"api": "1.2.3", "worker": "1.2.3"}}
			},
			assert: func(t *testing.T, err error) {
				if !errors.Is(err, ErrPromoteSourceNotFeature) {
					t.Fatalf("error = %v, want ErrPromoteSourceNotFeature", err)
				}
			},
		},
		{
			name: "release rule missing",
			arrange: func(t *testing.T, fx *promoteFixture) PromoteToReleaseParams {
				flow := &gitflow.ResolvedGitFlow{
					DefaultBranchType: gitflow.BranchTypeFeature,
					BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
						gitflow.BranchTypeFeature: {Prefixes: []string{"feature/"}, BaseBranch: "develop"},
					},
				}
				fx.mgr = newTestManagerWithDeps(t, fx.cfg, fx.git, flow, nil)
				return PromoteToReleaseParams{TaskID: "ZA-553", Versions: map[string]string{"api": "1.2.3", "worker": "1.2.3"}}
			},
			assert: func(t *testing.T, err error) {
				if !errors.Is(err, ErrPromoteNoReleaseRule) {
					t.Fatalf("error = %v, want ErrPromoteNoReleaseRule", err)
				}
			},
		},
		{
			name: "target task already exists",
			arrange: func(t *testing.T, fx *promoteFixture) PromoteToReleaseParams {
				if err := os.MkdirAll(filepath.Join(fx.tasksRoot, "ZA-553-release"), 0o755); err != nil {
					t.Fatalf("setup target task: %v", err)
				}
				return PromoteToReleaseParams{TaskID: "ZA-553", Versions: map[string]string{"api": "1.2.3", "worker": "1.2.3"}}
			},
			assert: func(t *testing.T, err error) {
				if !errors.Is(err, ErrPromoteTargetExists) {
					t.Fatalf("error = %v, want ErrPromoteTargetExists", err)
				}
			},
		},
		{
			name: "version missing for service",
			arrange: func(_ *testing.T, _ *promoteFixture) PromoteToReleaseParams {
				return PromoteToReleaseParams{TaskID: "ZA-553", Versions: map[string]string{"api": "1.2.3"}}
			},
			assert: func(t *testing.T, err error) {
				if !errors.Is(err, ErrPromoteVersionMissing) {
					t.Fatalf("error = %v, want ErrPromoteVersionMissing", err)
				}
			},
		},
		{
			name: "version invalid semver",
			arrange: func(_ *testing.T, _ *promoteFixture) PromoteToReleaseParams {
				return PromoteToReleaseParams{TaskID: "ZA-553", Versions: map[string]string{"api": "bad", "worker": "1.2.3"}}
			},
			assert: func(t *testing.T, err error) {
				if !errors.Is(err, ErrPromoteVersionInvalid) {
					t.Fatalf("error = %v, want ErrPromoteVersionInvalid", err)
				}
			},
		},
		{
			name: "branch checked out in another worktree",
			arrange: func(_ *testing.T, fx *promoteFixture) PromoteToReleaseParams {
				fx.git.listWorktreesRes = append(fx.git.listWorktreesRes,
					git.WorktreeEntry{Path: filepath.Join(fx.rootDir, "other", "api-release"), Branch: "refs/heads/release/1.2.3"},
				)
				return PromoteToReleaseParams{TaskID: "ZA-553", Versions: map[string]string{"api": "1.2.3", "worker": "2.0.0"}}
			},
			assert: func(t *testing.T, err error) {
				if !errors.Is(err, ErrPromoteBranchCheckedOut) {
					t.Fatalf("error = %v, want ErrPromoteBranchCheckedOut", err)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fx := setupPromoteFixture(t, "ZA-553", []string{"api", "worker"}, true)
			params := tc.arrange(t, fx)

			_, err := fx.mgr.PromoteToRelease(context.Background(), params)
			if err == nil {
				t.Fatal("PromoteToRelease error = nil, want non-nil")
			}
			tc.assert(t, err)
			assertNoPromoteMutations(t, fx.git)
		})
	}
}

func TestPromoteToRelease_DirtyService_BlockedBeforeMutation(t *testing.T) {
	fx := setupPromoteFixture(t, "ZA-553", []string{"api", "worker"}, true)
	fx.git.isDirtyFn = func(path string) (bool, error) {
		if path == fx.sourcePaths["worker"] {
			return true, nil
		}
		return false, nil
	}

	_, err := fx.mgr.PromoteToRelease(context.Background(), PromoteToReleaseParams{
		TaskID: "ZA-553",
		Versions: map[string]string{
			"api":    "1.2.3",
			"worker": "2.0.0",
		},
	})
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("error = %v, want ErrValidationFailed", err)
	}

	assertNoPromoteMutations(t, fx.git)
}

func TestPromoteToRelease_ExistingTargetDir_Blocked(t *testing.T) {
	fx := setupPromoteFixture(t, "ZA-553", []string{"api", "worker"}, true)
	if err := os.MkdirAll(filepath.Join(fx.tasksRoot, "ZA-553-release"), 0o755); err != nil {
		t.Fatalf("setup release task dir: %v", err)
	}

	_, err := fx.mgr.PromoteToRelease(context.Background(), PromoteToReleaseParams{
		TaskID: "ZA-553",
		Versions: map[string]string{
			"api":    "1.2.3",
			"worker": "2.0.0",
		},
	})
	if !errors.Is(err, ErrPromoteTargetExists) {
		t.Fatalf("error = %v, want ErrPromoteTargetExists", err)
	}

	assertNoPromoteMutations(t, fx.git)
}

func TestPromoteToRelease_PartialFailure_RollsBackCreatedWorktrees(t *testing.T) {
	fx := setupPromoteFixture(t, "ZA-553", []string{"api", "worker"}, true)

	fx.git.fetchFn = func(path string) error {
		if path == fx.repoPaths["worker"] {
			return errors.New("fetch failed for worker")
		}
		return nil
	}
	fx.git.commonDirFn = func(path string) (string, error) {
		if path == filepath.Join(fx.tasksRoot, "ZA-553-release", "api") {
			return filepath.Join(fx.rootDir, "repos", "api", ".git"), nil
		}
		if repoPath, ok := fx.repoPaths[path]; ok {
			return filepath.Join(repoPath, ".git"), nil
		}
		return "", errors.New("common dir missing")
	}

	_, err := fx.mgr.PromoteToRelease(context.Background(), PromoteToReleaseParams{
		TaskID: "ZA-553",
		Versions: map[string]string{
			"api":    "1.2.3",
			"worker": "2.0.0",
		},
	})
	if err == nil {
		t.Fatal("PromoteToRelease error = nil, want non-nil")
	}

	fx.git.mu.Lock()
	removeCalls := append([]removeWorktreeCall(nil), fx.git.removeWorktreeCalls...)
	fx.git.mu.Unlock()

	if len(removeCalls) != 1 {
		t.Fatalf("RemoveWorktree calls = %d, want 1", len(removeCalls))
	}
	if got := removeCalls[0].WorktreePath; got != filepath.Join(fx.tasksRoot, "ZA-553-release", "api") {
		t.Fatalf("RemoveWorktree path = %q, want %q", got, filepath.Join(fx.tasksRoot, "ZA-553-release", "api"))
	}

	if _, statErr := os.Stat(filepath.Join(fx.tasksRoot, "ZA-553-release")); !os.IsNotExist(statErr) {
		t.Fatalf("release task dir still exists after rollback")
	}
}

type promoteFixture struct {
	rootDir     string
	tasksRoot   string
	cfg         *config.Config
	git         *mockGitClient
	mgr         Manager
	sourcePaths map[string]string
	repoPaths   map[string]string
}

func setupPromoteFixture(t *testing.T, taskID string, services []string, branchMissing bool) *promoteFixture {
	t.Helper()

	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskDir := filepath.Join(tasksRoot, taskID)

	sourcePaths := make(map[string]string, len(services))
	repoPaths := make(map[string]string, len(services))
	worktrees := make([]git.WorktreeEntry, 0, len(services))

	for _, svc := range services {
		sourcePath := filepath.Join(taskDir, svc)
		if err := os.MkdirAll(sourcePath, 0o755); err != nil {
			t.Fatalf("setup service dir %s: %v", svc, err)
		}
		sourcePaths[svc] = sourcePath

		repoPath := filepath.Join(rootDir, "repos", svc)
		if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
			t.Fatalf("setup repo dir %s: %v", svc, err)
		}
		repoPaths[svc] = repoPath
		repoPaths[sourcePath] = repoPath

		worktrees = append(worktrees, git.WorktreeEntry{Path: sourcePath, Branch: "refs/heads/feature/" + taskID})
	}

	gitMock := &mockGitClient{
		listWorktreesRes: worktrees,
		commonDirFn: func(path string) (string, error) {
			repoPath, ok := repoPaths[path]
			if !ok {
				return "", errors.New("not a git worktree")
			}
			return filepath.Join(repoPath, ".git"), nil
		},
		isDirtyFn: func(_ string) (bool, error) {
			return false, nil
		},
		branchExistsFn: func(_ string, _ string) (bool, error) {
			return !branchMissing, nil
		},
	}

	cfg := newCloseTestConfig(rootDir, tasksRoot)
	flow := &gitflow.ResolvedGitFlow{
		DefaultBranchType: gitflow.BranchTypeFeature,
		BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
			gitflow.BranchTypeFeature: {Prefixes: []string{"feature/"}, BaseBranch: "develop"},
			gitflow.BranchTypeRelease: {Prefixes: []string{"release/"}, BaseBranch: "develop"},
		},
	}

	mgr := newTestManagerWithDeps(t, cfg, gitMock, flow, nil)

	return &promoteFixture{
		rootDir:     rootDir,
		tasksRoot:   tasksRoot,
		cfg:         cfg,
		git:         gitMock,
		mgr:         mgr,
		sourcePaths: sourcePaths,
		repoPaths:   repoPaths,
	}
}

func assertNoPromoteMutations(t *testing.T, gitMock *mockGitClient) {
	t.Helper()

	gitMock.mu.Lock()
	defer gitMock.mu.Unlock()

	if len(gitMock.fetchCalls) != 0 {
		t.Fatalf("Fetch calls = %d, want 0", len(gitMock.fetchCalls))
	}
	if len(gitMock.createBranchFromBranchCalls) != 0 {
		t.Fatalf("CreateBranchFromBranch calls = %d, want 0", len(gitMock.createBranchFromBranchCalls))
	}
	if len(gitMock.pushBranchExplicitCalls) != 0 {
		t.Fatalf("PushBranchExplicit calls = %d, want 0", len(gitMock.pushBranchExplicitCalls))
	}
	if len(gitMock.addWorktreeCalls) != 0 {
		t.Fatalf("AddWorktree calls = %d, want 0", len(gitMock.addWorktreeCalls))
	}
}
