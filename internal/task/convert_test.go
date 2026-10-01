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
	gitMock.pushRefWithLeaseFn = func(_, capturedURL, targetRef, exactOID, leaseRef, leaseOID string) error {
		operations = append(operations, "push")
		if capturedURL != "git@github.com:org/repo.git" {
			t.Fatalf("PushRefWithLease capturedURL = %q, want verified origin URL", capturedURL)
		}
		if targetRef != "refs/heads/"+targetBranch || leaseRef != targetRef || leaseOID != "" {
			t.Fatalf("PushRefWithLease refs = %q/%q/%q", targetRef, leaseRef, leaseOID)
		}
		if exactOID != sha {
			t.Fatalf("PushRefWithLease exactOID = %q, want %q", exactOID, sha)
		}
		if failPush {
			return errors.New("push failed")
		}
		remoteBranches[targetBranch] = exactOID
		return nil
	}
	gitMock.pushBranchExplicitFn = func(_ string, branch string) error {
		t.Fatalf("PushBranchExplicit called for %q, want lease-based PushRefWithLease", branch)
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

	wantOps := []string{"create", "push", "push", "remove-source-worktree", "delete-local", "move-target"}
	if !slices.Equal(operations, wantOps) {
		t.Fatalf("operations = %#v, want %#v", operations, wantOps)
	}
	if _, ok := localBranches[sourceBranch]; ok {
		t.Fatal("local hotfix branch still exists")
	}
	if remoteBranches[sourceBranch] != sha {
		t.Fatalf("remote hotfix branch = %q, want retained at %q", remoteBranches[sourceBranch], sha)
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

func TestConversionSourceExtras_MovesTaskRootFile(t *testing.T) {
	m, _ := newReleasePlanTestManager(t, &mockGitClient{})
	sourceRoot := m.taskDir("APP-1")
	targetRoot := m.taskDir("APP-2")
	if err := os.MkdirAll(filepath.Join(sourceRoot, "svc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "cdc error.md"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := conversionManifest{
		SourceTaskID: "APP-1",
		TargetTaskID: "APP-2",
		Services:     []conversionService{{Name: "svc"}},
	}

	if err := m.requireConvertibleSourceRoot(manifest); err != nil {
		t.Fatalf("requireConvertibleSourceRoot() error = %v", err)
	}
	if err := m.moveConversionSourceExtras(manifest); err != nil {
		t.Fatalf("moveConversionSourceExtras() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(targetRoot, "cdc error.md"))
	if err != nil || string(data) != "keep" {
		t.Fatalf("target file = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(sourceRoot, "cdc error.md")); !os.IsNotExist(err) {
		t.Fatalf("source file still exists: %v", err)
	}
}

func TestConversionSourceExtras_RejectsTargetCollision(t *testing.T) {
	m, _ := newReleasePlanTestManager(t, &mockGitClient{})
	sourceRoot := m.taskDir("APP-1")
	targetRoot := m.taskDir("APP-2")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		filepath.Join(sourceRoot, "notes.txt"): "source",
		filepath.Join(targetRoot, "notes.txt"): "target",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	err := m.requireConvertibleSourceRoot(conversionManifest{SourceTaskID: "APP-1", TargetTaskID: "APP-2"})
	if err == nil || !strings.Contains(err.Error(), "target task root already contains entry notes.txt") {
		t.Fatalf("requireConvertibleSourceRoot() error = %v", err)
	}
	data, readErr := os.ReadFile(filepath.Join(targetRoot, "notes.txt"))
	if readErr != nil || string(data) != "target" {
		t.Fatalf("target file = %q, %v", data, readErr)
	}
}

func TestRequireConvertibleSourceRoot_AllowsDSStore(t *testing.T) {
	m, _ := newReleasePlanTestManager(t, &mockGitClient{})
	root := m.taskDir("APP-1")
	if err := os.MkdirAll(filepath.Join(root, "svc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".DS_Store"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	err := m.requireConvertibleSourceRoot(conversionManifest{
		SourceTaskID: "APP-1",
		Services:     []conversionService{{Name: "svc"}},
	})
	if err != nil {
		t.Fatalf("requireConvertibleSourceRoot() error = %v", err)
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

func TestRetainConversionSourceRemote_SourceRetainedAtExpectedSHA(t *testing.T) {
	const sha = "1111111111111111111111111111111111111111"
	m, _ := newReleasePlanTestManager(t, &mockGitClient{
		remoteBranchExistsRes: true,
		remoteRefSHAFn: func(_, _ string) (string, error) {
			return sha, nil
		},
	})
	statusCh := make(chan string, 2)

	err := m.retainConversionSourceRemote(context.Background(), conversionService{
		Name:            "svc",
		RepoPath:        "/repo",
		SourceBranch:    "hotfix/APP-1",
		TargetBranch:    "feature/APP-2",
		SourceSHA:       sha,
		SourceRemoteSHA: sha,
	}, statusCh)
	if err != nil {
		t.Fatalf("retainConversionSourceRemote() error = %v", err)
	}
	if first := <-statusCh; !strings.Contains(first, "retaining remote hotfix/APP-1") {
		t.Fatalf("status = %q, want retaining remote", first)
	}
}

func TestRetainConversionSourceRemote_SourceMovedStillRetained(t *testing.T) {
	const (
		planned = "1111111111111111111111111111111111111111"
		moved   = "2222222222222222222222222222222222222222"
	)
	m, _ := newReleasePlanTestManager(t, &mockGitClient{
		remoteBranchExistsRes: true,
		remoteRefSHAFn: func(_, _ string) (string, error) {
			return moved, nil
		},
	})
	statusCh := make(chan string, 2)

	err := m.retainConversionSourceRemote(context.Background(), conversionService{
		Name:            "svc",
		RepoPath:        "/repo",
		SourceBranch:    "hotfix/APP-1",
		TargetBranch:    "feature/APP-2",
		SourceSHA:       planned,
		SourceRemoteSHA: planned,
	}, statusCh)
	if err != nil {
		t.Fatalf("retainConversionSourceRemote() error = %v, want nil (source retained, not deleted)", err)
	}
	if first := <-statusCh; !strings.Contains(first, "retaining remote hotfix/APP-1") || !strings.Contains(first, moved) {
		t.Fatalf("status = %q, want retaining with moved SHA", first)
	}
}

func TestEnsureConversionTargetPushed_ExactMatchAcceptsWithoutPush(t *testing.T) {
	const sha = "1111111111111111111111111111111111111111"
	m, gitMock := newReleasePlanTestManager(t, &mockGitClient{
		remoteRefSHAFn: func(_, ref string) (string, error) {
			if ref != "refs/heads/feature/APP-1" {
				t.Errorf("RemoteRefSHA ref = %q", ref)
			}
			return sha, nil
		},
	})

	err := m.ensureConversionTargetPushed(context.Background(), conversionService{
		Name:         "svc",
		RepoPath:     "/repo",
		TargetBranch: "feature/APP-1",
		SourceSHA:    sha,
	}, nil)
	if err != nil {
		t.Fatalf("ensureConversionTargetPushed() error = %v", err)
	}
	gitMock.mu.Lock()
	pushes := len(gitMock.pushRefWithLeaseCalls)
	gitMock.mu.Unlock()
	if pushes != 0 {
		t.Fatalf("PushRefWithLease calls = %d, want 0 for exact match", pushes)
	}
}

func TestEnsureConversionTargetPushed_AbsentTargetPushesWithAbsentLease(t *testing.T) {
	const sha = "1111111111111111111111111111111111111111"
	remote := map[string]string{}
	m, gitMock := newReleasePlanTestManager(t, &mockGitClient{
		remoteRefSHAFn: func(_, ref string) (string, error) {
			return remote[ref], nil
		},
	})
	gitMock.pushRefWithLeaseFn = func(_, capturedURL, targetRef, exactOID, leaseRef, leaseOID string) error {
		if capturedURL != "git@github.com:org/repo.git" {
			t.Fatalf("PushRefWithLease capturedURL = %q, want verified origin URL", capturedURL)
		}
		if targetRef != "refs/heads/feature/APP-1" || leaseRef != targetRef || leaseOID != "" {
			t.Fatalf("PushRefWithLease refs = %q/%q/%q", targetRef, leaseRef, leaseOID)
		}
		remote[targetRef] = exactOID
		return nil
	}

	err := m.ensureConversionTargetPushed(context.Background(), conversionService{
		Name:         "svc",
		RepoPath:     "/repo",
		TargetBranch: "feature/APP-1",
		SourceSHA:    sha,
	}, nil)
	if err != nil {
		t.Fatalf("ensureConversionTargetPushed() error = %v", err)
	}
	if remote["refs/heads/feature/APP-1"] != sha {
		t.Fatalf("remote target = %q, want %q", remote["refs/heads/feature/APP-1"], sha)
	}
	gitMock.mu.Lock()
	pushes := len(gitMock.pushRefWithLeaseCalls)
	gitMock.mu.Unlock()
	if pushes != 1 {
		t.Fatalf("PushRefWithLease calls = %d, want 1", pushes)
	}
}

func TestEnsureConversionTargetPushed_PushURLMismatchFailsClosed(t *testing.T) {
	const sha = "1111111111111111111111111111111111111111"
	m, gitMock := newReleasePlanTestManager(t, &mockGitClient{
		remoteURLRes: "git@gitlab.com:group/repo.git",
		pushURLRes:   "git@gitlab.com:group/someone-else.git",
		remoteRefSHAFn: func(_, _ string) (string, error) {
			return "", nil
		},
	})

	err := m.ensureConversionTargetPushed(context.Background(), conversionService{
		Name:         "svc",
		RepoPath:     "/repo",
		TargetBranch: "feature/APP-1",
		SourceSHA:    sha,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "push URL") {
		t.Fatalf("ensureConversionTargetPushed() error = %v, want push URL mismatch", err)
	}
	gitMock.mu.Lock()
	pushes := len(gitMock.pushRefWithLeaseCalls)
	gitMock.mu.Unlock()
	if pushes != 0 {
		t.Fatalf("PushRefWithLease calls = %d, want 0 for push URL mismatch", pushes)
	}
}

func TestEnsureConversionTargetPushed_ConflictingTargetFailsClosed(t *testing.T) {
	const (
		sha      = "1111111111111111111111111111111111111111"
		conflict = "2222222222222222222222222222222222222222"
	)
	m, gitMock := newReleasePlanTestManager(t, &mockGitClient{
		remoteRefSHAFn: func(_, _ string) (string, error) {
			return conflict, nil
		},
	})

	err := m.ensureConversionTargetPushed(context.Background(), conversionService{
		Name:         "svc",
		RepoPath:     "/repo",
		TargetBranch: "feature/APP-1",
		SourceSHA:    sha,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "points to") {
		t.Fatalf("ensureConversionTargetPushed() error = %v, want conflict", err)
	}
	gitMock.mu.Lock()
	pushes := len(gitMock.pushRefWithLeaseCalls)
	gitMock.mu.Unlock()
	if pushes != 0 {
		t.Fatalf("PushRefWithLease calls = %d, want 0 for conflict", pushes)
	}
}

func TestEnsureConversionTargetPushed_LeaseRejectedFails(t *testing.T) {
	const sha = "1111111111111111111111111111111111111111"
	m, gitMock := newReleasePlanTestManager(t, &mockGitClient{
		remoteRefSHAFn: func(_, _ string) (string, error) {
			return "", nil
		},
	})
	gitMock.pushRefWithLeaseFn = func(_, _, _, _, _, _ string) error {
		return errors.New("stale info")
	}

	err := m.ensureConversionTargetPushed(context.Background(), conversionService{
		Name:         "svc",
		RepoPath:     "/repo",
		TargetBranch: "feature/APP-1",
		SourceSHA:    sha,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "stale info") {
		t.Fatalf("ensureConversionTargetPushed() error = %v, want lease rejection", err)
	}
}

func TestRemoveConversionSourceRoot_RemovesKnownArtifactsAndEmptyDirs(t *testing.T) {
	m, _ := newReleasePlanTestManager(t, &mockGitClient{})
	root := m.taskDir("APP-1")
	if err := os.MkdirAll(filepath.Join(root, "svc"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{conversionMarkerName, "APP-1.code-workspace", "APP-1.sln", ".DS_Store"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("generated"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := conversionManifest{
		SourceTaskID: "APP-1",
		TargetTaskID: "APP-2",
		Services:     []conversionService{{Name: "svc"}},
	}

	if err := m.removeConversionSourceRoot(manifest); err != nil {
		t.Fatalf("removeConversionSourceRoot() error = %v", err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("source root still exists: %v", err)
	}
}

func TestRemoveConversionSourceRoot_PreservesUnknownAndReplacementEntries(t *testing.T) {
	m, _ := newReleasePlanTestManager(t, &mockGitClient{})
	root := m.taskDir("APP-1")
	replacementFile := filepath.Join(root, "svc", "REPLACEMENT.txt")
	if err := os.MkdirAll(filepath.Dir(replacementFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replacementFile, []byte("user data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "race-notes.txt"), []byte("concurrent"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, conversionMarkerName), []byte("marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := conversionManifest{
		SourceTaskID: "APP-1",
		TargetTaskID: "APP-2",
		Services:     []conversionService{{Name: "svc"}},
	}

	err := m.removeConversionSourceRoot(manifest)
	if err == nil || !strings.Contains(err.Error(), "race-notes.txt") || !strings.Contains(err.Error(), "svc") {
		t.Fatalf("removeConversionSourceRoot() error = %v, want preserved entries listed", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "race-notes.txt")); statErr != nil {
		t.Fatalf("race-injected file lost: %v", statErr)
	}
	if _, statErr := os.Stat(replacementFile); statErr != nil {
		t.Fatalf("replacement service content lost: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, conversionMarkerName)); !os.IsNotExist(statErr) {
		t.Fatalf("known marker not removed: %v", statErr)
	}
}

func TestRemoveConversionStagingDir_RemovesEmptyStaging(t *testing.T) {
	stagingDir := filepath.Join(t.TempDir(), "staging")
	if err := os.MkdirAll(filepath.Join(stagingDir, "services", "svc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, conversionMarkerName), []byte("marker"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := removeConversionStagingDir(stagingDir); err != nil {
		t.Fatalf("removeConversionStagingDir() error = %v", err)
	}
	if _, err := os.Stat(stagingDir); !os.IsNotExist(err) {
		t.Fatalf("staging dir still exists: %v", err)
	}
}

func TestRemoveConversionStagingDir_PreservesUnknownEntries(t *testing.T) {
	stagingDir := filepath.Join(t.TempDir(), "staging")
	if err := os.MkdirAll(filepath.Join(stagingDir, "services"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"leftover.txt", conversionMarkerName} {
		if err := os.WriteFile(filepath.Join(stagingDir, name), []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	err := removeConversionStagingDir(stagingDir)
	if err == nil || !strings.Contains(err.Error(), "leftover.txt") {
		t.Fatalf("removeConversionStagingDir() error = %v, want preserved entry listed", err)
	}
	if _, statErr := os.Stat(filepath.Join(stagingDir, "leftover.txt")); statErr != nil {
		t.Fatalf("unknown staging entry lost: %v", statErr)
	}
	if _, statErr := os.Stat(stagingDir); statErr != nil {
		t.Fatalf("staging dir with preserved entry must survive: %v", statErr)
	}
}

func TestConvertHotfixToFeature_DifferentIDPreservesReplacementServiceDir(t *testing.T) {
	const (
		sourceTaskID = "APP-1"
		targetTaskID = "APP-2"
		sourceBranch = "hotfix/APP-1"
		targetBranch = "feature/APP-2"
		sha          = "1111111111111111111111111111111111111111"
	)

	m, gitMock := newReleasePlanTestManager(t, &mockGitClient{})
	repoPath := filepath.Join(m.cfg.RootDir, "repo-a")
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock, releasePlanTaskService{
		TaskID: sourceTaskID, ServiceName: "svc", Branch: sourceBranch, RepoPath: repoPath,
	})
	sourceRoot := m.taskDir(sourceTaskID)

	localBranches := map[string]string{sourceBranch: sha}
	remoteBranches := map[string]string{sourceBranch: sha}
	worktrees := slices.Clone(gitMock.listWorktreesRes)
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
	gitMock.pushRefWithLeaseFn = func(_, _, targetRef, exactOID, _, _ string) error {
		if targetRef != "refs/heads/"+targetBranch || exactOID != sha {
			t.Fatalf("PushRefWithLease ref/OID = %q/%q", targetRef, exactOID)
		}
		if failPush {
			return errors.New("push failed")
		}
		remoteBranches[targetBranch] = exactOID
		return nil
	}
	gitMock.removeWorktreeFn = func(_, path string, force bool) error {
		if force {
			t.Fatal("source worktree removal must not force")
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		worktrees = slices.DeleteFunc(worktrees, func(entry git.WorktreeEntry) bool { return entry.Path == path })
		if path == filepath.Join(sourceRoot, "svc") {
			// Model the race the cleanup must survive: replacement content
			// repopulates the source worktree dir inside the cleanup window.
			replacement := filepath.Join(path, "REPLACEMENT.txt")
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(replacement, []byte("user data"), 0o600); err != nil {
				return err
			}
		}
		return nil
	}
	gitMock.deleteBranchIfUnchangedFn = func(_ string, branch, expectedSHA string) error {
		if localBranches[branch] != expectedSHA {
			return errors.New("local lease mismatch")
		}
		delete(localBranches, branch)
		return nil
	}
	gitMock.moveWorktreeFn = func(_ string, from, to string) error {
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

	params := ConvertHotfixParams{SourceTaskID: sourceTaskID, TargetTaskID: targetTaskID}
	if err := m.ConvertHotfixToFeature(context.Background(), params); err == nil {
		t.Fatal("first conversion must fail during push")
	}

	failPush = false
	replacementFile := filepath.Join(sourceRoot, "svc", "REPLACEMENT.txt")
	err := m.ConvertHotfixToFeature(context.Background(), params)
	if err == nil || !strings.Contains(err.Error(), "preserved") {
		t.Fatalf("second conversion error = %v, want preserved-entries blocker", err)
	}
	if _, statErr := os.Stat(replacementFile); statErr != nil {
		t.Fatalf("replacement content lost: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(m.taskDir(targetTaskID), conversionMarkerName)); statErr != nil {
		t.Fatalf("resumable checkpoint marker not restored in target: %v", statErr)
	}
	if _, statErr := os.Stat(m.conversionStagingDir(sourceTaskID, targetTaskID)); statErr != nil {
		t.Fatalf("staging dir must survive a blocked cleanup: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(m.taskDir(targetTaskID), "svc")); statErr != nil {
		t.Fatalf("promoted target worktree missing: %v", statErr)
	}

	if err := os.RemoveAll(filepath.Join(sourceRoot, "svc")); err != nil {
		t.Fatal(err)
	}
	if err := m.ConvertHotfixToFeature(context.Background(), params); err != nil {
		t.Fatalf("third conversion error = %v, want success after blocker cleared", err)
	}
	if _, err := os.Stat(sourceRoot); !os.IsNotExist(err) {
		t.Fatalf("source root still exists: %v", err)
	}
	if _, err := os.Stat(m.conversionStagingDir(sourceTaskID, targetTaskID)); !os.IsNotExist(err) {
		t.Fatalf("staging dir still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.taskDir(targetTaskID), "svc")); err != nil {
		t.Fatalf("final worktree missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.taskDir(targetTaskID), targetTaskID+".code-workspace")); err != nil {
		t.Fatalf("workspace not generated: %v", err)
	}
}
