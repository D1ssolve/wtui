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
	"github.com/D1ssolve/wtui/internal/domain"
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
		repoStatusFn: func(path string) (git.RawStatus, error) {
			return git.RawStatus{Branch: "feature/IN-CLOSE-RESTORE-FAIL"}, nil
		},
		isAncestorFn:         func(repoPath, ancestor, descendant string) (bool, error) { return true, nil },
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

// TestCloseTask_ContinueOnError_RecordedFailureForcesUnsuccessful pins the
// result contract that gates destructive task-cleanup inspection: every step
// recorded as failed under continue_on_error must leave Success=false, while
// later services still progress.
func TestCloseTask_ContinueOnError_RecordedFailureForcesUnsuccessful(t *testing.T) {
	tagRule := func(cfg *config.Config) {
		featureRule := cfg.GitFlow.BranchTypes["feature"]
		featureRule.TagOnClose = true
		featureRule.TagSource = "develop"
		cfg.GitFlow.BranchTypes["feature"] = featureRule
	}
	pipelineRule := func(cfg *config.Config) {
		featureRule := cfg.GitFlow.BranchTypes["feature"]
		featureRule.TriggerPipelineOnClose = true
		cfg.GitFlow.BranchTypes["feature"] = featureRule
	}

	cases := []struct {
		name           string
		mutate         func(taskID string, gitMock *mockGitClient)
		mutateCfg      func(cfg *config.Config)
		wantFailedStep string
	}{
		{
			name: "resolve branch",
			mutate: func(taskID string, gitMock *mockGitClient) {
				gitMock.getWorktreeBranchFn = func(path string) (string, error) {
					if strings.Contains(path, "svc-a") {
						return "", errors.New("resolve branch failed")
					}
					return "feature/" + taskID, nil
				}
			},
			wantFailedStep: "svc-a:resolve-branch",
		},
		{
			name: "fetch",
			mutate: func(_ string, gitMock *mockGitClient) {
				gitMock.fetchFn = func(path string) error {
					if strings.Contains(path, "svc-a") {
						return errors.New("fetch failed")
					}
					return nil
				}
			},
			wantFailedStep: "svc-a:fetch",
		},
		{
			name: "merge",
			mutate: func(_ string, gitMock *mockGitClient) {
				gitMock.mergeFn = func(path, _ string) error {
					if strings.Contains(path, "svc-a") {
						return errors.New("merge failed")
					}
					return nil
				}
			},
			wantFailedStep: "svc-a:merge:develop",
		},
		{
			name: "push",
			mutate: func(_ string, gitMock *mockGitClient) {
				gitMock.pushRefWithLeaseFn = func(repoPath, _, _, _, _, _ string) error {
					if strings.Contains(repoPath, "svc-a") {
						return errors.New("push failed")
					}
					return nil
				}
			},
			wantFailedStep: "svc-a:push:develop",
		},
		{
			name:      "tag",
			mutateCfg: tagRule,
			mutate: func(_ string, gitMock *mockGitClient) {
				createTagCalls := 0
				gitMock.createTagFn = func(repoPath, tag, target, message string) error {
					createTagCalls++
					if createTagCalls == 1 {
						return errors.New("create tag failed")
					}
					return nil
				}
			},
			wantFailedStep: "svc-a:tag",
		},
		{
			name:      "push tag",
			mutateCfg: tagRule,
			mutate: func(_ string, gitMock *mockGitClient) {
				gitMock.resolveRefFn = func(_ string, ref string) (string, error) {
					if ref == "refs/tags/v0.1.0" {
						return "tag-oid-sha", nil
					}
					if ref == "tag-oid-sha^{commit}" {
						return "develop-sha", nil
					}
					return ref + "-sha", nil
				}
				pushTagCalls := 0
				gitMock.pushTagFn = func(repoPath, _, tag, tagObjectOID string) error {
					pushTagCalls++
					if pushTagCalls == 1 {
						return errors.New("push tag failed")
					}
					return nil
				}
			},
			wantFailedStep: "svc-a:push-tag",
		},
		{
			name:      "pipeline",
			mutateCfg: pipelineRule,
			mutate: func(_ string, gitMock *mockGitClient) {
				gitMock.remoteURLRes = "git@gitlab.com:group/svc-a.git"
			},
			wantFailedStep: "svc-a:pipeline",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rootDir := t.TempDir()
			tasksRoot := filepath.Join(rootDir, ".tasks")
			taskID := "IN-CLOSE-COE-" + strings.ToUpper(strings.ReplaceAll(tc.name, " ", "-"))
			svcAPath := filepath.Join(tasksRoot, taskID, "svc-a")
			svcBPath := filepath.Join(tasksRoot, taskID, "svc-b")
			for _, dir := range []string{svcAPath, svcBPath} {
				if err := osMkdirAll(dir); err != nil {
					t.Fatal(err)
				}
			}
			for _, dir := range []string{
				filepath.Join(rootDir, "repos", "svc-a", ".git"),
				filepath.Join(rootDir, "repos", "svc-b", ".git"),
			} {
				if err := osMkdirAll(dir); err != nil {
					t.Fatal(err)
				}
			}

			gitMock := &mockGitClient{
				commonDirFn: func(path string) (string, error) {
					switch path {
					case svcAPath:
						return filepath.Join(rootDir, "repos", "svc-a", ".git"), nil
					case svcBPath:
						return filepath.Join(rootDir, "repos", "svc-b", ".git"), nil
					default:
						return "", errors.New("not a worktree")
					}
				},
				listWorktreesRes: []git.WorktreeEntry{
					{Path: svcAPath, Branch: "refs/heads/feature/" + taskID},
					{Path: svcBPath, Branch: "refs/heads/feature/" + taskID},
				},
				repoStatusFn: func(string) (git.RawStatus, error) {
					return git.RawStatus{Branch: "feature/" + taskID}, nil
				},
				isAncestorFn: func(_, ancestor, descendant string) (bool, error) {
					// The merged local target builds on the fresh remote target.
					return ancestor == "origin/develop-sha" && descendant == "refs/heads/develop-sha", nil
				},
				getWorktreeBranchFn: func(string) (string, error) {
					return "feature/" + taskID, nil
				},
				remoteURLRes: "git@gitlab.com:group/svc-a.git",
			}
			tc.mutate(taskID, gitMock)

			cfg := newCloseTestConfig(rootDir, tasksRoot)
			cfg.Close.ContinueOnError = true
			if tc.mutateCfg != nil {
				tc.mutateCfg(cfg)
			}
			flow, _ := gitflow.EffectiveConfig(cfg.GitFlow)

			forgeClients := map[forge.ForgeProvider]forge.ForgeClient{
				forge.ForgeProviderGitLab: &mockForgeClient{
					triggerPipelineFn: func(_ context.Context, params forge.TriggerPipelineParams) error {
						if strings.Contains(params.WorktreePath, "svc-a") {
							return errors.New("trigger pipeline failed")
						}
						return nil
					},
				},
			}
			mgr := newTestManagerWithDeps(t, cfg, gitMock, flow, forgeClients)

			res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID})
			if err != nil {
				t.Fatalf("CloseTask error: %v", err)
			}
			if res.Success {
				t.Fatalf("result.Success = true after %s failure; cleanup inspection could trigger", tc.wantFailedStep)
			}

			foundFailed := false
			foundSvcBProgress := false
			for _, st := range res.Steps {
				if st.Name == tc.wantFailedStep && st.Status == StepStatusFailed {
					foundFailed = true
				}
				if st.Name == "svc-b:push:develop" && st.Status == StepStatusOK {
					foundSvcBProgress = true
				}
			}
			if !foundFailed {
				t.Fatalf("steps %+v do not contain failed step %q", res.Steps, tc.wantFailedStep)
			}
			if !foundSvcBProgress {
				t.Fatalf("steps %+v do not show svc-b progressing after continued failure", res.Steps)
			}
		})
	}
}

