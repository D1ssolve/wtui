package task

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

// newDirectMergeCloseFixture builds a single-service task whose branch rule
// closes by direct merge. resolveSHAs maps refs to SHAs for the fresh remote
// target and merged local target resolutions.
func newDirectMergeCloseFixture(t *testing.T, taskID, branch string, targets []string, mutate func(*mockGitClient, *config.Config)) (Manager, *mockGitClient) {
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

	remoteSHA := func(target string) string { return "remote-" + target + "-sha" }
	mergedSHA := func(target string) string { return "merged-" + target + "-sha" }

	gitMock := &mockGitClient{
		commonDirFn:          func(path string) (string, error) { return fakeCommonDir, nil },
		listWorktreesRes:     []git.WorktreeEntry{{Path: svcPath, Branch: "refs/heads/" + branch}},
		repoStatusFn:         func(path string) (git.RawStatus, error) { return git.RawStatus{Branch: branch}, nil },
		worktreeBranchResult: branch,
		remoteURLRes:         "git@gitlab.com:group/svc-a.git",
		isAncestorFn: func(_ string, ancestor, descendant string) (bool, error) {
			// The merged local target must build on the fresh remote
			// target: remote-<t>-sha is always an ancestor of
			// merged-<t>-sha here.
			return strings.HasPrefix(ancestor, "remote-") && strings.HasPrefix(descendant, "merged-"), nil
		},
		resolveRefFn: func(_ string, ref string) (string, error) {
			for _, target := range targets {
				if ref == "origin/"+target {
					return remoteSHA(target), nil
				}
				if ref == "refs/heads/"+target {
					return mergedSHA(target), nil
				}
			}
			return ref + "-sha", nil
		},
	}

	cfg := newCloseTestConfig(rootDir, tasksRoot)
	if mutate != nil {
		mutate(gitMock, cfg)
	}
	flow, err := gitflow.EffectiveConfig(cfg.GitFlow)
	if err != nil {
		t.Fatalf("flow: %v", err)
	}
	return newTestManagerWithDeps(t, cfg, gitMock, flow, nil), gitMock
}

func leasePushes(gitMock *mockGitClient) []pushRefWithLeaseCall {
	gitMock.mu.Lock()
	defer gitMock.mu.Unlock()
	return append([]pushRefWithLeaseCall(nil), gitMock.pushRefWithLeaseCalls...)
}

func TestCloseTask_DirectMerge_PublishesMergedTargetWithLease(t *testing.T) {
	mgr, gitMock := newDirectMergeCloseFixture(t, "IN-CLOSE-DM-LEASE", "feature/IN-CLOSE-DM-LEASE", []string{"develop"}, nil)

	res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: "IN-CLOSE-DM-LEASE"})
	if err != nil {
		t.Fatalf("CloseTask error: %v", err)
	}
	if !res.Success {
		t.Fatalf("result.Success = false, want true: %+v", res.Steps)
	}

	pushes := leasePushes(gitMock)
	if len(pushes) != 1 {
		t.Fatalf("PushRefWithLease calls = %d, want 1", len(pushes))
	}
	push := pushes[0]
	if push.TargetRef != "refs/heads/develop" || push.LeaseRef != "refs/heads/develop" {
		t.Fatalf("refs = %q/%q, want refs/heads/develop pair", push.TargetRef, push.LeaseRef)
	}
	if push.ExactOID != "merged-develop-sha" {
		t.Fatalf("exactOID = %q, want merged local target OID", push.ExactOID)
	}
	if push.LeaseOID != "remote-develop-sha" {
		t.Fatalf("leaseOID = %q, want fresh pre-merge remote target OID", push.LeaseOID)
	}
	if push.CapturedURL != "git@gitlab.com:group/svc-a.git" {
		t.Fatalf("capturedURL = %q, want verified origin push URL, never the mutable remote name", push.CapturedURL)
	}

	gitMock.mu.Lock()
	genericPushes := len(gitMock.pushCalls)
	mergeCalls := len(gitMock.mergeCalls)
	gitMock.mu.Unlock()
	if genericPushes != 0 {
		t.Fatalf("generic Push calls = %d, want 0", genericPushes)
	}
	if mergeCalls != 1 {
		t.Fatalf("Merge calls = %d, want 1", mergeCalls)
	}
}

func TestCloseTask_DirectMerge_RemoteConcurrentMoveFails(t *testing.T) {
	mgr, gitMock := newDirectMergeCloseFixture(t, "IN-CLOSE-DM-RACE", "feature/IN-CLOSE-DM-RACE", []string{"develop"},
		func(gitMock *mockGitClient, _ *config.Config) {
			gitMock.pushRefWithLeaseFn = func(_, _, _, _, _, _ string) error {
				return errors.New("lease failed: remote ref changed")
			}
		})

	res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: "IN-CLOSE-DM-RACE"})
	if err == nil {
		t.Fatal("CloseTask error = nil, want lease failure")
	}
	if res.Success {
		t.Fatal("result.Success = true, want false on concurrent target movement")
	}
	status, _ := closeStepStatus(res, "svc-a:push:develop")
	if status != StepStatusFailed {
		t.Fatalf("svc-a:push:develop status = %q, want failed: %+v", status, res.Steps)
	}
	if pushes := leasePushes(gitMock); len(pushes) != 1 {
		t.Fatalf("PushRefWithLease calls = %d, want 1 attempted push", len(pushes))
	}
}

