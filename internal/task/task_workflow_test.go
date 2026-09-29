package task

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

// newStrategyWorkflowManager builds a manager whose task has one service per
// branch entry (name -> branch), with git ancestry answers driven by isAncestor.
func newStrategyWorkflowManager(t *testing.T, flow *gitflow.ResolvedGitFlow, branches map[string]string, isAncestor func(repoPath, ancestor, descendant string) (bool, error), client *workflowForgeClient) (Manager, *mockGitClient) {
	t.Helper()
	rootDir := t.TempDir()
	tasksRoot := filepath.Join(rootDir, ".tasks")
	taskDir := filepath.Join(tasksRoot, "TASK-1")
	worktrees := make(map[string]git.WorktreeEntry, len(branches))
	names := make([]string, 0, len(branches))
	for name := range branches {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(taskDir, name)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		worktrees[filepath.Join(rootDir, "repos", name)] = git.WorktreeEntry{Path: path, Branch: "refs/heads/" + branches[name]}
	}
	gitMock := &mockGitClient{
		commonDirFn: func(path string) (string, error) {
			return filepath.Join(rootDir, "repos", filepath.Base(path), ".git"), nil
		},
		listWorktreesFn: func(repoPath string) ([]git.WorktreeEntry, error) {
			return []git.WorktreeEntry{worktrees[repoPath]}, nil
		},
		remoteURLRes: "git@gitlab.com:group/repo.git",
		isAncestorFn: isAncestor,
	}
	var clients map[forge.ForgeProvider]forge.ForgeClient
	if client != nil {
		clients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitLab: client}
	}
	cfg := newCloseTestConfig(rootDir, tasksRoot)
	mgr := newTestManagerWithDeps(t, cfg, gitMock, flow, clients)
	return mgr, gitMock
}

func defaultWorkflowFlow(t *testing.T) *gitflow.ResolvedGitFlow {
	t.Helper()
	flow, err := gitflow.EffectiveConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	return flow
}

func workflowStepLabels(summary domain.WorkflowSummary) []string {
	labels := make([]string, len(summary.Steps))
	for i, step := range summary.Steps {
		labels[i] = step.Label
	}
	return labels
}