func TestCloseTask_PushTagFailure_RollsBackLocalTagWithLease(t *testing.T) {
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
		commonDirFn:          func(path string) (string, error) { return fakeCommonDir, nil },
		listWorktreesRes:     []git.WorktreeEntry{{Path: svcPath, Branch: "refs/heads/release/1.2.0"}},
		repoStatusFn:         func(path string) (git.RawStatus, error) { return git.RawStatus{Branch: "release/1.2.0"}, nil },
		isAncestorFn:         func(repoPath, ancestor, descendant string) (bool, error) { return true, nil },
		worktreeBranchResult: "release/1.2.0",
		remoteURLRes:         "git@gitlab.com:group/svc-a.git",
		resolveRefFn:         func(_ string, _ string) (string, error) { return "sha-source", nil },
		pushTagErr:           errors.New("push tag failed"),
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
	leaseCalls := append([]deleteTagIfUnchangedCall(nil), gitMock.deleteTagIfUnchangedCallList...)
	gitMock.mu.Unlock()

	if createTagCalls != 1 {
		t.Fatalf("CreateTag calls = %d, want 1", createTagCalls)
	}
	if pushTagCalls != 1 {
		t.Fatalf("PushTag calls = %d, want 1", pushTagCalls)
	}
	if len(leaseCalls) != 1 {
		t.Fatalf("DeleteTagIfUnchanged calls = %d, want 1", len(leaseCalls))
	}
	if leaseCalls[0].ExpectedOID != "sha-source" {
		t.Fatalf("rollback expected OID = %q, want captured tag object sha-source", leaseCalls[0].ExpectedOID)
	}
}