func TestCloseTask_DirectMerge_PriorLocalMergeUnpushedRetryPublishes(t *testing.T) {
	mgr, gitMock := newDirectMergeCloseFixture(t, "IN-CLOSE-DM-RETRY", "feature/IN-CLOSE-DM-RETRY", []string{"develop"},
		func(gitMock *mockGitClient, _ *config.Config) {
			// Local develop already contains the source; the fresh remote
			// develop does not. Retry must publish, not skip.
			gitMock.isAncestorFn = func(_ string, ancestor, descendant string) (bool, error) {
				return descendant == "develop" ||
					(strings.HasPrefix(ancestor, "remote-") && strings.HasPrefix(descendant, "merged-")), nil
			}
		})

	res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: "IN-CLOSE-DM-RETRY"})
	if err != nil {
		t.Fatalf("CloseTask error: %v", err)
	}
	if !res.Success {
		t.Fatalf("result.Success = false, want true: %+v", res.Steps)
	}

	gitMock.mu.Lock()
	mergeCalls := len(gitMock.mergeCalls)
	gitMock.mu.Unlock()
	if mergeCalls != 0 {
		t.Fatalf("Merge calls = %d, want 0 for already-merged local target", mergeCalls)
	}

	pushes := leasePushes(gitMock)
	if len(pushes) != 1 {
		t.Fatalf("PushRefWithLease calls = %d, want 1", len(pushes))
	}
	if pushes[0].ExactOID != "merged-develop-sha" || pushes[0].LeaseOID != "remote-develop-sha" {
		t.Fatalf("push = %+v, want local merged OID leased on fresh remote OID", pushes[0])
	}
}

func TestCloseTask_DirectMerge_PushURLMismatchFailsWithoutPush(t *testing.T) {
	mgr, gitMock := newDirectMergeCloseFixture(t, "IN-CLOSE-DM-PUSHURL", "feature/IN-CLOSE-DM-PUSHURL", []string{"develop"},
		func(gitMock *mockGitClient, _ *config.Config) {
			gitMock.pushURLFn = func(string, string) (string, error) {
				return "git@evil.example.com:group/svc-a.git", nil
			}
		})

	res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: "IN-CLOSE-DM-PUSHURL"})
	if err == nil {
		t.Fatal("CloseTask error = nil, want push URL mismatch failure")
	}
	if res.Success {
		t.Fatal("result.Success = true, want false on push URL mismatch")
	}
	if pushes := leasePushes(gitMock); len(pushes) != 0 {
		t.Fatalf("PushRefWithLease calls = %d, want 0 for push URL mismatch", len(pushes))
	}
}

func TestCloseTask_DirectMerge_ProtectedTargetsPublishWithLease(t *testing.T) {
	// Release branch closes directly into protected production and
	// integration targets; both publish via lease, never the generic guard.
	mgr, gitMock := newDirectMergeCloseFixture(t, "IN-CLOSE-DM-REL", "release/1.2.0", []string{"master", "develop"},
		func(_ *mockGitClient, cfg *config.Config) {
			releaseRule := cfg.GitFlow.BranchTypes[string(gitflow.BranchTypeRelease)]
			releaseRule.TagOnClose = false
			cfg.GitFlow.BranchTypes[string(gitflow.BranchTypeRelease)] = releaseRule
		})

	res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: "IN-CLOSE-DM-REL"})
	if err != nil {
		t.Fatalf("CloseTask error: %v", err)
	}
	if !res.Success {
		t.Fatalf("result.Success = false, want true: %+v", res.Steps)
	}

	pushes := leasePushes(gitMock)
	if len(pushes) != 2 {
		t.Fatalf("PushRefWithLease calls = %d, want 2", len(pushes))
	}
	want := map[string][2]string{
		"refs/heads/master":  {"merged-master-sha", "remote-master-sha"},
		"refs/heads/develop": {"merged-develop-sha", "remote-develop-sha"},
	}
	for _, push := range pushes {
		w, ok := want[push.TargetRef]
		if !ok {
			t.Fatalf("unexpected target ref %q", push.TargetRef)
		}
		if push.ExactOID != w[0] || push.LeaseOID != w[1] || push.LeaseRef != push.TargetRef {
			t.Fatalf("push = %+v, want exact %s lease %s", push, w[0], w[1])
		}
		delete(want, push.TargetRef)
	}
	if len(want) != 0 {
		t.Fatalf("unpushed targets: %v", want)
	}
}