func TestTaskWorkflow_DirectMergeHotfix_NoForgeNoMRSteps(t *testing.T) {
	flow := defaultWorkflowFlow(t)
	client := &workflowForgeClient{}
	var mu sync.Mutex
	var descendants []string
	mgr, _ := newStrategyWorkflowManager(t, flow, map[string]string{"a": "hotfix/1.0.1", "b": "hotfix/1.0.1"}, func(_, _, descendant string) (bool, error) {
		mu.Lock()
		descendants = append(descendants, descendant)
		mu.Unlock()
		return false, nil
	}, client)

	summary, err := mgr.TaskWorkflow(t.Context(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if reads := int(client.reads.Load()); reads != 0 {
		t.Fatalf("forge reads = %d, want 0 for direct_merge hotfix", reads)
	}
	if summary.Current != domain.TaskWorkflowMerge {
		t.Fatalf("Current = %q, want %q", summary.Current, domain.TaskWorkflowMerge)
	}
	if summary.NextAction != "press C to merge into master, develop" {
		t.Fatalf("NextAction = %q, want %q", summary.NextAction, "press C to merge into master, develop")
	}
	if labels := workflowStepLabels(summary); !reflect.DeepEqual(labels, []string{"code", "merge", "tag"}) {
		t.Fatalf("step labels = %v, want [code merge tag]", labels)
	}
	for _, label := range workflowStepLabels(summary) {
		if strings.Contains(label, "MR") || strings.Contains(label, "release") {
			t.Fatalf("direct hotfix step label %q must not suggest MR/release", label)
		}
	}
	for _, row := range summary.Services {
		if row.Status != "pending" || !strings.Contains(row.Detail, "origin/master") {
			t.Fatalf("row = %#v, want pending with origin/master detail", row)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(descendants) != 4 {
		t.Fatalf("ancestry checks = %d, want one per target per service", len(descendants))
	}
	for _, d := range descendants {
		if d != "origin/master" && d != "origin/develop" {
			t.Fatalf("descendant = %q, want origin/master or origin/develop", d)
		}
	}
}

func TestTaskWorkflow_DirectMergeHotfix_AllTargetsMerged_SuggestsTag(t *testing.T) {
	flow := defaultWorkflowFlow(t)
	client := &workflowForgeClient{}
	mgr, _ := newStrategyWorkflowManager(t, flow, map[string]string{"a": "hotfix/1.0.1"}, func(_, _, _ string) (bool, error) {
		return true, nil
	}, client)

	summary, err := mgr.TaskWorkflow(t.Context(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if reads := int(client.reads.Load()); reads != 0 {
		t.Fatalf("forge reads = %d, want 0", reads)
	}
	if summary.Current != domain.TaskWorkflowReleaseEligible {
		t.Fatalf("Current = %q, want %q", summary.Current, domain.TaskWorkflowReleaseEligible)
	}
	if summary.NextAction != "press C to create tag" {
		t.Fatalf("NextAction = %q, want %q", summary.NextAction, "press C to create tag")
	}
	if labels := workflowStepLabels(summary); !reflect.DeepEqual(labels, []string{"code", "merge", "tag"}) {
		t.Fatalf("step labels = %v, want [code merge tag]", labels)
	}
	states := map[domain.WorkflowPhase]string{}
	for _, step := range summary.Steps {
		states[step.Phase] = step.State
	}
	if states[domain.TaskWorkflowCode] != "done" || states[domain.TaskWorkflowMerge] != "done" || states[domain.TaskWorkflowReleaseEligible] != "now" {
		t.Fatalf("step states = %v, want code/merge done and tag now", states)
	}
	if len(summary.Services) != 1 || summary.Services[0].Status != "merged" {
		t.Fatalf("Services = %#v, want one merged row", summary.Services)
	}
}

func TestTaskWorkflow_ReviewHotfix_PerTargetRows(t *testing.T) {
	t.Run("missing develop MR offers creation", func(t *testing.T) {
		m, _, _ := hotfixManager(t)
		summary, err := m.TaskWorkflow(t.Context(), "H")
		if err != nil {
			t.Fatal(err)
		}
		if summary.Current != domain.TaskWorkflowMR || summary.NextAction != "press C to create missing hotfix MRs" {
			t.Fatalf("summary = %#v, want MR phase with creation next action", summary)
		}
		if len(summary.Services) != 2 {
			t.Fatalf("rows = %#v, want one row per review target", summary.Services)
		}
		byTarget := map[string]domain.ServiceWorkflow{}
		for _, row := range summary.Services {
			byTarget[strings.Split(row.Detail, ":")[0]] = row
		}
		if byTarget["master"].Status != "merged" {
			t.Fatalf("master row = %#v, want merged", byTarget["master"])
		}
		if byTarget["develop"].Status != "no_mr" {
			t.Fatalf("develop row = %#v, want no_mr", byTarget["develop"])
		}
	})

	t.Run("ready master with waiting develop offers merge", func(t *testing.T) {
		m, _, f := hotfixManager(t)
		f.requests[0].State = "open"
		f.requests[0].Ready = true
		f.requests = append(f.requests, forge.MRReadiness{Number: 2, State: "open", SourceBranch: "hotfix/H", TargetBranch: "develop", HeadSHA: "source", Blockers: []string{"not approved"}})
		summary, err := m.TaskWorkflow(t.Context(), "H")
		if err != nil {
			t.Fatal(err)
		}
		if summary.Current != domain.TaskWorkflowMerge || summary.NextAction != "Services → m → Merge MR" {
			t.Fatalf("summary = %#v, want merge phase", summary)
		}
	})
}

func TestTaskWorkflow_ReviewFeature_UsesEffectiveReviewTarget(t *testing.T) {
	flow := defaultWorkflowFlow(t)
	rule := flow.BranchTypes[gitflow.BranchTypeFeature]
	rule.ReviewTargets = []string{"staging"}
	rule.MergeTargets = []string{"staging"}
	flow.BranchTypes[gitflow.BranchTypeFeature] = rule

	t.Run("ancestry checked against review target", func(t *testing.T) {
		client := &workflowForgeClient{}
		var mu sync.Mutex
		var descendants []string
		mgr, gitMock := newStrategyWorkflowManager(t, flow, map[string]string{"a": "feature/x"}, func(_, _, descendant string) (bool, error) {
			mu.Lock()
			descendants = append(descendants, descendant)
			mu.Unlock()
			return false, nil
		}, client)
		gitMock.remoteBranchExistsRes = true

		summary, err := mgr.TaskWorkflow(t.Context(), "TASK-1")
		if err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(descendants) != 1 || descendants[0] != "origin/staging" {
			t.Fatalf("descendants = %v, want [origin/staging]", descendants)
		}
		if summary.Current != domain.TaskWorkflowMR {
			t.Fatalf("Current = %q, want %q", summary.Current, domain.TaskWorkflowMR)
		}
	})

	t.Run("merged into review target is release eligible", func(t *testing.T) {
		client := &workflowForgeClient{}
		mgr, _ := newStrategyWorkflowManager(t, flow, map[string]string{"a": "feature/x"}, func(_, _, _ string) (bool, error) {
			return true, nil
		}, client)
		summary, err := mgr.TaskWorkflow(t.Context(), "TASK-1")
		if err != nil {
			t.Fatal(err)
		}
		if summary.Current != domain.TaskWorkflowReleaseEligible || summary.NextAction != "select in release (N)" {
			t.Fatalf("summary = %#v, want release eligible", summary)
		}
		if reads := int(client.reads.Load()); reads != 0 {
			t.Fatalf("forge reads = %d, want 0 when merged", reads)
		}
	})
}

func TestTaskWorkflow_MixedReviewAndDirect_OnlyReviewUsesForge(t *testing.T) {
	flow := defaultWorkflowFlow(t)
	flow.AllowMixed = true
	client := &workflowForgeClient{readiness: map[string]forge.MRReadiness{
		"feature/x": {Number: 1, State: "open", Blockers: []string{"not approved"}},
	}}
	mgr, _ := newStrategyWorkflowManager(t, flow, map[string]string{"a": "feature/x", "h": "hotfix/1.0.1"}, func(_, _, _ string) (bool, error) {
		return false, nil
	}, client)

	summary, err := mgr.TaskWorkflow(t.Context(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if reads := int(client.reads.Load()); reads != 1 {
		t.Fatalf("forge reads = %d, want 1 (review service only)", reads)
	}
	if summary.Current != domain.TaskWorkflowMerge {
		t.Fatalf("Current = %q, want %q", summary.Current, domain.TaskWorkflowMerge)
	}
	if summary.NextAction != "press C to merge into master, develop" {
		t.Fatalf("NextAction = %q, want %q", summary.NextAction, "press C to merge into master, develop")
	}
	if len(summary.Services) != 2 {
		t.Fatalf("rows = %#v, want one row per service", summary.Services)
	}
	byName := map[string]domain.ServiceWorkflow{}
	for _, row := range summary.Services {
		byName[row.ServiceName] = row
	}
	if byName["a"].Status != "waiting" || byName["h"].Status != "pending" {
		t.Fatalf("rows = %#v, want a waiting and h pending", summary.Services)
	}
}

func TestTaskWorkflow_NoneStrategy_NoForgeNoAction(t *testing.T) {
	flow := defaultWorkflowFlow(t)
	flow.BranchTypes[gitflow.BranchType("chore")] = gitflow.BranchTypeRule{
		Prefixes:      []string{"chore/"},
		BaseBranch:    "develop",
		CloseStrategy: gitflow.CloseStrategyNone,
		MergeStrategy: gitflow.MergeStrategyMerge,
	}
	client := &workflowForgeClient{}
	mgr, _ := newStrategyWorkflowManager(t, flow, map[string]string{"a": "chore/x"}, func(_, _, _ string) (bool, error) {
		return false, nil
	}, client)

	summary, err := mgr.TaskWorkflow(t.Context(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if reads := int(client.reads.Load()); reads != 0 {
		t.Fatalf("forge reads = %d, want 0", reads)
	}
	if len(summary.Steps) != 0 {
		t.Fatalf("steps = %#v, want none for close_strategy none", summary.Steps)
	}
	if summary.NextAction != "no close action configured" {
		t.Fatalf("NextAction = %q, want %q", summary.NextAction, "no close action configured")
	}
	want := []domain.ServiceWorkflow{{ServiceName: "a", Status: "none", Detail: "no close action"}}
	if !reflect.DeepEqual(summary.Services, want) {
		t.Fatalf("Services = %#v, want %#v", summary.Services, want)
	}
}

func TestTaskWorkflow_ReviewFeature_RejectsWrongTargetReadyMR(t *testing.T) {
	flow := defaultWorkflowFlow(t)
	rule := flow.BranchTypes[gitflow.BranchTypeFeature]
	rule.ReviewTargets = []string{"staging"}
	rule.MergeTargets = []string{"staging"}
	flow.BranchTypes[gitflow.BranchTypeFeature] = rule

	t.Run("ready MR against wrong target is blocked", func(t *testing.T) {
		client := &workflowForgeClient{readiness: map[string]forge.MRReadiness{
			"feature/x": {Number: 1, State: "open", Ready: true, SourceBranch: "feature/x", TargetBranch: "develop"},
		}}
		mgr, _ := newStrategyWorkflowManager(t, flow, map[string]string{"a": "feature/x"}, func(_, _, _ string) (bool, error) {
			return false, nil
		}, client)

		summary, err := mgr.TaskWorkflow(t.Context(), "TASK-1")
		if err != nil {
			t.Fatal(err)
		}
		if summary.Current != domain.TaskWorkflowReviewCI || summary.NextAction != "fix blockers, then M" {
			t.Fatalf("summary = %#v, want blocked review phase without merge guidance", summary)
		}
		if !strings.Contains(summary.Blocker, "develop") || !strings.Contains(summary.Blocker, "staging") {
			t.Fatalf("Blocker = %q, want target mismatch detail", summary.Blocker)
		}
		if strings.Contains(summary.NextAction, "press M") {
			t.Fatalf("NextAction = %q, must not offer merge for wrong-target MR", summary.NextAction)
		}
	})

	t.Run("ready MR from wrong source is blocked", func(t *testing.T) {
		client := &workflowForgeClient{readiness: map[string]forge.MRReadiness{
			"feature/x": {Number: 1, State: "open", Ready: true, SourceBranch: "feature/other", TargetBranch: "staging"},
		}}
		mgr, _ := newStrategyWorkflowManager(t, flow, map[string]string{"a": "feature/x"}, func(_, _, _ string) (bool, error) {
			return false, nil
		}, client)

		summary, err := mgr.TaskWorkflow(t.Context(), "TASK-1")
		if err != nil {
			t.Fatal(err)
		}
		if summary.Current != domain.TaskWorkflowReviewCI || summary.NextAction != "fix blockers, then M" {
			t.Fatalf("summary = %#v, want blocked review phase", summary)
		}
		if !strings.Contains(summary.Blocker, "feature/other") {
			t.Fatalf("Blocker = %q, want source mismatch detail", summary.Blocker)
		}
	})

	t.Run("ready MR matching source and target still merges", func(t *testing.T) {
		client := &workflowForgeClient{readiness: map[string]forge.MRReadiness{
			"feature/x": {Number: 1, State: "open", Ready: true, SourceBranch: "feature/x", TargetBranch: "staging"},
		}}
		mgr, _ := newStrategyWorkflowManager(t, flow, map[string]string{"a": "feature/x"}, func(_, _, _ string) (bool, error) {
			return false, nil
		}, client)

		summary, err := mgr.TaskWorkflow(t.Context(), "TASK-1")
		if err != nil {
			t.Fatal(err)
		}
		if summary.Current != domain.TaskWorkflowMerge || summary.NextAction != "press M to merge ready MRs" {
			t.Fatalf("summary = %#v, want merge phase", summary)
		}
	})
}

func TestTaskWorkflow_NoneStrategy_WithPostActions_GuidesClose(t *testing.T) {
	tests := []struct {
		name     string
		rule     func(gitflow.BranchTypeRule) gitflow.BranchTypeRule
		wantNext string
		wantRow  domain.ServiceWorkflow
	}{
		{
			name: "tag on close",
			rule: func(r gitflow.BranchTypeRule) gitflow.BranchTypeRule {
				r.TagOnClose = true
				r.TagSource = "master"
				return r
			},
			wantNext: "press C to create tag",
			wantRow:  domain.ServiceWorkflow{ServiceName: "a", Status: "tag", Detail: "create tag"},
		},
		{
			name: "pipeline on close",
			rule: func(r gitflow.BranchTypeRule) gitflow.BranchTypeRule {
				r.TriggerPipelineOnClose = true
				return r
			},
			wantNext: "press C to trigger pipeline",
			wantRow:  domain.ServiceWorkflow{ServiceName: "a", Status: "pipeline", Detail: "trigger pipeline"},
		},
		{
			name: "tag and pipeline on close",
			rule: func(r gitflow.BranchTypeRule) gitflow.BranchTypeRule {
				r.TagOnClose = true
				r.TagSource = "master"
				r.TriggerPipelineOnClose = true
				return r
			},
			wantNext: "press C to tag and trigger pipeline",
			wantRow:  domain.ServiceWorkflow{ServiceName: "a", Status: "tag+pipeline", Detail: "tag and trigger pipeline"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flow := defaultWorkflowFlow(t)
			flow.BranchTypes[gitflow.BranchType("chore")] = tt.rule(gitflow.BranchTypeRule{
				Prefixes:      []string{"chore/"},
				BaseBranch:    "develop",
				CloseStrategy: gitflow.CloseStrategyNone,
				MergeStrategy: gitflow.MergeStrategyMerge,
			})
			client := &workflowForgeClient{}
			mgr, _ := newStrategyWorkflowManager(t, flow, map[string]string{"a": "chore/x"}, func(_, _, _ string) (bool, error) {
				return false, nil
			}, client)

			summary, err := mgr.TaskWorkflow(t.Context(), "TASK-1")
			if err != nil {
				t.Fatal(err)
			}
			if reads := int(client.reads.Load()); reads != 0 {
				t.Fatalf("forge reads = %d, want 0", reads)
			}
			if summary.NextAction != tt.wantNext {
				t.Fatalf("NextAction = %q, want %q", summary.NextAction, tt.wantNext)
			}
			if !reflect.DeepEqual(summary.Services, []domain.ServiceWorkflow{tt.wantRow}) {
				t.Fatalf("Services = %#v, want %#v", summary.Services, tt.wantRow)
			}
		})
	}
}

func TestTaskWorkflow_MixedNoneWithPostActionAndDirect_RowMatchesGuidance(t *testing.T) {
	flow := defaultWorkflowFlow(t)
	flow.AllowMixed = true
	choreRule := gitflow.BranchTypeRule{
		Prefixes:      []string{"chore/"},
		BaseBranch:    "develop",
		CloseStrategy: gitflow.CloseStrategyNone,
		MergeStrategy: gitflow.MergeStrategyMerge,
		TagOnClose:    true,
		TagSource:     "master",
	}
	flow.BranchTypes[gitflow.BranchType("chore")] = choreRule
	client := &workflowForgeClient{}
	mgr, _ := newStrategyWorkflowManager(t, flow, map[string]string{"c": "chore/x", "h": "hotfix/1.0.1"}, func(_, _, _ string) (bool, error) {
		return false, nil
	}, client)

	summary, err := mgr.TaskWorkflow(t.Context(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if summary.NextAction != "press C to merge into master, develop" {
		t.Fatalf("NextAction = %q, want direct-merge guidance", summary.NextAction)
	}
	want := []domain.ServiceWorkflow{
		{ServiceName: "c", Status: "tag", Detail: "create tag"},
		{ServiceName: "h", Status: "pending", Detail: "pending: origin/master, origin/develop"},
	}
	if !reflect.DeepEqual(summary.Services, want) {
		t.Fatalf("Services = %#v, want %#v", summary.Services, want)
	}
}

func TestTaskWorkflow_DirectMergeMerged_WithPostActions_GuidesClose(t *testing.T) {
	t.Run("hotfix merged with pipeline offers combined close", func(t *testing.T) {
		flow := defaultWorkflowFlow(t)
		rule := flow.BranchTypes[gitflow.BranchTypeHotfix]
		rule.TriggerPipelineOnClose = true
		flow.BranchTypes[gitflow.BranchTypeHotfix] = rule
		client := &workflowForgeClient{}
		mgr, _ := newStrategyWorkflowManager(t, flow, map[string]string{"a": "hotfix/1.0.1"}, func(_, _, _ string) (bool, error) {
			return true, nil
		}, client)

		summary, err := mgr.TaskWorkflow(t.Context(), "TASK-1")
		if err != nil {
			t.Fatal(err)
		}
		if summary.NextAction != "press C to tag and trigger pipeline" {
			t.Fatalf("NextAction = %q, want %q", summary.NextAction, "press C to tag and trigger pipeline")
		}
		if summary.Current != domain.TaskWorkflowReleaseEligible {
			t.Fatalf("Current = %q, want %q", summary.Current, domain.TaskWorkflowReleaseEligible)
		}
	})

	t.Run("direct feature without post actions is neutral", func(t *testing.T) {
		flow := defaultWorkflowFlow(t)
		rule := flow.BranchTypes[gitflow.BranchTypeFeature]
		rule.CloseStrategy = gitflow.CloseStrategyDirectMerge
		flow.BranchTypes[gitflow.BranchTypeFeature] = rule
		client := &workflowForgeClient{}
		mgr, _ := newStrategyWorkflowManager(t, flow, map[string]string{"a": "feature/x"}, func(_, _, _ string) (bool, error) {
			return true, nil
		}, client)

		summary, err := mgr.TaskWorkflow(t.Context(), "TASK-1")
		if err != nil {
			t.Fatal(err)
		}
		if summary.NextAction != "" {
			t.Fatalf("NextAction = %q, want neutral empty action", summary.NextAction)
		}
		if reads := int(client.reads.Load()); reads != 0 {
			t.Fatalf("forge reads = %d, want 0", reads)
		}
	})
}

func TestTaskWorkflow_MixedDirectPendingAndMissingMR_IncludesBothCloseActions(t *testing.T) {
	flow := defaultWorkflowFlow(t)
	flow.AllowMixed = true
	client := &workflowForgeClient{}
	mgr, _ := newStrategyWorkflowManager(t, flow, map[string]string{"a": "feature/x", "h": "hotfix/1.0.1"}, func(_, _, _ string) (bool, error) {
		return false, nil
	}, client)

	summary, err := mgr.TaskWorkflow(t.Context(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Current != domain.TaskWorkflowMerge {
		t.Fatalf("Current = %q, want %q", summary.Current, domain.TaskWorkflowMerge)
	}
	if !strings.Contains(summary.NextAction, "press C to merge into master, develop") {
		t.Fatalf("NextAction = %q, want direct-merge C action", summary.NextAction)
	}
	if !strings.Contains(summary.NextAction, "press C to create MRs") {
		t.Fatalf("NextAction = %q, want missing-MR C action", summary.NextAction)
	}
	if reads := int(client.reads.Load()); reads != 1 {
		t.Fatalf("forge reads = %d, want 1 (review service only)", reads)
	}
}

func TestTaskWorkflow_DirectMergeHotfix_ActiveReleaseWarningSurfaced(t *testing.T) {
	flow := defaultWorkflowFlow(t)
	client := &workflowForgeClient{}
	mgr, gitMock := newStrategyWorkflowManager(t, flow, map[string]string{"a": "hotfix/1.0.1"}, func(_, _, _ string) (bool, error) {
		return false, nil
	}, client)
	gitMock.listBranchesFn = func(string, string) ([]string, error) {
		return nil, errors.New("git branch --list failed")
	}

	summary, err := mgr.TaskWorkflow(t.Context(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Services) != 1 || !strings.Contains(summary.Services[0].Detail, "active release detection failed") {
		t.Fatalf("Services = %#v, want active-release warning in row detail", summary.Services)
	}
}