func TestCloseTask_DirectMergeClose_DoesNotDeleteBranch(t *testing.T) {
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
		isAncestorFn: func(_, ancestor, descendant string) (bool, error) {
			return ancestor == "origin/develop-sha" && descendant == "refs/heads/develop-sha", nil
		},
		worktreeBranchResult: "feature/IN-CLOSE-DEL",
		remoteURLRes:         "git@gitlab.com:group/svc-a.git",
	}

	cfg := newCloseTestConfig(rootDir, tasksRoot)
	cfg.GitFlow.BranchTypes["feature"] = config.BranchTypeRule{
		Prefixes:      []string{"feature/"},
		BaseBranch:    "develop",
		MergeTargets:  []string{"develop"},
		ReviewTargets: []string{"develop"},
		CloseStrategy: "direct_merge",
		MergeStrategy: "merge_commit",
		RequiresClean: true,
	}
	flow, _ := gitflow.EffectiveConfig(cfg.GitFlow)
	mgr := newTestManagerWithDeps(t, cfg, gitMock, flow, nil)

	result, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID})
	if err != nil {
		t.Fatalf("CloseTask error: %v", err)
	}
	if !result.Success {
		t.Fatal("CloseTask result.Success = false, want true")
	}

	gitMock.mu.Lock()
	deleteCalls := gitMock.deleteBranchCalls
	gitMock.mu.Unlock()
	if deleteCalls != 0 {
		t.Fatalf("DeleteBranch call count = %d, want 0: close must not delete branches inline", deleteCalls)
	}
}

func TestCloseTask_ReviewRequest_NeverRequestsSourceRemoval(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskID := "IN-CLOSE-REVIEW"
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
		listWorktreesRes: []git.WorktreeEntry{{Path: svcPath, Branch: "refs/heads/feature/IN-CLOSE-REVIEW"}},
		repoStatusFn:     func(path string) (git.RawStatus, error) { return git.RawStatus{Branch: "feature/IN-CLOSE-REVIEW"}, nil },
		remoteURLRes:     "git@gitlab.com:group/svc-a.git",
	}

	cfg := newCloseTestConfig(rootDir, tasksRoot)
	cfg.Close.PushSourceBeforeReview = false
	cfg.GitFlow.BranchTypes["feature"] = config.BranchTypeRule{
		Prefixes:      []string{"feature/"},
		BaseBranch:    "develop",
		MergeTargets:  []string{"develop"},
		ReviewTargets: []string{"develop"},
		CloseStrategy: "review_request",
		MergeStrategy: "merge_commit",
		RequiresClean: true,
	}
	flow, _ := gitflow.EffectiveConfig(cfg.GitFlow)

	var createParams []forge.CreateMRParams
	forgeClient := &mockForgeClient{createMRFn: func(_ context.Context, params forge.CreateMRParams) (forge.MRInfo, error) {
		createParams = append(createParams, params)
		return forge.MRInfo{Number: 1, URL: "https://gitlab.example/group/svc-a/1"}, nil
	}}
	mgr := newTestManagerWithDeps(t, cfg, gitMock, flow, map[forge.ForgeProvider]forge.ForgeClient{
		forge.ForgeProviderGitLab: forgeClient,
	})

	result, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID})
	if err != nil {
		t.Fatalf("CloseTask error: %v", err)
	}
	if !result.Success {
		t.Fatal("CloseTask result.Success = false, want true")
	}
	if len(createParams) != 1 {
		t.Fatalf("CreateMR calls = %d, want 1", len(createParams))
	}
	if createParams[0].RemoveSource {
		t.Fatal("CreateMR RemoveSource = true, want false: close must not ask forge to auto-delete source branch")
	}

	gitMock.mu.Lock()
	deleteCalls := gitMock.deleteBranchCalls
	gitMock.mu.Unlock()
	if deleteCalls != 0 {
		t.Fatalf("DeleteBranch call count = %d, want 0", deleteCalls)
	}
}

