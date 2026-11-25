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

func TestServiceRules_InitPlanAndClose(t *testing.T) {
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
    api: {task_types: {feature: {from: production, close: [{merge: {into: production}}]}}}
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
	targets := map[string]string{"api": "main", "web": "develop"}
	for name := range targets {
		remote := filepath.Join(t.TempDir(), name+".git")
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
	result, err := m.CloseTask(ctx, CloseTaskParams{TaskID: id})
	if err != nil || !result.Success {
		t.Fatalf("close: %+v, %v", result, err)
	}
	for name, target := range targets {
		repo := filepath.Join(root, name)
		if got := gitOutput(t, repo, "show", "origin/"+target+":work"); got != name {
			t.Fatalf("%s %s: %q", name, target, got)
		}
	}
}
