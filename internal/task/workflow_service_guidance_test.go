package task

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func TestTaskWorkflow_ServiceGuidance_ReviewRows(t *testing.T) {
	t.Run("ready service points at merge", func(t *testing.T) {
		mgr, _ := newWorkflowTestManager(t, map[string]forge.MRReadiness{
			"feature/a": {Number: 1, State: "open", Ready: true},
		}, false, true)
		summary := mustTaskWorkflow(t, mgr)
		row := findServiceRow(t, summary.Services, "a")
		if row.Current != domain.TaskWorkflowMerge || row.NextAction != "merge ready MRs in forge, then press M to reconcile" {
			t.Fatalf("guidance = current %q next %q, want merge guidance", row.Current, row.NextAction)
		}
		if row.Blocker != "" {
			t.Fatalf("Blocker = %q, want empty", row.Blocker)
		}
	})
	t.Run("waiting service points at review CI", func(t *testing.T) {
		mgr, _ := newWorkflowTestManager(t, map[string]forge.MRReadiness{
			"feature/a": {Number: 1, State: "open", Blockers: []string{"checks pending"}},
			"feature/b": {Number: 2, State: "open", Blockers: []string{"checks pending"}},
		}, false, true)
		summary := mustTaskWorkflow(t, mgr)
		for _, name := range []string{"a", "b"} {
			row := findServiceRow(t, summary.Services, name)
			if row.Current != domain.TaskWorkflowReviewCI || row.NextAction != "wait for review/CI, then merge in forge and press M to reconcile" {
				t.Fatalf("%s guidance = current %q next %q, want review CI guidance", name, row.Current, row.NextAction)
			}
		}
	})
	t.Run("blocked service carries blocker", func(t *testing.T) {
		mgr, _ := newWorkflowTestManager(t, map[string]forge.MRReadiness{
			"feature/a": {Number: 1, State: "open", Blockers: []string{"merge blocked: need rebase"}},
			"feature/b": {Number: 2, State: "open", Blockers: []string{"checks pending"}},
		}, false, true)
		summary := mustTaskWorkflow(t, mgr)
		row := findServiceRow(t, summary.Services, "a")
		if row.Current != domain.TaskWorkflowReviewCI {
			t.Fatalf("Current = %q, want %q", row.Current, domain.TaskWorkflowReviewCI)
		}
		if row.Blocker != "merge blocked: need rebase" {
			t.Fatalf("Blocker = %q, want rebase blocker", row.Blocker)
		}
		if row.NextAction != "" {
			t.Fatalf("NextAction = %q, want empty for blocked service", row.NextAction)
		}
	})
	t.Run("missing MR service points at MR creation", func(t *testing.T) {
		mgr, _ := newWorkflowTestManager(t, map[string]forge.MRReadiness{}, false, true)
		summary := mustTaskWorkflow(t, mgr)
		for _, row := range summary.Services {
			if row.Current != domain.TaskWorkflowMR || row.NextAction != "press C to create MRs" {
				t.Fatalf("%s guidance = current %q next %q, want MR creation guidance", row.ServiceName, row.Current, row.NextAction)
			}
		}
	})
}