func TestCloseTask_MergeError_AbortsMergeAndRestoresBranch(t *testing.T) {
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskID := "IN-CLOSE-MERGE-ABORT"
	svcPath := filepath.Join(tasksRoot, taskID, "svc-a")
	if err := osMkdirAll(svcPath); err != nil {
		t.Fatal(err)
	}

	fakeCommonDir := filepath.Join(rootDir, "repos", "svc-a", ".git")
	if err := osMkdirAll(fakeCommonDir); err != nil {
		t.Fatal(err)
	}

	mergeErr := errors.New("merge conflict")
	operationStateCalls := 0
	gitMock := &mockGitClient{
		commonDirFn:      func(path string) (string, error) { return fakeCommonDir, nil },
		listWorktreesRes: []git.WorktreeEntry{{Path: svcPath, Branch: "refs/heads/feature/IN-CLOSE-MERGE-ABORT"}},
		repoStatusFn: func(path string) (git.RawStatus, error) {
			return git.RawStatus{Branch: "feature/IN-CLOSE-MERGE-ABORT"}, nil
		},
		isAncestorFn:         func(repoPath, ancestor, descendant string) (bool, error) { return false, nil },
		worktreeBranchResult: "feature/IN-CLOSE-MERGE-ABORT",
		mergeFn: func(path, branch string) error {
			if branch == "feature/IN-CLOSE-MERGE-ABORT" {
				return mergeErr
			}
			return nil
		},
		operationStateFn: func(path string) ([]domain.RepoState, error) {
			operationStateCalls++
			if operationStateCalls == 1 {
				return nil, nil
			}
			return []domain.RepoState{domain.RepoStateMerging, domain.RepoStateConflicted}, nil
		},
	}

	cfg := newCloseTestConfig(rootDir, tasksRoot)
	flow, _ := gitflow.EffectiveConfig(cfg.GitFlow)
	mgr := newTestManagerWithDeps(t, cfg, gitMock, flow, nil)

	_, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: taskID})
	if err == nil {
		t.Fatal("CloseTask error = nil, want merge error")
	}
	if !errors.Is(err, mergeErr) {
		t.Fatalf("CloseTask error = %v, want merge error wrapped", err)
	}

	gitMock.mu.Lock()
	abortCalls := append([]string(nil), gitMock.mergeAbortCalls...)
	checkoutCalls := append([]checkoutCall(nil), gitMock.checkoutCalls...)
	gitMock.mu.Unlock()

	if len(abortCalls) != 1 || abortCalls[0] != svcPath {
		t.Fatalf("MergeAbort calls = %+v, want one call for %s", abortCalls, svcPath)
	}

	foundRestore := false
	for _, call := range checkoutCalls {
		if call.WorktreePath == svcPath && call.Branch == "feature/IN-CLOSE-MERGE-ABORT" {
			foundRestore = true
			break
		}
	}
	if !foundRestore {
		t.Fatalf("checkout calls = %+v, want restore checkout", checkoutCalls)
	}
}

func TestPlanCloseTask_TagExists_KeepsTagPlanWithVerificationPendingWarning(t *testing.T) {
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
	tagPlan := plan.Services[0].TagPlan
	if tagPlan == nil {
		t.Fatal("TagPlan removed when tag exists; existing required tags must stay planned")
	}
	if !plan.RequiresTag {
		t.Fatal("RequiresTag = false, want true: existing tag still needs verification proof")
	}
	var warned bool
	for _, w := range plan.Warnings {
		if strings.Contains(w, "already exists") && strings.Contains(w, "verification pending") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("warnings=%v, want tag-exists verification-pending warning", plan.Warnings)
	}
}

