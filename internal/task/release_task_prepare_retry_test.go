package task

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
)

type taskPrepareRecovery struct {
	m                           *manager
	g                           *mockGitClient
	f                           *releaseTaskMergeForge
	params                      CreateReleaseParams
	tip, local, remote, failure string
}

func newTaskPrepareRecovery(t *testing.T) *taskPrepareRecovery {
	t.Helper()
	x := &taskPrepareRecovery{tip: "d0"}
	x.g = &mockGitClient{remoteURLRes: "git@github.com:org/repo.git"}
	x.g.resolveRefFn = func(_, ref string) (string, error) {
		switch ref {
		case "origin/develop", "HEAD":
			return x.tip, nil
		case "release/1.2.3":
			return x.local, nil
		case "origin/release/1.2.3":
			return x.remote, nil
		default:
			return ref + "-sha", nil
		}
	}
	x.g.branchExistsFn = func(_, branch string) (bool, error) { return branch == "release/1.2.3" && x.local != "", nil }
	x.g.remoteRefSHAFn = func(_, _ string) (string, error) { return x.remote, nil }
	x.g.createBranchFromBranchFn = func(_, _, sha string) error {
		if x.failure == "branch" {
			return errors.New("branch interrupted")
		}
		x.local = sha
		return nil
	}
	x.g.pushBranchExplicitFn = func(_, branch string) error {
		if branch != "release/1.2.3" {
			t.Errorf("unexpected push: %s", branch)
		}
		if x.failure == "push" {
			return errors.New("push interrupted")
		}
		x.remote = x.local
		if x.failure == "lost response" {
			return errors.New("push response lost")
		}
		return nil
	}
	x.g.isAncestorFn = func(_, ancestor, _ string) (bool, error) {
		if strings.HasPrefix(ancestor, "feature/") {
			return false, errors.New("feature ancestry forbidden")
		}
		return ancestor == "d1", nil
	}
	x.m, _ = newReleasePlanTestManager(t, x.g)
	enableReleasePrepareTaskMerge(t, x.m)
	*x.m.cfg.Release.PushIntegration = true
	*x.m.cfg.Release.PushReleaseBranches = true
	*x.m.cfg.Release.CreateReleaseWorktrees = false
	x.f = newReleaseTaskMergeForge()
	x.m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitHub: x.f}
	var specs []releasePlanTaskService
	for n := 1; n <= 2; n++ {
		id := fmt.Sprintf("APP-%d", n)
		x.f.readiness[n] = forge.MRReadiness{Number: n, State: "open", SourceBranch: "feature/" + id, TargetBranch: "develop", HeadSHA: fmt.Sprintf("head-%d", n), Ready: true, SupportsSHAPin: true}
		specs = append(specs, releasePlanTaskService{TaskID: id, ServiceName: "api", Branch: "feature/" + id, RepoPath: filepath.Join(x.m.cfg.RootDir, "repo-api")})
	}
	seedReleasePlanTasks(t, x.m.cfg.TasksRoot, x.g, specs...)
	commonDir := x.g.commonDirFn
	x.g.commonDirFn = func(path string) (string, error) {
		if filepath.Base(path) == "api-integration" && x.failure == "common dir" {
			return "", errors.New("common directory lookup interrupted")
		}
		return commonDir(path)
	}
	x.g.removeWorktreeFn = func(dir, path string, force bool) error {
		if dir != filepath.Join(x.m.cfg.RootDir, "repo-api", ".git") || force {
			t.Fatalf("unsafe cleanup: dir=%s path=%s force=%v", dir, path, force)
		}
		if x.failure == "cleanup" {
			return errors.New("cleanup interrupted")
		}
		return nil
	}
	setTaskWorktreeHeads(x.g, map[string]string{
		filepath.Join(x.m.cfg.TasksRoot, "APP-1", "api"): "head-1",
		filepath.Join(x.m.cfg.TasksRoot, "APP-2", "api"): "head-2",
	})
	x.f.afterMerge = func(n int) {
		x.tip = fmt.Sprintf("d%d", n)
		mr := x.f.readiness[n]
		mr.State, mr.MergedSHA = "merged", x.tip
		x.f.readiness[n] = mr
	}
	x.params = CreateReleaseParams{TaskIDs: []string{"APP-1", "APP-2"}, ServiceVersions: map[string]string{"api": "1.2.3"}, StartImmediately: true}
	plan, err := x.m.PlanReleaseTaskMerges(t.Context(), x.params)
	if err != nil {
		t.Fatal(err)
	}
	x.params.ConfirmedTaskMergePlan = &plan
	return x
}

