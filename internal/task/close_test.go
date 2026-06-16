package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/discovery"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/sln"
	"github.com/D1ssolve/wtui/internal/validation"
)

func TestPlanCloseTask_FeatureBranch_MergesToDevelopOnly(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskID := "IN-CLOSE-FEATURE"
	svcPath := filepath.Join(tasksRoot, taskID, "svc-a")
	if err := osMkdirAll(svcPath); err != nil {
		t.Fatal(err)
	}

	fakeCommonDir := filepath.Join(rootDir, "repos", "svc-a", ".git")
	if err := osMkdirAll(fakeCommonDir); err != nil {
		t.Fatal(err)
	}

	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			if path == svcPath {
				return fakeCommonDir, nil
			}
			return "", errors.New("not a worktree")
		},
		listWorktreesRes: []git.WorktreeEntry{{
			Path:   svcPath,
			Branch: "refs/heads/feature/IN-CLOSE-FEATURE",
		}},
		repoStatusFn: func(path string) (git.RawStatus, error) {
			return git.RawStatus{Branch: "feature/IN-CLOSE-FEATURE"}, nil
		},
	}

	cfg := newCloseTestConfig(rootDir, tasksRoot)
	flow, err := gitflow.EffectiveConfig(cfg.GitFlow)
	if err != nil {
		t.Fatalf("flow: %v", err)
	}
	mgr := newTestManagerWithDeps(t, cfg, gitMock, flow, nil)

	plan, err := mgr.PlanCloseTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("PlanCloseTask error: %v", err)
	}
	if len(plan.Services) != 1 {
		t.Fatalf("services len = %d, want 1", len(plan.Services))
	}
	if len(plan.Services[0].TargetBranches) != 1 || plan.Services[0].TargetBranches[0] != "develop" {
		t.Fatalf("targets = %v, want [develop]", plan.Services[0].TargetBranches)
	}
}

func TestPlanCloseTask_ReleaseBranch_HasMasterDevelopAndTag(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskID := "IN-CLOSE-RELEASE"
	svcPath := filepath.Join(tasksRoot, taskID, "svc-a")
	if err := osMkdirAll(svcPath); err != nil {
		t.Fatal(err)
	}

	fakeCommonDir := filepath.Join(rootDir, "repos", "svc-a", ".git")
	if err := osMkdirAll(fakeCommonDir); err != nil {
		t.Fatal(err)
	}

	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) { return fakeCommonDir, nil },
		listWorktreesRes: []git.WorktreeEntry{{
			Path:   svcPath,
			Branch: "refs/heads/release/1.2.0",
		}},
		repoStatusFn: func(path string) (git.RawStatus, error) { return git.RawStatus{Branch: "release/1.2.0"}, nil },
	}

	cfg := newCloseTestConfig(rootDir, tasksRoot)
	flow, _ := gitflow.EffectiveConfig(cfg.GitFlow)
	mgr := newTestManagerWithDeps(t, cfg, gitMock, flow, nil)

	plan, err := mgr.PlanCloseTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("PlanCloseTask error: %v", err)
	}
	svcPlan := plan.Services[0]
	if len(svcPlan.TargetBranches) != 2 || svcPlan.TargetBranches[0] != "master" || svcPlan.TargetBranches[1] != "develop" {
		t.Fatalf("targets = %v, want [master develop]", svcPlan.TargetBranches)
	}
	if svcPlan.TagPlan == nil {
		t.Fatal("TagPlan nil, want non-nil")
	}
}