func newTagCloseFixture(t *testing.T, taskID string, mutate func(*mockGitClient, *config.Config)) (Manager, *mockGitClient) {
	t.Helper()
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	svcPath := filepath.Join(tasksRoot, taskID, "svc-a")
	if err := osMkdirAll(svcPath); err != nil {
		t.Fatal(err)
	}
	fakeCommonDir := filepath.Join(rootDir, "repos", "svc-a", ".git")
	if err := osMkdirAll(fakeCommonDir); err != nil {
		t.Fatal(err)
	}
	gitMock := &mockGitClient{
		commonDirFn:          func(path string) (string, error) { return fakeCommonDir, nil },
		listWorktreesRes:     []git.WorktreeEntry{{Path: svcPath, Branch: "refs/heads/release/1.2.0"}},
		repoStatusFn:         func(path string) (git.RawStatus, error) { return git.RawStatus{Branch: "release/1.2.0"}, nil },
		isAncestorFn:         func(repoPath, ancestor, descendant string) (bool, error) { return true, nil },
		worktreeBranchResult: "release/1.2.0",
		remoteURLRes:         "git@gitlab.com:group/svc-a.git",
		resolveRefFn: func(_ string, ref string) (string, error) {
			if ref == "refs/tags/v1.2.0" {
				return "tag-oid-sha", nil
			}
			return "sha-source", nil
		},
	}
	cfg := newCloseTestConfig(rootDir, tasksRoot)
	if mutate != nil {
		mutate(gitMock, cfg)
	}
	flow, _ := gitflow.EffectiveConfig(cfg.GitFlow)
	return newTestManagerWithDeps(t, cfg, gitMock, flow, nil), gitMock
}

func TestCloseTask_PushTagUsesCapturedUnpeeledTagObjectOID(t *testing.T) {
	mgr, gitMock := newTagCloseFixture(t, "IN-CLOSE-TAG-OID", nil)

	res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: "IN-CLOSE-TAG-OID"})
	if err != nil {
		t.Fatalf("CloseTask error: %v", err)
	}
	if !res.Success {
		t.Fatalf("result.Success = false, want true: %+v", res.Steps)
	}

	gitMock.mu.Lock()
	pushes := append([]pushTagCall(nil), gitMock.pushTagCallList...)
	resolveCalls := append([]resolveRefCall(nil), gitMock.resolveRefCalls...)
	gitMock.mu.Unlock()

	if len(pushes) != 1 {
		t.Fatalf("PushTag calls = %d, want 1", len(pushes))
	}
	if pushes[0].ObjectOID != "tag-oid-sha" {
		t.Fatalf("PushTag OID = %q, want captured unpeeled tag object tag-oid-sha (not commit sha-source)", pushes[0].ObjectOID)
	}
	wantRefs := map[string]bool{"refs/tags/v1.2.0": false, "tag-oid-sha^{commit}": false}
	for _, call := range resolveCalls {
		if _, ok := wantRefs[call.Ref]; ok {
			wantRefs[call.Ref] = true
		}
	}
	for ref, found := range wantRefs {
		if !found {
			t.Fatalf("ResolveRef calls = %+v, missing %s", resolveCalls, ref)
		}
	}
}

func TestCloseTask_ExistingTagMatchingSource_VerifiesAndPushesCapturedOID(t *testing.T) {
	mgr, gitMock := newTagCloseFixture(t, "IN-CLOSE-TAG-VERIFY", func(gitMock *mockGitClient, _ *config.Config) {
		tagExistsCalls := 0
		gitMock.tagExistsFn = func(_ string, _ string) (bool, error) {
			tagExistsCalls++
			return tagExistsCalls > 1, nil // plan sees absent, execute sees present
		}
	})

	res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: "IN-CLOSE-TAG-VERIFY"})
	if err != nil {
		t.Fatalf("CloseTask error: %v", err)
	}
	if !res.Success {
		t.Fatalf("result.Success = false, want true: %+v", res.Steps)
	}
	if status, msg := closeStepStatus(res, "svc-a:tag"); status != StepStatusSkipped {
		t.Fatalf("tag step = %q (%s), want skipped: verified existing tag", status, msg)
	}

	gitMock.mu.Lock()
	createCalls := len(gitMock.createTagCallList)
	pushes := append([]pushTagCall(nil), gitMock.pushTagCallList...)
	gitMock.mu.Unlock()

	if createCalls != 0 {
		t.Fatalf("CreateTag calls = %d, want 0: existing matching tag must not move", createCalls)
	}
	if len(pushes) != 1 || pushes[0].ObjectOID != "tag-oid-sha" {
		t.Fatalf("PushTag calls = %#v, want 1 push with captured OID", pushes)
	}
}

