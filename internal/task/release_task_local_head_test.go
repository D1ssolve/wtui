package task

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
)

func TestPlanReleaseTaskMerges_LocalHead(t *testing.T) {
	for _, tc := range []struct {
		name  string
		head  string
		err   error
		ready bool
	}{
		{name: "matching", head: "remote-head", ready: true},
		{name: "local ahead stale remote", head: "local-ahead"},
		{name: "empty"},
		{name: "whitespace", head: "  "},
		{name: "unresolvable", err: errors.New("missing HEAD")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			m, g, _, params := localHeadTestManager(t)
			worktree := filepath.Join(m.cfg.TasksRoot, "APP-1", "api")
			g.resolveRefFn = func(path, ref string) (string, error) {
				if path == worktree && ref == "HEAD" {
					return tc.head, tc.err
				}
				return "target-head", nil
			}

			// When
			plan, err := m.PlanReleaseTaskMerges(t.Context(), params)

			// Then
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Rows) != 1 || plan.Rows[0].Ready != tc.ready || allReleaseTaskMergeRowsReady(&plan) != tc.ready {
				t.Fatalf("plan = %+v, want ready=%t", plan, tc.ready)
			}
			if !tc.ready && !strings.Contains(strings.Join(plan.Rows[0].Blockers, " "), "HEAD") {
				t.Fatalf("missing local HEAD blocker: %+v", plan.Rows[0])
			}
			found := false
			for _, call := range g.resolveRefCalls {
				if call.RepoPath == worktree && call.Ref == "HEAD" {
					found = true
				}
			}
			if !found {
				t.Fatal("did not resolve HEAD in task worktree")
			}
		})
	}
}

func TestCreateRelease_LocalHeadDriftRejectedBeforeMutation(t *testing.T) {
	for _, tc := range []struct {
		name string
		head string
		err  error
	}{
		{name: "drift", head: "new-local-commit"},
		{name: "empty"},
		{name: "unresolvable", err: errors.New("missing HEAD")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Given
			m, g, f, params := localHeadTestManager(t)
			plan, err := m.PlanReleaseTaskMerges(t.Context(), params)
			if err != nil || !allReleaseTaskMergeRowsReady(&plan) {
				t.Fatalf("preview = %+v, err=%v", plan, err)
			}
			g.resolveRefFn = func(_, ref string) (string, error) {
				if ref == "HEAD" {
					return tc.head, tc.err
				}
				return "target-head", nil
			}
			params.ConfirmedTaskMergePlan = &plan
			params.StartImmediately = true

			// When
			_, err = m.CreateRelease(t.Context(), params)

			// Then
			if err == nil || !strings.Contains(err.Error(), "HEAD") {
				t.Errorf("error = %v, want local HEAD rejection", err)
			}
			if f.mergeCalls != 0 || len(g.createBranchFromBranchCalls) != 0 {
				t.Errorf("mutations: merges=%d branches=%v", f.mergeCalls, g.createBranchFromBranchCalls)
			}
			releases, err := m.listReleaseManifests()
			if err != nil || len(releases) != 0 {
				t.Fatalf("manifests = %+v, err=%v, want none", releases, err)
			}
		})
	}
}

func localHeadTestManager(t *testing.T) (*manager, *mockGitClient, *releaseTaskMergeForge, CreateReleaseParams) {
	t.Helper()
	g := &mockGitClient{remoteURLRes: "git@github.com:org/repo.git"}
	m, _ := newReleasePlanTestManager(t, g)
	enableReleasePrepareTaskMerge(t, m)
	f := newReleaseTaskMergeForge()
	f.readiness[1] = forge.MRReadiness{Number: 1, State: "open", SourceBranch: "feature/APP-1", TargetBranch: "develop", HeadSHA: "remote-head", Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitHub: f}
	seedReleasePlanTasks(t, m.cfg.TasksRoot, g, releasePlanTaskService{TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-api")})
	g.resolveRefFn = func(path, ref string) (string, error) {
		if path == filepath.Join(m.cfg.TasksRoot, "APP-1", "api") && ref == "HEAD" {
			return "remote-head", nil
		}
		return "target-head", nil
	}
	return m, g, f, CreateReleaseParams{TaskIDs: []string{"APP-1"}, ServiceVersions: map[string]string{"api": "1.2.3"}}
}

func setTaskWorktreeHeads(g *mockGitClient, heads map[string]string) {
	resolve := g.resolveRefFn
	g.resolveRefFn = func(path, ref string) (string, error) {
		if head, ok := heads[path]; ok && ref == "HEAD" {
			return head, nil
		}
		if resolve != nil {
			return resolve(path, ref)
		}
		return ref + "-sha", nil
	}
}

func TestPlanReleaseTaskMergeRetry_LocalHeadMismatchBlocks(t *testing.T) {
	for _, state := range []string{"open", "merged"} {
		t.Run(state, func(t *testing.T) {
			// Given
			m, g, f, _ := localHeadTestManager(t)
			worktree := filepath.Join(m.cfg.TasksRoot, "APP-1", "api")
			setTaskWorktreeHeads(g, map[string]string{worktree: "local-ahead"})
			mr := f.readiness[1]
			mr.State, mr.MergedSHA = state, "target-head"
			f.readiness[1] = mr
			release := writeTaskMergeRetryRelease(t, m, domain.ReleaseStatusTaskMergePartial, domain.ReleaseFeatureBranch{
				TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", WorktreePath: worktree,
				TaskMergeStatus: taskMergeStatusPending, TaskMergeMRNumber: 1, TaskMergeHeadSHA: "remote-head", TaskMergeTargetSHA: "target-head",
			})

			// When
			plan, err := m.PlanReleaseTaskMergeRetry(t.Context(), release.ID)

			// Then
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Rows) != 1 || plan.Rows[0].Ready || allReleaseTaskMergeRowsReady(&plan) || !strings.Contains(strings.Join(plan.Rows[0].Blockers, " "), "HEAD") {
				t.Fatalf("retry must block local HEAD mismatch: %+v", plan)
			}
		})
	}
}