func TestTaskWorkflow_ServiceGuidance_DirectMerge(t *testing.T) {
	rule := gitflow.BranchTypeRule{
		Prefixes:      []string{"feature/"},
		CloseStrategy: gitflow.CloseStrategyDirectMerge,
		MergeTargets:  []string{"develop"},
	}
	t.Run("pending target points at merge", func(t *testing.T) {
		mgr := newRuleWorkflowTestManager(t, rule, false)
		summary := mustTaskWorkflow(t, mgr)
		if summary.Current != domain.TaskWorkflowMerge {
			t.Fatalf("Current = %q, want merge", summary.Current)
		}
		for _, row := range summary.Services {
			if row.Status != "pending" {
				t.Fatalf("Status = %q, want pending", row.Status)
			}
			if row.Current != domain.TaskWorkflowMerge || row.NextAction != "press C to merge into develop" {
				t.Fatalf("%s guidance = current %q next %q, want direct merge guidance", row.ServiceName, row.Current, row.NextAction)
			}
		}
	})

	postActionTests := []struct {
		name      string
		mutate    func(*gitflow.BranchTypeRule)
		wantNext  string
		wantPhase domain.WorkflowPhase
	}{
		{name: "tag on close", mutate: func(r *gitflow.BranchTypeRule) { r.TagOnClose = true }, wantNext: "press C to create tag", wantPhase: domain.TaskWorkflowReleaseEligible},
		{name: "pipeline on close", mutate: func(r *gitflow.BranchTypeRule) { r.TriggerPipelineOnClose = true }, wantNext: "press C to trigger pipeline", wantPhase: domain.TaskWorkflowReleaseEligible},
		{name: "tag and pipeline on close", mutate: func(r *gitflow.BranchTypeRule) { r.TagOnClose = true; r.TriggerPipelineOnClose = true }, wantNext: "press C to tag and trigger pipeline", wantPhase: domain.TaskWorkflowReleaseEligible},
		{name: "no post action", mutate: func(r *gitflow.BranchTypeRule) {}, wantNext: "", wantPhase: domain.TaskWorkflowMerge},
	}
	for _, tt := range postActionTests {
		t.Run("merged service with "+tt.name, func(t *testing.T) {
			mergedRule := rule
			tt.mutate(&mergedRule)
			mgr := newRuleWorkflowTestManager(t, mergedRule, true)
			summary := mustTaskWorkflow(t, mgr)
			for _, row := range summary.Services {
				if row.Status != "merged" {
					t.Fatalf("Status = %q, want merged", row.Status)
				}
				if row.Current != tt.wantPhase || row.NextAction != tt.wantNext {
					t.Fatalf("%s guidance = current %q next %q, want current %q next %q", row.ServiceName, row.Current, row.NextAction, tt.wantPhase, tt.wantNext)
				}
			}
		})
	}
}

func TestTaskWorkflow_ServiceGuidance_NoCloseAction(t *testing.T) {
	t.Run("no post actions", func(t *testing.T) {
		mgr := newRuleWorkflowTestManager(t, gitflow.BranchTypeRule{
			Prefixes:      []string{"feature/"},
			CloseStrategy: gitflow.CloseStrategyNone,
		}, false)
		summary := mustTaskWorkflow(t, mgr)
		if summary.NextAction != "no close action configured" {
			t.Fatalf("NextAction = %q, want %q", summary.NextAction, "no close action configured")
		}
		for _, row := range summary.Services {
			if row.Status != "none" || row.Detail != "no close action" {
				t.Fatalf("row = %#v, want none status and detail", row)
			}
			if row.Current != domain.TaskWorkflowReleaseEligible || row.NextAction != "" {
				t.Fatalf("guidance = current %q next %q, want release-eligible with no action", row.Current, row.NextAction)
			}
		}
	})
	t.Run("tag and pipeline post actions", func(t *testing.T) {
		mgr := newRuleWorkflowTestManager(t, gitflow.BranchTypeRule{
			Prefixes:               []string{"feature/"},
			CloseStrategy:          gitflow.CloseStrategyNone,
			TagOnClose:             true,
			TriggerPipelineOnClose: true,
		}, false)
		summary := mustTaskWorkflow(t, mgr)
		for _, row := range summary.Services {
			if row.Status != "tag+pipeline" {
				t.Fatalf("Status = %q, want tag+pipeline", row.Status)
			}
			if row.Current != domain.TaskWorkflowReleaseEligible || row.NextAction != "press C to tag and trigger pipeline" {
				t.Fatalf("guidance = current %q next %q, want combined post action", row.Current, row.NextAction)
			}
		}
	})
}

func TestTaskWorkflow_ServiceGuidance_HotfixDuplicateRowsPerTarget(t *testing.T) {
	m, _, f := hotfixManager(t)
	f.requests = append(f.requests, forge.MRReadiness{Number: 2, State: "merged", SourceBranch: "hotfix/H", TargetBranch: "develop", HeadSHA: "source", MergedSHA: "develop-merge"})

	summary, err := m.TaskWorkflow(t.Context(), "H")
	if err != nil {
		t.Fatalf("TaskWorkflow() err = %v", err)
	}
	if len(summary.Services) != 2 {
		t.Fatalf("Services = %#v, want one row per hotfix target", summary.Services)
	}
	details := map[string]bool{}
	for _, row := range summary.Services {
		if row.ServiceName != "svc" {
			t.Fatalf("ServiceName = %q, want svc on every hotfix row", row.ServiceName)
		}
		if row.Status != "merged" {
			t.Fatalf("Status = %q, want merged", row.Status)
		}
		if row.Current != domain.TaskWorkflowReleaseEligible {
			t.Fatalf("Current = %q, want %q on merged hotfix row", row.Current, domain.TaskWorkflowReleaseEligible)
		}
		details[row.Detail] = true
	}
	if !details["master: "] || !details["develop: "] {
		t.Fatalf("Details = %v, want distinct per-target details", details)
	}
}