func (x *taskPrepareRecovery) reload() {
	x.m = New(x.m.cfg, x.g, x.m.discoverer, x.m.slnMgr, x.m.validator, x.m.flow, x.m.forgeClients, x.m.logger).(*manager)
}

func TestRetryRelease_TaskPrepareLifecycle(t *testing.T) {
	for _, stage := range []string{"branch", "push", "lost response", "common dir", "cleanup"} {
		t.Run(stage, func(t *testing.T) {
			// Given: actual creation merges both MRs and fails during preparation.
			x := newTaskPrepareRecovery(t)
			x.failure = stage
			failed, err := x.m.CreateRelease(t.Context(), x.params)
			if err == nil || failed.Status != domain.ReleaseStatusFailed || failed.Error == nil || !failed.Error.Recoverable {
				t.Fatalf("release=%+v err=%v", failed, err)
			}
			if failed.Services[0].PostIntegrationSHA != "d2" || failed.Services[0].IntegrationWorktreePath == "" {
				t.Fatalf("lost recovery evidence: %+v", failed.Services[0])
			}
			x.failure = ""
			x.reload()
			pushes := len(x.g.pushBranchExplicitCalls)
			// When
			got, err := x.m.RetryRelease(t.Context(), failed.ID)
			// Then
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != domain.ReleaseStatusPrepared || got.PreparedAt == nil || got.CompletedAt != nil || got.Error != nil || got.Services[0].Error != nil {
				t.Fatalf("recovery=%+v", got)
			}
			if got.Services[0].ReleaseSHA != "d2" || !reflect.DeepEqual(x.f.mergeNumbers, []int{1, 2}) || len(x.g.mergeFFOnlyCalls) != 0 {
				t.Fatalf("unsafe recovery: %+v merges=%v", got, x.f.mergeNumbers)
			}
			if stage == "lost response" && len(x.g.pushBranchExplicitCalls) != pushes {
				t.Fatal("matching remote pushed again")
			}
			stored, err := x.m.GetRelease(t.Context(), got.ID)
			if err != nil || stored.PreparedAt == nil || stored.Error != nil {
				t.Fatalf("stored=%+v err=%v", stored, err)
			}
		})
	}
}

func TestRetryReleaseTaskMerges_PrepareFailureRecovery(t *testing.T) {
	// Given: real creation stops after the first MR; dedicated retry merges the second.
	x := newTaskPrepareRecovery(t)
	after := x.f.afterMerge
	x.f.afterMerge = func(n int) {
		after(n)
		if n == 1 {
			mr := x.f.readiness[2]
			mr.Ready = false
			x.f.readiness[2] = mr
		}
	}
	partial, err := x.m.CreateRelease(t.Context(), x.params)
	if err == nil || partial.Status != domain.ReleaseStatusTaskMergePartial {
		t.Fatalf("release=%+v err=%v", partial, err)
	}
	mr := x.f.readiness[2]
	mr.Ready = true
	x.f.readiness[2] = mr
	x.reload()
	plan, err := x.m.PlanReleaseTaskMergeRetry(t.Context(), partial.ID)
	if err != nil {
		t.Fatal(err)
	}
	x.failure = "push"
	// When
	failed, err := x.m.RetryReleaseTaskMerges(t.Context(), partial.ID, &plan)
	// Then: failure is recoverable from disk through generic retry.
	if err == nil || failed.Status != domain.ReleaseStatusFailed {
		t.Fatalf("release=%+v err=%v", failed, err)
	}
	x.reload()
	stored, err := x.m.GetRelease(t.Context(), failed.ID)
	if err != nil || stored.Error == nil || !stored.Error.Recoverable || stored.CompletedAt == nil {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	if _, err := x.m.PlanReleaseTaskMergeRetry(t.Context(), failed.ID); !errors.Is(err, ErrReleaseInvalidStatusTransition) {
		t.Fatalf("failed preview: %v", err)
	}
	x.failure = ""
	got, err := x.m.RetryRelease(t.Context(), failed.ID)
	if err != nil || got.Status != domain.ReleaseStatusPrepared || got.CompletedAt != nil || got.Error != nil || got.Services[0].Error != nil || !reflect.DeepEqual(x.f.mergeNumbers, []int{1, 2}) {
		t.Fatalf("recovery=%+v merges=%v err=%v", got, x.f.mergeNumbers, err)
	}
}
