//go:build integration

package task

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/discovery"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/validation"
)

func serviceRulesGitEnv(t *testing.T) (*manager, string, map[string]string) {
	t.Helper()
	t.Setenv("GIT_ALLOW_PROTOCOL", "file")
	root := resolvedPath(t, t.TempDir())
	taskRoot := filepath.Join(root, ".tasks")
	path := filepath.Join(root, "config.yaml")
	writeFile(t, path, `workflow:
  extends: git-flow
  branches: {production: main}
  task_types:
    feature: {prefixes: [feature/], from: integration, close: [{merge: {into: integration}}]}
  services:
    api: {task_types: {feature: {from: production, close: [{merge: {into: production, merge: ff_only}}]}}}
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RootDir, cfg.TasksRoot = root, taskRoot
	if _, err = cfg.Effective(); err != nil {
		t.Fatal(err)
	}
	flow, err := gitflow.Resolve(cfg)
	if err != nil {
		t.Fatal(err)
	}
	remotes := map[string]string{}
	for _, name := range []string{"api", "web"} {
		remote := filepath.Join(t.TempDir(), name+".git")
		remotes[name] = remote
		repo := filepath.Join(root, name)
		mustGit(t, root, "init", "--bare", "-b", "main", remote)
		mustGit(t, root, "clone", remote, repo)
		mustGit(t, repo, "config", "user.email", "test@example.com")
		mustGit(t, repo, "config", "user.name", "Test")
		mustGit(t, repo, "commit", "--allow-empty", "-m", "main tip")
		mustGit(t, repo, "checkout", "-b", "develop")
		mustGit(t, repo, "commit", "--allow-empty", "-m", "develop tip")
		mustGit(t, repo, "push", "origin", "main", "develop")
		mustGit(t, repo, "checkout", "--detach")
	}
	if err := os.MkdirAll(taskRoot, 0755); err != nil {
		t.Fatal(err)
	}
	logger := newIntegrationLogger()
	g := git.NewCommandClient(logger)
	m := New(cfg, g, discovery.New(cfg, g, logger), &recordingSlnGenerator{}, validation.NewTaskValidator(g), flow, nil, logger)
	return m.(*manager), root, remotes
}

func TestServiceRules_InitPlanAndClose(t *testing.T) {
	m, _, remotes := serviceRulesGitEnv(t)
	taskRoot := m.cfg.TasksRoot
	targets := map[string]string{"api": "main", "web": "develop"}
	ctx := t.Context()
	const id = "T-19"
	if _, err := m.Init(ctx, InitParams{TaskID: id, Services: []string{"api", "web"}}); err != nil {
		t.Fatal(err)
	}
	for name, target := range targets {
		wt := filepath.Join(taskRoot, id, name)
		if got, want := gitOutput(t, wt, "rev-parse", "HEAD"), gitOutput(t, wt, "rev-parse", "origin/"+target); got != want {
			t.Fatalf("%s initialized at %s, want %s (%s)", name, got, target, want)
		}
		writeFile(t, filepath.Join(wt, "work"), name)
		mustGit(t, wt, "add", "work")
		mustGit(t, wt, "commit", "-m", "task change")
	}
	plan, err := m.PlanCloseTask(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Services) != 2 {
		t.Fatal(plan)
	}
	for _, svc := range plan.Services {
		if !slices.Equal(svc.TargetBranches, []string{targets[svc.ServiceName]}) {
			t.Fatalf("wrong preview: %+v", svc)
		}
	}
	// A change to web's inherited target must not reuse approval for api + web.
	old := m.flow.BranchTypes[gitflow.BranchTypeFeature]
	drift := old
	drift.MergeTargets = []string{"main"}
	m.flow.BranchTypes[gitflow.BranchTypeFeature] = drift
	before := map[string]string{}
	for name, remote := range remotes {
		before[name] = gitOutput(t, remote, "show-ref", "--heads")
	}
	if _, err := m.CloseTask(ctx, CloseTaskParams{TaskID: id, Fingerprint: plan.Fingerprint}); err == nil {
		t.Fatal("stale close accepted")
	}
	for name, remote := range remotes {
		if got := gitOutput(t, remote, "show-ref", "--heads"); got != before[name] {
			t.Fatalf("%s bare refs changed after refusal: %s", name, got)
		}
	}
	m.flow.BranchTypes[gitflow.BranchTypeFeature] = old
	result, err := m.CloseTask(ctx, CloseTaskParams{TaskID: id, Fingerprint: plan.Fingerprint})
	if err != nil || !result.Success {
		t.Fatalf("close: %+v, %v", result, err)
	}
	for name, target := range targets {
		if got := gitOutput(t, remotes[name], "show", target+":work"); got != name {
			t.Fatalf("%s %s: %q", name, target, got)
		}
	}
}

func TestServiceRules_DivergedFFOnlyLeavesBareRefsUnchanged(t *testing.T) {
	m, root, remotes := serviceRulesGitEnv(t)
	ctx := t.Context()
	const id = "T-FF"
	if _, err := m.Init(ctx, InitParams{TaskID: id, Services: []string{"api", "web"}}); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "api")
	wt := filepath.Join(m.cfg.TasksRoot, id, "api")
	mustGit(t, wt, "commit", "--allow-empty", "-m", "task")
	mustGit(t, repo, "checkout", "main")
	mustGit(t, repo, "commit", "--allow-empty", "-m", "independent")
	mustGit(t, repo, "push", "origin", "main")
	mustGit(t, repo, "checkout", "--detach")
	before := gitOutput(t, remotes["api"], "show-ref", "--heads")
	plan, err := m.PlanCloseTask(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.CloseTask(ctx, CloseTaskParams{TaskID: id, Fingerprint: plan.Fingerprint}); err == nil {
		t.Fatal("diverged ff_only succeeded")
	}
	if after := gitOutput(t, remotes["api"], "show-ref", "--heads"); after != before {
		t.Fatalf("bare refs changed: %s -> %s", before, after)
	}
}

func TestInitCustomPrefixPreservesSelectedType(t *testing.T) {
	m, _, remotes := serviceRulesGitEnv(t)
	const id = "T-PREFIX"
	if _, err := m.Init(t.Context(), InitParams{TaskID: id, Services: []string{"web"}, BranchType: "hotfix", BranchPrefix: "repair/"}); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(m.cfg.TasksRoot, id, "web")
	if got, want := gitOutput(t, wt, "rev-parse", "HEAD"), gitOutput(t, remotes["web"], "rev-parse", "main"); got != want {
		t.Fatalf("repair branch at %s, want production %s", got, want)
	}
}