func TestCloseTask_DirtyServiceFailsValidation_NoMerge(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskID := "IN-CLOSE-DIRTY"
	svcPath := filepath.Join(tasksRoot, taskID, "svc-a")
	if err := osMkdirAll(svcPath); err != nil {
		t.Fatal(err)
	}

	fakeCommonDir := filepath.Join(rootDir, "repos", "svc-a", ".git")
	if err := osMkdirAll(fakeCommonDir); err != nil {
		t.Fatal(err)
	}

	gitMock := &mockGitClient{
		commonDirFn:      func(path string) (string, error) { return fakeCommonDir, nil },
		listWorktreesRes: []git.WorktreeEntry{{Path: svcPath, Branch: "refs/heads/feature/IN-CLOSE-DIRTY"}},
		repoStatusFn: func(path string) (git.RawStatus, error) {
			return git.RawStatus{Branch: "feature/IN-CLOSE-DIRTY", ChangedEntries: []git.StatusEntry{{XY: "M.", Path: "a.txt"}}}, nil
		},
	}

	cfg := newCloseTestConfig(rootDir, tasksRoot)
	flow, _ := gitflow.EffectiveConfig(cfg.GitFlow)
	mgr := newTestManagerWithDeps(t, cfg, gitMock, flow, nil)

	_, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID})
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("CloseTask error = %v, want ErrValidationFailed", err)
	}

	gitMock.mu.Lock()
	mergeCalls := len(gitMock.mergeCalls)
	gitMock.mu.Unlock()
	if mergeCalls != 0 {
		t.Fatalf("merge calls = %d, want 0", mergeCalls)
	}
}

func TestCloseTask_AlreadyMerged_SkipsMerge(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskID := "IN-CLOSE-SKIP"
	svcPath := filepath.Join(tasksRoot, taskID, "svc-a")
	if err := osMkdirAll(svcPath); err != nil {
		t.Fatal(err)
	}

	fakeCommonDir := filepath.Join(rootDir, "repos", "svc-a", ".git")
	if err := osMkdirAll(fakeCommonDir); err != nil {
		t.Fatal(err)
	}

	gitMock := &mockGitClient{
		commonDirFn:      func(path string) (string, error) { return fakeCommonDir, nil },
		listWorktreesRes: []git.WorktreeEntry{{Path: svcPath, Branch: "refs/heads/feature/IN-CLOSE-SKIP"}},
		repoStatusFn:     func(path string) (git.RawStatus, error) { return git.RawStatus{Branch: "feature/IN-CLOSE-SKIP"}, nil },
		isAncestorFn:     func(repoPath, ancestor, descendant string) (bool, error) { return true, nil },
	}

	cfg := newCloseTestConfig(rootDir, tasksRoot)
	flow, _ := gitflow.EffectiveConfig(cfg.GitFlow)
	mgr := newTestManagerWithDeps(t, cfg, gitMock, flow, nil)

	res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID})
	if err != nil {
		t.Fatalf("CloseTask error: %v", err)
	}

	foundSkipped := false
	for _, st := range res.Steps {
		if strings.Contains(st.Name, "merge") && st.Status == StepStatusSkipped {
			foundSkipped = true
		}
	}
	if !foundSkipped {
		t.Fatalf("steps %+v do not contain skipped merge step", res.Steps)
	}
}

func TestCloseTask_DryRun_NoGitMutations(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskID := "IN-CLOSE-DRY"
	svcPath := filepath.Join(tasksRoot, taskID, "svc-a")
	if err := osMkdirAll(svcPath); err != nil {
		t.Fatal(err)
	}

	fakeCommonDir := filepath.Join(rootDir, "repos", "svc-a", ".git")
	if err := osMkdirAll(fakeCommonDir); err != nil {
		t.Fatal(err)
	}

	gitMock := &mockGitClient{
		commonDirFn:      func(path string) (string, error) { return fakeCommonDir, nil },
		listWorktreesRes: []git.WorktreeEntry{{Path: svcPath, Branch: "refs/heads/feature/IN-CLOSE-DRY"}},
		repoStatusFn:     func(path string) (git.RawStatus, error) { return git.RawStatus{Branch: "feature/IN-CLOSE-DRY"}, nil },
	}

	cfg := newCloseTestConfig(rootDir, tasksRoot)
	flow, _ := gitflow.EffectiveConfig(cfg.GitFlow)
	mgr := newTestManagerWithDeps(t, cfg, gitMock, flow, nil)

	res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID, DryRun: true})
	if err != nil {
		t.Fatalf("CloseTask error: %v", err)
	}
	if !res.Success {
		t.Fatal("result.Success = false, want true")
	}

	gitMock.mu.Lock()
	fetchCalls := len(gitMock.fetchCalls)
	mergeCalls := len(gitMock.mergeCalls)
	pushCalls := len(gitMock.pushCalls)
	gitMock.mu.Unlock()

	if fetchCalls != 0 || mergeCalls != 0 || pushCalls != 0 {
		t.Fatalf("mutating calls fetch=%d merge=%d push=%d, want 0/0/0", fetchCalls, mergeCalls, pushCalls)
	}
}