func TestCloseTask_ExistingTagMismatch_NotPushed(t *testing.T) {
	mgr, gitMock := newTagCloseFixture(t, "IN-CLOSE-TAG-MISMATCH", func(gitMock *mockGitClient, _ *config.Config) {
		tagExistsCalls := 0
		gitMock.tagExistsFn = func(_ string, _ string) (bool, error) {
			tagExistsCalls++
			return tagExistsCalls > 1, nil
		}
		gitMock.resolveRefFn = func(_ string, ref string) (string, error) {
			if ref == "tag-oid-sha^{commit}" {
				return "sha-wrong", nil
			}
			if ref == "refs/tags/v1.2.0" {
				return "tag-oid-sha", nil
			}
			return "sha-source", nil
		}
	})

	res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: "IN-CLOSE-TAG-MISMATCH"})
	if err == nil {
		t.Fatal("CloseTask error = nil, want tag mismatch error")
	}
	if res.Success {
		t.Fatal("result.Success = true, want false")
	}
	if status, msg := closeStepStatus(res, "svc-a:tag"); status != StepStatusFailed || !strings.Contains(msg, "sha-wrong") {
		t.Fatalf("tag step = %q %q, want failed with wrong SHA", status, msg)
	}

	gitMock.mu.Lock()
	createCalls := len(gitMock.createTagCallList)
	pushCalls := gitMock.pushTagCalls
	gitMock.mu.Unlock()
	if createCalls != 0 || pushCalls != 0 {
		t.Fatalf("CreateTag = %d PushTag = %d, want 0/0: mismatched tag must not be created or pushed", createCalls, pushCalls)
	}
}

func TestCloseTask_TagObjectResolutionFailure_BlocksPush(t *testing.T) {
	mgr, gitMock := newTagCloseFixture(t, "IN-CLOSE-TAG-OID-FAIL", func(gitMock *mockGitClient, _ *config.Config) {
		gitMock.resolveRefFn = func(_ string, ref string) (string, error) {
			if ref == "refs/tags/v2.0.0" {
				return "", errors.New("tag object vanished")
			}
			return "sha-source", nil
		}
	})

	res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: "IN-CLOSE-TAG-OID-FAIL", TagVersion: "2.0.0"})
	if err == nil {
		t.Fatal("CloseTask error = nil, want tag object resolution failure")
	}
	if res.Success {
		t.Fatal("result.Success = true, want false")
	}

	gitMock.mu.Lock()
	createCalls := len(gitMock.createTagCallList)
	pushCalls := gitMock.pushTagCalls
	leaseCalls := len(gitMock.deleteTagIfUnchangedCallList)
	gitMock.mu.Unlock()
	if createCalls != 1 {
		t.Fatalf("CreateTag calls = %d, want 1 before resolution failure", createCalls)
	}
	if pushCalls != 0 {
		t.Fatalf("PushTag calls = %d, want 0", pushCalls)
	}
	if leaseCalls != 0 {
		t.Fatalf("DeleteTagIfUnchanged calls = %d, want 0: push never attempted", leaseCalls)
	}
}

func closeStepStatus(res CloseTaskResult, name string) (StepStatus, string) {
	for _, st := range res.Steps {
		if st.Name == name {
			return st.Status, st.Message
		}
	}
	return "", ""
}

func newCloseTestConfig(rootDir, tasksRoot string) *config.Config {
	cfg := &config.Config{RootDir: rootDir, TasksRoot: tasksRoot, BranchPrefix: "feature/", BaseBranch: "develop", Editor: "code"}
	if _, err := cfg.Effective(); err != nil {
		panic(err)
	}
	releaseRule := cfg.GitFlow.BranchTypes[string(gitflow.BranchTypeRelease)]
	releaseRule.CloseStrategy = string(gitflow.CloseStrategyDirectMerge)
	releaseRule.TagOnClose = true
	cfg.GitFlow.BranchTypes[string(gitflow.BranchTypeRelease)] = releaseRule
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