func TestCloseTask_DirectMerge_RemoteAlreadyContainsSourceSkipsPush(t *testing.T) {
	mgr, gitMock := newDirectMergeCloseFixture(t, "IN-CLOSE-DM-SKIP", "feature/IN-CLOSE-DM-SKIP", []string{"develop"},
		func(gitMock *mockGitClient, _ *config.Config) {
			gitMock.isAncestorFn = func(_ string, _, _ string) (bool, error) { return true, nil }
		})

	res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: "IN-CLOSE-DM-SKIP"})
	if err != nil {
		t.Fatalf("CloseTask error: %v", err)
	}
	if !res.Success {
		t.Fatalf("result.Success = false, want true: %+v", res.Steps)
	}
	if pushes := leasePushes(gitMock); len(pushes) != 0 {
		t.Fatalf("PushRefWithLease calls = %d, want 0 when remote target already contains source", len(pushes))
	}
	status, _ := closeStepStatus(res, "svc-a:merge:develop")
	if status != StepStatusSkipped {
		t.Fatalf("svc-a:merge:develop status = %q, want skipped", status)
	}
}

func TestCloseTask_DirectMerge_StaleLocalTargetDivergedFailsWithoutPush(t *testing.T) {
	mgr, gitMock := newDirectMergeCloseFixture(t, "IN-CLOSE-DM-STALE", "feature/IN-CLOSE-DM-STALE", []string{"develop"},
		func(gitMock *mockGitClient, _ *config.Config) {
			gitMock.isAncestorFn = func(_, _, _ string) (bool, error) {
				// Remote develop does not contain the source (merge path),
				// and the merged local target does not build on the fresh
				// remote target (stale/diverged local target).
				return false, nil
			}
		})

	res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: "IN-CLOSE-DM-STALE"})
	if err == nil {
		t.Fatal("CloseTask error = nil, want diverged local target failure")
	}
	if res.Success {
		t.Fatal("result.Success = true, want false on diverged local target")
	}
	status, msg := closeStepStatus(res, "svc-a:push:develop")
	if status != StepStatusFailed || !strings.Contains(msg, "diverged") {
		t.Fatalf("svc-a:push:develop step = %q %q, want failed containing diverged", status, msg)
	}
	if pushes := leasePushes(gitMock); len(pushes) != 0 {
		t.Fatalf("PushRefWithLease calls = %d, want 0 for diverged local target", len(pushes))
	}
}

func TestCloseTask_DirectMerge_CapturedPushURLSurvivesConcurrentPushurlChange(t *testing.T) {
	mgr, gitMock := newDirectMergeCloseFixture(t, "IN-CLOSE-DM-PURL", "feature/IN-CLOSE-DM-PURL", []string{"develop"},
		func(gitMock *mockGitClient, _ *config.Config) {
			calls := 0
			gitMock.pushURLFn = func(string, string) (string, error) {
				calls++
				if calls == 1 {
					return "git@gitlab.com:group/svc-a.git", nil
				}
				return "git@evil.example.com:group/svc-a.git", nil
			}
		})

	res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: "IN-CLOSE-DM-PURL"})
	if err != nil {
		t.Fatalf("CloseTask error: %v", err)
	}
	if !res.Success {
		t.Fatalf("result.Success = false, want true: %+v", res.Steps)
	}
	pushes := leasePushes(gitMock)
	if len(pushes) != 1 {
		t.Fatalf("PushRefWithLease calls = %d, want 1", len(pushes))
	}
	if pushes[0].CapturedURL != "git@gitlab.com:group/svc-a.git" {
		t.Fatalf("capturedURL = %q, want URL captured at verification time", pushes[0].CapturedURL)
	}
}

func TestCloseTask_DirectMerge_ResolveRemoteTargetFailureFailsClosed(t *testing.T) {
	mgr, gitMock := newDirectMergeCloseFixture(t, "IN-CLOSE-DM-NOREMOTE", "feature/IN-CLOSE-DM-NOREMOTE", []string{"develop"},
		func(gitMock *mockGitClient, _ *config.Config) {
			gitMock.resolveRefFn = func(_ string, ref string) (string, error) {
				if strings.HasPrefix(ref, "origin/") {
					return "", errors.New("remote target unresolvable")
				}
				return ref + "-sha", nil
			}
		})

	res, err := mgr.CloseTask(context.Background(), CloseTaskParams{TaskID: "IN-CLOSE-DM-NOREMOTE"})
	if err == nil {
		t.Fatal("CloseTask error = nil, want unresolved remote target failure")
	}
	if res.Success {
		t.Fatal("result.Success = true, want false")
	}
	if pushes := leasePushes(gitMock); len(pushes) != 0 {
		t.Fatalf("PushRefWithLease calls = %d, want 0", len(pushes))
	}
}
