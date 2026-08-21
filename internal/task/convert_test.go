package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/git"
)

func TestConvertHotfixToFeature_SameIDStagesPushesThenSwapsTask(t *testing.T) {
	const (
		taskID       = "APP-1"
		sourceBranch = "hotfix/APP-1"
		targetBranch = "feature/APP-1"
		sha          = "1111111111111111111111111111111111111111"
	)

	m, gitMock := newReleasePlanTestManager(t, &mockGitClient{})
	repoPath := filepath.Join(m.cfg.RootDir, "repo-a")
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock, releasePlanTaskService{
		TaskID: taskID, ServiceName: "svc", Branch: sourceBranch, RepoPath: repoPath,
	})
	sourcePath := filepath.Join(m.cfg.TasksRoot, taskID, "svc")
	finalPath := sourcePath

	localBranches := map[string]string{sourceBranch: sha}
	remoteBranches := map[string]string{sourceBranch: sha}
	worktrees := slices.Clone(gitMock.listWorktreesRes)
	var operations []string
	failPush := true

	gitMock.listWorktreesFn = func(string) ([]git.WorktreeEntry, error) {
		return slices.Clone(worktrees), nil
	}
	gitMock.branchExistsFn = func(_ string, branch string) (bool, error) {
		_, ok := localBranches[branch]
		return ok, nil
	}
	gitMock.remoteBranchExistsFn = func(_ string, branch string) (bool, error) {
		_, ok := remoteBranches[branch]
		return ok, nil
	}
	gitMock.resolveRefFn = func(_ string, ref string) (string, error) {
		if value, ok := localBranches[ref]; ok {
			return value, nil
		}
		return "", errors.New("ref not found")
	}
	gitMock.remoteRefSHAFn = func(_ string, ref string) (string, error) {
		return remoteBranches[ref[len("refs/heads/"):]], nil
	}
	gitMock.isAncestorFn = func(_, _, _ string) (bool, error) { return true, nil }
	gitMock.commonDirFn = func(string) (string, error) { return filepath.Join(repoPath, ".git"), nil }
	gitMock.addWorktreeFn = func(_ string, dest, branch string, newBranch bool, base string) error {
		operations = append(operations, "create")
		if !newBranch || base != sourceBranch {
			t.Fatalf("AddWorktree new/base = %v/%q", newBranch, base)
		}
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return err
		}
		localBranches[branch] = sha
		worktrees = append(worktrees, git.WorktreeEntry{Path: dest, Branch: "refs/heads/" + branch})
		return nil
	}
	gitMock.pushBranchExplicitFn = func(_ string, branch string) error {
		operations = append(operations, "push")
		if failPush {
			return errors.New("push failed")
		}
		remoteBranches[branch] = localBranches[branch]
		return nil
	}
	gitMock.deleteRemoteBranchIfUnchangedFn = func(_ string, branch, expectedSHA string) error {
		operations = append(operations, "delete-remote")
		if remoteBranches[branch] != expectedSHA || remoteBranches[targetBranch] != sha {
			return errors.New("lease mismatch")
		}
		delete(remoteBranches, branch)
		return nil
	}
	gitMock.removeWorktreeFn = func(_, path string, force bool) error {
		operations = append(operations, "remove-source-worktree")
		if force {
			t.Fatal("source worktree removal must not force")
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		worktrees = slices.DeleteFunc(worktrees, func(entry git.WorktreeEntry) bool { return entry.Path == path })
		return nil
	}
	gitMock.deleteBranchIfUnchangedFn = func(_ string, branch, expectedSHA string) error {
		operations = append(operations, "delete-local")
		if localBranches[branch] != expectedSHA {
			return errors.New("local lease mismatch")
		}
		delete(localBranches, branch)
		return nil
	}
	gitMock.moveWorktreeFn = func(_ string, from, to string) error {
		operations = append(operations, "move-target")
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		if err := os.Rename(from, to); err != nil {
			return err
		}
		for i := range worktrees {
			if worktrees[i].Path == from {
				worktrees[i].Path = to
			}
		}
		return nil
	}

	params := ConvertHotfixParams{
		SourceTaskID: taskID,
		TargetTaskID: taskID,
	}
	if err := m.ConvertHotfixToFeature(context.Background(), params); err == nil {
		t.Fatal("first conversion must fail during push")
	}
	if _, err := os.Stat(sourcePath); err != nil {
		t.Fatalf("source removed after failed push: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.taskDir(taskID), conversionMarkerName)); err != nil {
		t.Fatalf("retry marker missing after failed push: %v", err)
	}
	if !slices.Equal(operations, []string{"create", "push"}) {
		t.Fatalf("operations after failed push = %#v", operations)
	}

	failPush = false
	err := m.ConvertHotfixToFeature(context.Background(), params)
	if err != nil {
		t.Fatalf("ConvertHotfixToFeature() error = %v", err)
	}

	wantOps := []string{"create", "push", "push", "delete-remote", "remove-source-worktree", "delete-local", "move-target"}
	if !slices.Equal(operations, wantOps) {
		t.Fatalf("operations = %#v, want %#v", operations, wantOps)
	}
	if _, ok := localBranches[sourceBranch]; ok {
		t.Fatal("local hotfix branch still exists")
	}
	if _, ok := remoteBranches[sourceBranch]; ok {
		t.Fatal("remote hotfix branch still exists")
	}
	if localBranches[targetBranch] != sha || remoteBranches[targetBranch] != sha {
		t.Fatalf("feature refs = local:%q remote:%q", localBranches[targetBranch], remoteBranches[targetBranch])
	}
	if _, err := os.Stat(finalPath); err != nil {
		t.Fatalf("final worktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.cfg.TasksRoot, taskID, conversionMarkerName)); !os.IsNotExist(err) {
		t.Fatalf("conversion marker still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.cfg.TasksRoot, taskID, taskID+".code-workspace")); err != nil {
		t.Fatalf("workspace not generated: %v", err)
	}
}

func TestList_HidesConversionStagingAndMarksSourcePending(t *testing.T) {
	m, _ := newReleasePlanTestManager(t, &mockGitClient{})
	if err := os.MkdirAll(m.taskDir("APP-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := conversionManifest{
		Version:      conversionManifestVersion,
		SourceTaskID: "APP-1",
		TargetTaskID: "APP-2",
		StagingDir:   m.conversionStagingDir("APP-1", "APP-2"),
	}
	if err := m.writeConversionManifest(manifest); err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(manifest.StagingDir, conversionMarkerName))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeConversionFile(filepath.Join(m.taskDir("APP-2"), conversionMarkerName), marker); err != nil {
		t.Fatal(err)
	}

	tasks, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != "APP-1" {
		t.Fatalf("tasks = %#v, want source only", tasks)
	}
	if tasks[0].PendingConversionTargetID != "APP-2" {
		t.Fatalf("PendingConversionTargetID = %q, want APP-2", tasks[0].PendingConversionTargetID)
	}
}

func TestPlanHotfixConversion_DifferentTargetIDRewritesBranchAndPath(t *testing.T) {
	const sha = "1111111111111111111111111111111111111111"
	m, gitMock := newReleasePlanTestManager(t, &mockGitClient{resolveRefRes: sha})
	repoPath := filepath.Join(m.cfg.RootDir, "repo-a")
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock, releasePlanTaskService{
		TaskID: "APP-1", ServiceName: "svc", Branch: "hotfix/APP-1-api", RepoPath: repoPath,
	})

	manifest, err := m.planHotfixConversion(context.Background(), ConvertHotfixParams{
		SourceTaskID: "APP-1",
		TargetTaskID: "APP-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := manifest.Services[0].TargetBranch; got != "feature/APP-2-api" {
		t.Fatalf("TargetBranch = %q, want feature/APP-2-api", got)
	}
	if got := manifest.Services[0].FinalWorktreePath; got != filepath.Join(m.cfg.TasksRoot, "APP-2", "svc") {
		t.Fatalf("FinalWorktreePath = %q", got)
	}
}

func TestConvertHotfixToFeature_DirtySourceBlocksBeforeManifest(t *testing.T) {
	m, gitMock := newReleasePlanTestManager(t, &mockGitClient{
		repoStatusFn: func(string) (git.RawStatus, error) {
			return git.RawStatus{UntrackedPaths: []string{"draft.txt"}}, nil
		},
	})
	repoPath := filepath.Join(m.cfg.RootDir, "repo-a")
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock, releasePlanTaskService{
		TaskID: "APP-1", ServiceName: "svc", Branch: "hotfix/APP-1", RepoPath: repoPath,
	})

	err := m.ConvertHotfixToFeature(context.Background(), ConvertHotfixParams{SourceTaskID: "APP-1"})
	if err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("ConvertHotfixToFeature() error = %v, want dirty", err)
	}
	if _, err := os.Stat(filepath.Join(m.taskDir("APP-1"), conversionMarkerName)); !os.IsNotExist(err) {
		t.Fatalf("conversion marker created despite dirty source: %v", err)
	}
	if len(gitMock.addWorktreeCalls) != 0 {
		t.Fatalf("AddWorktree calls = %d, want 0", len(gitMock.addWorktreeCalls))
	}
}

func TestConvertHotfixToFeature_DifferentIDRejectsUnexpectedTaskRootFile(t *testing.T) {
	const sha = "1111111111111111111111111111111111111111"
	m, gitMock := newReleasePlanTestManager(t, &mockGitClient{resolveRefRes: sha})
	repoPath := filepath.Join(m.cfg.RootDir, "repo-a")
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock, releasePlanTaskService{
		TaskID: "APP-1", ServiceName: "svc", Branch: "hotfix/APP-1", RepoPath: repoPath,
	})
	if err := os.WriteFile(filepath.Join(m.taskDir("APP-1"), "notes.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := m.ConvertHotfixToFeature(context.Background(), ConvertHotfixParams{SourceTaskID: "APP-1", TargetTaskID: "APP-2"})
	if err == nil || !strings.Contains(err.Error(), "unexpected entry notes.txt") {
		t.Fatalf("ConvertHotfixToFeature() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.taskDir("APP-1"), "notes.txt")); err != nil {
		t.Fatalf("source file was removed: %v", err)
	}
	if len(gitMock.addWorktreeCalls) != 0 {
		t.Fatalf("AddWorktree calls = %d, want 0", len(gitMock.addWorktreeCalls))
	}
}

func TestPlanHotfixConversion_RejectsTaskIDPrefixCollision(t *testing.T) {
	const sha = "1111111111111111111111111111111111111111"
	m, gitMock := newReleasePlanTestManager(t, &mockGitClient{resolveRefRes: sha})
	repoPath := filepath.Join(m.cfg.RootDir, "repo-a")
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock, releasePlanTaskService{
		TaskID: "APP-1", ServiceName: "svc", Branch: "hotfix/APP-10", RepoPath: repoPath,
	})

	_, err := m.planHotfixConversion(context.Background(), ConvertHotfixParams{SourceTaskID: "APP-1", TargetTaskID: "APP-2"})
	if err == nil || !strings.Contains(err.Error(), "does not match task APP-1") {
		t.Fatalf("planHotfixConversion() error = %v", err)
	}
}

func TestValidateConversionManifest_RejectsDivergentRemoteSHA(t *testing.T) {
	const (
		sourceSHA = "1111111111111111111111111111111111111111"
		remoteSHA = "2222222222222222222222222222222222222222"
	)
	m, gitMock := newReleasePlanTestManager(t, &mockGitClient{
		isAncestorFn: func(_, _, _ string) (bool, error) { return false, nil },
	})
	repoPath := filepath.Join(m.cfg.RootDir, "repo-a")
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock, releasePlanTaskService{
		TaskID: "APP-1", ServiceName: "svc", Branch: "hotfix/APP-1", RepoPath: repoPath,
	})
	manifest := conversionManifest{
		Version:      conversionManifestVersion,
		SourceTaskID: "APP-1",
		TargetTaskID: "APP-1",
		StagingDir:   m.conversionStagingDir("APP-1", "APP-1"),
		Services: []conversionService{{
			Name:                "svc",
			RepoPath:            repoPath,
			SourceWorktreePath:  filepath.Join(m.taskDir("APP-1"), "svc"),
			StagingWorktreePath: filepath.Join(m.conversionStagingDir("APP-1", "APP-1"), "services", "svc"),
			FinalWorktreePath:   filepath.Join(m.taskDir("APP-1"), "svc"),
			SourceBranch:        "hotfix/APP-1",
			TargetBranch:        "feature/APP-1",
			SourceSHA:           sourceSHA,
			SourceRemoteSHA:     remoteSHA,
		}},
	}

	err := m.validateConversionManifest(context.Background(), manifest, "APP-1")
	if err == nil || !strings.Contains(err.Error(), "remote source is not contained") {
		t.Fatalf("validateConversionManifest() error = %v", err)
	}
}