func TestReleaseWorkflow_ServiceGuidance(t *testing.T) {
	release := domain.Release{Status: domain.ReleaseStatusMasterMerged, Services: []domain.ReleaseService{
		{Name: "api", Status: domain.ReleaseStatusAwaitingMasterMerge, ProductionMR: &domain.ProductionMRRef{Number: 42, State: "open"}},
		{Name: "worker", Status: domain.ReleaseStatusFailed, Error: &domain.ReleaseError{Message: "CI failed"}},
		{Name: "web", Status: domain.ReleaseStatusBranching},
	}}

	summary := ReleaseWorkflow(release)
	api := findServiceRow(t, summary.Services, "api")
	if api.Current != domain.ReleaseWorkflowMasterMR || api.NextAction != "merge ready MRs in forge, then press M to reconcile" {
		t.Fatalf("api guidance = current %q next %q, want master MR guidance", api.Current, api.NextAction)
	}
	worker := findServiceRow(t, summary.Services, "worker")
	if worker.Current != domain.ReleaseWorkflowDevelop || worker.NextAction != "release failed" || worker.Blocker != "CI failed" {
		t.Fatalf("worker guidance = current %q next %q blocker %q, want failed guidance", worker.Current, worker.NextAction, worker.Blocker)
	}
	web := findServiceRow(t, summary.Services, "web")
	if web.Current != domain.ReleaseWorkflowReleaseBranch || web.NextAction != "creating release branches" {
		t.Fatalf("web guidance = current %q next %q, want release branch guidance", web.Current, web.NextAction)
	}
	for _, row := range summary.Services {
		if row.Detail == "" && row.ServiceName != "web" {
			t.Fatalf("%s Detail = %q, want existing detail preserved", row.ServiceName, row.Detail)
		}
	}
}

func mustTaskWorkflow(t *testing.T, mgr Manager) domain.WorkflowSummary {
	t.Helper()
	summary, err := mgr.TaskWorkflow(t.Context(), "TASK-1")
	if err != nil {
		t.Fatalf("TaskWorkflow() err = %v", err)
	}
	return summary
}

func findServiceRow(t *testing.T, rows []domain.ServiceWorkflow, name string) domain.ServiceWorkflow {
	t.Helper()
	for _, row := range rows {
		if row.ServiceName == name {
			return row
		}
	}
	t.Fatalf("service row %q not found in %#v", name, rows)
	return domain.ServiceWorkflow{}
}

// newRuleWorkflowTestManager builds a two-service task manager whose feature
// branches resolve to the given rule, with every close target either merged or
// not yet merged depending on merged.
func newRuleWorkflowTestManager(t *testing.T, rule gitflow.BranchTypeRule, merged bool) Manager {
	t.Helper()
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskDir := filepath.Join(tasksRoot, "TASK-1")
	worktrees := make(map[string]git.WorktreeEntry, 2)
	for _, name := range []string{"a", "b"} {
		path := filepath.Join(taskDir, name)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		worktrees[filepath.Join(rootDir, "repos", name)] = git.WorktreeEntry{Path: path, Branch: "refs/heads/feature/" + name}
	}
	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			return filepath.Join(rootDir, "repos", filepath.Base(path), ".git"), nil
		},
		listWorktreesFn: func(repoPath string) ([]git.WorktreeEntry, error) {
			return []git.WorktreeEntry{worktrees[repoPath]}, nil
		},
		remoteURLRes:          "git@gitlab.com:group/repo.git",
		remoteBranchExistsRes: true,
		isAncestorFn: func(_, _, _ string) (bool, error) {
			return merged, nil
		},
	}
	flow := &gitflow.ResolvedGitFlow{
		IntegrationBranch: "develop",
		BranchTypes:       map[gitflow.BranchType]gitflow.BranchTypeRule{gitflow.BranchTypeFeature: rule},
	}
	return newTestManagerWithDeps(t, newCloseTestConfig(rootDir, tasksRoot), gitMock, flow, nil)
}