func TestCloseTask_RestoreBranchFailure_ReturnsErrorWhenContinueOnErrorDisabled(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskID := "IN-CLOSE-RESTORE-FAIL"
	svcPath := filepath.Join(tasksRoot, taskID, "svc-a")
	if err := osMkdirAll(svcPath); err != nil {
		t.Fatal(err)
	}

	fakeCommonDir := filepath.Join(rootDir, "repos", "svc-a", ".git")
	if err := osMkdirAll(fakeCommonDir); err != nil {
		t.Fatal(err)
	}

	gitMock := &mockGitClient{
		commonDirFn:      func(path string) (string, error) { return fakeCommonDir, nil },
		listWorktreesRes: []git.WorktreeEntry{{Path: svcPath, Branch: "refs/heads/feature/IN-CLOSE-RESTORE-FAIL"}},
		repoStatusFn:     func(path string) (git.RawStatus, error) { return git.RawStatus{Branch: "feature/IN-CLOSE-RESTORE-FAIL"}, nil },
		isAncestorFn:     func(repoPath, ancestor, descendant string) (bool, error) { return true, nil },
		worktreeBranchResult: "feature/IN-CLOSE-RESTORE-FAIL",
		checkoutFn: func(worktreePath, branch string) error {
			if branch == "feature/IN-CLOSE-RESTORE-FAIL" {
				return errors.New("restore failed")
			}
			return nil
		},
	}

	cfg := newCloseTestConfig(rootDir, tasksRoot)
	flow, _ := gitflow.EffectiveConfig(cfg.GitFlow)
	mgr := newTestManagerWithDeps(t, cfg, gitMock, flow, nil)

	res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID})
	if err == nil {
		t.Fatal("CloseTask error = nil, want non-nil")
	}
	if err.Error() != "close task failed" {
		t.Fatalf("CloseTask error = %q, want %q", err.Error(), "close task failed")
	}

	foundRestoreFailed := false
	for _, st := range res.Steps {
		if st.Name == "svc-a:restore-branch" && st.Status == StepStatusFailed {
			foundRestoreFailed = true
			break
		}
	}
	if !foundRestoreFailed {
		t.Fatalf("steps %+v do not contain failed restore step", res.Steps)
	}
}

func TestCloseTask_RestoreBranchFailure_ContinueOnErrorMarksResultFailed(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskID := "IN-CLOSE-RESTORE-CONTINUE"
	svcAPath := filepath.Join(tasksRoot, taskID, "svc-a")
	svcBPath := filepath.Join(tasksRoot, taskID, "svc-b")
	if err := osMkdirAll(svcAPath); err != nil {
		t.Fatal(err)
	}
	if err := osMkdirAll(svcBPath); err != nil {
		t.Fatal(err)
	}

	fakeCommonDirA := filepath.Join(rootDir, "repos", "svc-a", ".git")
	fakeCommonDirB := filepath.Join(rootDir, "repos", "svc-b", ".git")
	if err := osMkdirAll(fakeCommonDirA); err != nil {
		t.Fatal(err)
	}
	if err := osMkdirAll(fakeCommonDirB); err != nil {
		t.Fatal(err)
	}

	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			switch path {
			case svcAPath:
				return fakeCommonDirA, nil
			case svcBPath:
				return fakeCommonDirB, nil
			default:
				return "", errors.New("not a worktree")
			}
		},
		listWorktreesRes: []git.WorktreeEntry{
			{Path: svcAPath, Branch: "refs/heads/feature/IN-CLOSE-RESTORE-CONTINUE"},
			{Path: svcBPath, Branch: "refs/heads/feature/IN-CLOSE-RESTORE-CONTINUE"},
		},
		repoStatusFn: func(path string) (git.RawStatus, error) {
			return git.RawStatus{Branch: "feature/IN-CLOSE-RESTORE-CONTINUE"}, nil
		},
		isAncestorFn: func(repoPath, ancestor, descendant string) (bool, error) { return true, nil },
		getWorktreeBranchFn: func(path string) (string, error) {
			if path == svcAPath {
				return "feature/IN-CLOSE-RESTORE-CONTINUE", nil
			}
			if path == svcBPath {
				return "feature/IN-CLOSE-RESTORE-CONTINUE", nil
			}
			return "", nil
		},
		checkoutFn: func(worktreePath, branch string) error {
			if worktreePath == svcAPath && branch == "feature/IN-CLOSE-RESTORE-CONTINUE" {
				return errors.New("restore failed")
			}
			return nil
		},
	}

	cfg := newCloseTestConfig(rootDir, tasksRoot)
	cfg.Close.ContinueOnError = true
	flow, _ := gitflow.EffectiveConfig(cfg.GitFlow)
	mgr := newTestManagerWithDeps(t, cfg, gitMock, flow, nil)

	res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID})
	if err != nil {
		t.Fatalf("CloseTask error: %v", err)
	}
	if res.Success {
		t.Fatal("result.Success = true, want false")
	}

	foundSvcBFetch := false
	for _, st := range res.Steps {
		if st.Name == "svc-b:fetch" && st.Status == StepStatusOK {
			foundSvcBFetch = true
			break
		}
	}
	if !foundSvcBFetch {
		t.Fatalf("steps %+v do not show continuation to svc-b", res.Steps)
	}
}

