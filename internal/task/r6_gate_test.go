//go:build integration

package task

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/discovery"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/validation"
)

func TestR6GateFFClosePublishesDespiteLocalTrackingLookalike(t *testing.T) {
	m, root, remotes := serviceRulesGitEnvWithRemote(t, "upstream")
	const id = "T-REMOTE"
	if _, err := m.Init(t.Context(), InitParams{TaskID: id, Services: []string{"api"}}); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(m.cfg.TasksRoot, id, "api")
	mustGit(t, wt, "commit", "--allow-empty", "-m", "task change")
	head := gitOutput(t, wt, "rev-parse", "HEAD")
	before := gitOutput(t, remotes["api"], "rev-parse", "refs/heads/main")
	mustGit(t, filepath.Join(root, "api"), "branch", "upstream/main", head)
	plan, err := m.PlanCloseTask(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	result, err := m.CloseTask(t.Context(), CloseTaskParams{TaskID: id, Fingerprint: plan.Fingerprint})
	if err != nil || !result.Success {
		t.Fatalf("close: %+v %v", result, err)
	}
	if got := gitOutput(t, remotes["api"], "rev-parse", "refs/heads/main"); got != head {
		t.Fatalf("successful close left upstream main at %s (before %s), want %s; steps=%+v", got, before, head, result.Steps)
	}
}

func TestR6GatePinnedTrainRejectsLocalTrackingLookalike(t *testing.T) {
	m, root, remotes := serviceRulesGitEnvWithRemote(t, "upstream")
	const id = "TRAIN-9"
	const branch = "feature/" + id
	const target = "develop"
	// web inherits the feature rule; use a real parsed review configuration for it.
	path := filepath.Join(root, "review.yaml")
	writeFile(t, path, "forge: {remote: upstream, merge_queue: true}\nworkflow: {extends: git-flow, branches: {production: main}}\n")
	m = r6GateLoadManager(t, root, path)
	if _, err := m.Init(t.Context(), InitParams{TaskID: id, Services: []string{"web"}}); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "web")
	wt := filepath.Join(m.cfg.TasksRoot, id, "web")
	url := "file://gitlab.com" + filepath.ToSlash(remotes["web"])
	mustGit(t, repo, "remote", "set-url", "upstream", url)
	mustGit(t, wt, "commit", "--allow-empty", "-m", "reviewed work")
	mustGit(t, wt, "push", "upstream", "HEAD:refs/heads/"+branch)
	head := gitOutput(t, wt, "rev-parse", "HEAD")
	// Deliver the exact commit and then rewind only the server target.
	old := gitOutput(t, remotes["web"], "rev-parse", "refs/heads/"+target)
	mustGit(t, wt, "push", "upstream", "HEAD:refs/heads/"+target)
	mustGit(t, remotes["web"], "update-ref", "refs/heads/"+target, old, head)
	rec := forge.MRReadiness{Number: 7, State: "merged", SourceBranch: branch, TargetBranch: target, HeadSHA: head, MergedSHA: head}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitLab: &mergeForgeClient{readinessByNumber: map[int]forge.MRReadiness{7: rec}}}
	if err := m.recordTaskTrainPin(id, taskTrainPin{taskTrainKey: taskTrainKey{"web", target}, Repo: forge.ExtractRepoPath(url), Number: 7, Source: branch, Head: head}); err != nil {
		t.Fatal(err)
	}
	for _, shadow := range []bool{false, true} {
		if shadow {
			mustGit(t, repo, "branch", "upstream/"+target, head)
		}
		got, err := m.MergeTaskMRs(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Merged) != 0 || got.Errs["web"] == nil {
			t.Fatalf("shadow=%v: M=%+v; merge SHA=%s actual upstream tip=%s", shadow, got, head, gitOutput(t, remotes["web"], "rev-parse", "refs/heads/"+target))
		}
	}
}

func TestR6GateCleanupChecksWholeConfiguredTargetName(t *testing.T) {
	m, root, remotes := serviceRulesGitEnvWithRemote(t, "team")
	const id = "SCOPE-1"
	path := filepath.Join(root, "service-target.yaml")
	writeFile(t, path, `forge: {remote: team}
workflow:
  extends: git-flow
  branches: {production: main}
  services:
    api: {task_types: {feature: {from: production, close: [{merge: {into: team/main, merge: ff_only}}]}}}
`)
	m = r6GateLoadManager(t, root, path)
	repo := filepath.Join(root, "api")
	mustGit(t, repo, "push", "team", "refs/heads/main:refs/heads/team/main")
	if _, err := m.Init(t.Context(), InitParams{TaskID: id, Services: []string{"api"}}); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(m.cfg.TasksRoot, id, "api")
	mustGit(t, wt, "commit", "--allow-empty", "-m", "not delivered to team/main")
	mustGit(t, wt, "push", "team", "HEAD:refs/heads/main", "HEAD:refs/heads/feature/"+id)
	before := gitOutput(t, remotes["api"], "rev-parse", "refs/heads/team/main")
	head := gitOutput(t, wt, "rev-parse", "HEAD")
	if before == head {
		t.Fatal("fixture delivered to the wrong branch")
	}
	p, err := m.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: id, DeleteRemoteTaskBranches: true})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Blocked() {
		res, err := m.ExecuteTaskCleanup(t.Context(), p, nil)
		_, statErr := os.Stat(wt)
		remaining := strings.TrimSpace(gitOutput(t, remotes["api"], "for-each-ref", "--format=%(refname)", "refs/heads/feature/"+id))
		t.Fatalf("D authorized undelivered task %s; intended branch team/main=%s vs main=%s; completed=%v err=%v worktree stat=%v remote source=%q", id, before, head, res.Completed, err, statErr, remaining)
	}
}

func r6GateLoadManager(t *testing.T, root, path string) *manager {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RootDir, cfg.TasksRoot = root, filepath.Join(root, ".tasks")
	cfg, err = cfg.Effective()
	if err != nil {
		t.Fatal(err)
	}
	flow, err := gitflow.Resolve(cfg)
	if err != nil {
		t.Fatal(err)
	}
	log := newIntegrationLogger()
	g := git.NewRemoteCommandClient(log, cfg.ServiceRemote())
	return New(cfg, g, discovery.New(cfg, g, log), &recordingSlnGenerator{}, validation.NewTaskValidator(g), flow, nil, log).(*manager)
}