func TestCloseTask_PushTagFailure_DeletesLocalTag(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskID := "IN-CLOSE-PUSH-TAG-FAIL"
	svcPath := filepath.Join(tasksRoot, taskID, "svc-a")
	if err := osMkdirAll(svcPath); err != nil {
		t.Fatal(err)
	}

	fakeCommonDir := filepath.Join(rootDir, "repos", "svc-a", ".git")
	if err := osMkdirAll(fakeCommonDir); err != nil {
		t.Fatal(err)
	}

	gitMock := &mockGitClient{
		commonDirFn:      func(path string) (string, error) { return fakeCommonDir, nil },
		listWorktreesRes: []git.WorktreeEntry{{Path: svcPath, Branch: "refs/heads/release/1.2.0"}},
		repoStatusFn:     func(path string) (git.RawStatus, error) { return git.RawStatus{Branch: "release/1.2.0"}, nil },
		isAncestorFn:     func(repoPath, ancestor, descendant string) (bool, error) { return true, nil },
		worktreeBranchResult: "release/1.2.0",
		pushTagErr:       errors.New("push tag failed"),
	}

	cfg := newCloseTestConfig(rootDir, tasksRoot)
	flow, _ := gitflow.EffectiveConfig(cfg.GitFlow)
	mgr := newTestManagerWithDeps(t, cfg, gitMock, flow, nil)

	_, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID})
	if err == nil {
		t.Fatal("CloseTask error = nil, want non-nil")
	}
	if err.Error() != "push tag failed" {
		t.Fatalf("CloseTask error = %q, want %q", err.Error(), "push tag failed")
	}

	gitMock.mu.Lock()
	createTagCalls := gitMock.createTagCalls
	pushTagCalls := gitMock.pushTagCalls
	deleteTagCalls := gitMock.deleteTagCalls
	gitMock.mu.Unlock()

	if createTagCalls != 1 {
		t.Fatalf("CreateTag calls = %d, want 1", createTagCalls)
	}
	if pushTagCalls != 1 {
		t.Fatalf("PushTag calls = %d, want 1", pushTagCalls)
	}
	if deleteTagCalls != 1 {
		t.Fatalf("DeleteTag calls = %d, want 1", deleteTagCalls)
	}
}

func TestCloseTask_DeleteSourceBranchAfterMerge_DeletesBranch(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskID := "IN-CLOSE-DEL"
	svcPath := filepath.Join(tasksRoot, taskID, "svc-a")
	if err := osMkdirAll(svcPath); err != nil {
		t.Fatal(err)
	}

	fakeCommonDir := filepath.Join(rootDir, "repos", "svc-a", ".git")
	if err := osMkdirAll(fakeCommonDir); err != nil {
		t.Fatal(err)
	}

	gitMock := &mockGitClient{
		commonDirFn:      func(path string) (string, error) { return fakeCommonDir, nil },
		listWorktreesRes: []git.WorktreeEntry{{Path: svcPath, Branch: "refs/heads/feature/IN-CLOSE-DEL"}},
		repoStatusFn:     func(path string) (git.RawStatus, error) { return git.RawStatus{Branch: "feature/IN-CLOSE-DEL"}, nil },
		isAncestorFn:     func(repoPath, ancestor, descendant string) (bool, error) { return false, nil },
	}

	cfg := newCloseTestConfig(rootDir, tasksRoot)
	cfg.GitFlow.BranchTypes["feature"] = config.BranchTypeRule{
		Prefixes:                     []string{"feature/"},
		BaseBranch:                   "develop",
		MergeTargets:                 []string{"develop"},
		ReviewTargets:                []string{"develop"},
		CloseStrategy:                "direct_merge",
		MergeStrategy:                "merge_commit",
		RequiresClean:                true,
		DeleteSourceBranchAfterMerge: true,
	}
	flow, _ := gitflow.EffectiveConfig(cfg.GitFlow)
	mgr := newTestManagerWithDeps(t, cfg, gitMock, flow, nil)

	_, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID})
	if err != nil {
		t.Fatalf("CloseTask error: %v", err)
	}

	gitMock.mu.Lock()
	deleteCalls := gitMock.deleteBranchCalls
	gitMock.mu.Unlock()
	if deleteCalls == 0 {
		t.Fatal("DeleteBranch call count = 0, want > 0")
	}
}

func TestPlanCloseTask_TagExists_AddsWarningAndSkipsTagPlan(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskID := "IN-CLOSE-TAG-EXISTS"
	svcPath := filepath.Join(tasksRoot, taskID, "svc-a")
	if err := osMkdirAll(svcPath); err != nil {
		t.Fatal(err)
	}

	fakeCommonDir := filepath.Join(rootDir, "repos", "svc-a", ".git")
	if err := osMkdirAll(fakeCommonDir); err != nil {
		t.Fatal(err)
	}

	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) { return fakeCommonDir, nil },
		listWorktreesRes: []git.WorktreeEntry{{
			Path:   svcPath,
			Branch: "refs/heads/release/1.2.0",
		}},
		repoStatusFn: func(path string) (git.RawStatus, error) { return git.RawStatus{Branch: "release/1.2.0"}, nil },
		tagExistsRes: true,
	}

	cfg := newCloseTestConfig(rootDir, tasksRoot)
	flow, _ := gitflow.EffectiveConfig(cfg.GitFlow)
	mgr := newTestManagerWithDeps(t, cfg, gitMock, flow, nil)

	plan, err := mgr.PlanCloseTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("PlanCloseTask error: %v", err)
	}

	if len(plan.Services) != 1 {
		t.Fatalf("services len=%d, want 1", len(plan.Services))
	}
	if plan.Services[0].TagPlan != nil {
		t.Fatal("TagPlan should be nil when tag exists")
	}
	if len(plan.Warnings) == 0 || !strings.Contains(plan.Warnings[0], "already exists") {
		t.Fatalf("warnings=%v, want tag-exists warning", plan.Warnings)
	}
}

func newCloseTestConfig(rootDir, tasksRoot string) *config.Config {
	cfg := &config.Config{RootDir: rootDir, TasksRoot: tasksRoot, BranchPrefix: "feature/", BaseBranch: "develop", Editor: "code"}
	if _, err := cfg.Effective(); err != nil {
		panic(err)
	}
	cfg.RootDir = rootDir
	cfg.TasksRoot = tasksRoot
	return cfg
}

func newTestManagerWithDeps(t *testing.T, cfg *config.Config, gitMock *mockGitClient, flow *gitflow.ResolvedGitFlow, forgeClients map[forge.ForgeProvider]forge.ForgeClient) Manager {
	t.Helper()
	logger := newTestLogger()
	disc := discovery.New(cfg, gitMock, logger)
	slnMgr := sln.NewManager(&mockDotnetClient{}, logger)
	validator := validation.NewTaskValidator(gitMock)
	return New(cfg, gitMock, disc, slnMgr, validator, flow, forgeClients, logger)
}

func osMkdirAll(path string) error {
	return os.MkdirAll(path, 0o755)
}
