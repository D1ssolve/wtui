//go:build integration

package task

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/discovery"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/dotnet"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/sln"
	"github.com/D1ssolve/wtui/internal/validation"
)

// TestIntegration_TrunkBasedFlow_EndToEnd runs the trunk-based preset against real repositories:
// squash-merged change, release cut from main, fix cherry-picked into the open release, release
// tagged on its own branch, and main never back-merged.
func TestIntegration_TrunkBasedFlow_EndToEnd(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	ctx := context.Background()
	root := resolvedPath(t, t.TempDir())
	tasksRoot := filepath.Join(root, ".tasks")
	remote := filepath.Join(root, "origin.git")
	repo := filepath.Join(root, "svc-api")
	mustGit(t, root, "init", "--bare", "-b", "main", remote)
	mustGit(t, root, "clone", remote, repo)
	mustGit(t, repo, "config", "user.email", "integration@example.com")
	mustGit(t, repo, "config", "user.name", "Integration Test")
	mustGit(t, repo, "checkout", "-b", "main")
	writeFile(t, filepath.Join(repo, "README.md"), "# svc-api\n")
	mustGit(t, repo, "add", ".")
	mustGit(t, repo, "commit", "-m", "initial")
	mustGit(t, repo, "push", "-u", "origin", "main")

	cfg := &config.Config{RootDir: root, TasksRoot: tasksRoot, Workflow: &config.WorkflowConfig{Extends: "trunk-based"}}
	if _, err := cfg.Effective(); err != nil {
		t.Fatal(err)
	}
	flow, err := gitflow.Resolve(cfg)
	if err != nil {
		t.Fatal(err)
	}
	logger := newIntegrationLogger()
	client := &integrationGitClient{Client: git.NewCommandClient(logger)}
	m := New(cfg, client, discovery.New(cfg, client, logger), sln.NewManager(dotnet.NewCommandClient(logger), logger), validation.NewTaskValidator(client), flow, nil, logger).(*manager)

	squashTask := func(taskID, branch, file string) string {
		t.Helper()
		wt := filepath.Join(tasksRoot, taskID, "svc-api")
		if err := os.MkdirAll(filepath.Dir(wt), 0o755); err != nil {
			t.Fatal(err)
		}
		mustGit(t, repo, "worktree", "add", "-b", branch, wt, "main")
		writeFile(t, filepath.Join(wt, file), taskID+"\n")
		mustGit(t, wt, "add", ".")
		mustGit(t, wt, "commit", "-m", taskID+" work")
		mustGit(t, wt, "push", "-u", "origin", branch)
		mustGit(t, repo, "merge", "--squash", branch)
		mustGit(t, repo, "commit", "-m", taskID+" (squash)")
		mustGit(t, repo, "push", "origin", "main")
		return gitOutput(t, repo, "rev-parse", "HEAD")
	}

	squashTask("APP-1", "APP-1", "feature.txt")
	release, err := m.CreateRelease(ctx, CreateReleaseParams{TaskIDs: []string{"APP-1"}, ServiceVersions: map[string]string{"svc-api": "1.0.0"}, StartImmediately: true})
	if err != nil {
		t.Fatalf("CreateRelease: %v", err)
	}
	if release.Status != domain.ReleaseStatusPrepared || release.FlowFingerprint == "" {
		t.Fatalf("release = %+v, want prepared with flow fingerprint", release)
	}

	fixSHA := squashTask("FIX-1", "fix/FIX-1", "fix.txt")
	services, err := m.ListServices(ctx, "FIX-1")
	if err != nil || len(services) != 1 {
		t.Fatalf("ListServices(FIX-1) = %v, %v", services, err)
	}
	var steps []string
	if err := m.cherryPickIntoOpenReleases(ctx, "FIX-1", services[0], fixSHA, func(name string, status StepStatus, _ string) {
		steps = append(steps, name+"="+string(status))
	}); err != nil {
		t.Fatalf("cherryPickIntoOpenReleases: %v (steps %v)", err, steps)
	}
	mustGit(t, repo, "fetch", "origin")
	if got := gitOutput(t, repo, "show", "origin/release/1.0.0:fix.txt"); got != "FIX-1" {
		t.Fatalf("release branch fix.txt = %q, want the cherry-picked fix", got)
	}
	if err := m.cherryPickIntoOpenReleases(ctx, "FIX-1", services[0], fixSHA, func(string, StepStatus, string) {}); err != nil {
		t.Fatalf("repeated cherry-pick must be idempotent: %v", err)
	}
	releaseTip := gitOutput(t, repo, "rev-parse", "origin/release/1.0.0")
	if count := gitOutput(t, repo, "rev-list", "--count", "origin/main..origin/release/1.0.0"); count != "1" {
		t.Fatalf("release-only commits = %s, want exactly one pick", count)
	}

	finished, err := m.PromoteRelease(ctx, release.ID, nil)
	if err != nil {
		t.Fatalf("PromoteRelease: %v", err)
	}
	if finished.Status != domain.ReleaseStatusReleased {
		t.Fatalf("status = %s, want released", finished.Status)
	}
	mustGit(t, repo, "fetch", "origin", "--tags")
	if tagged := gitOutput(t, repo, "rev-parse", "v1.0.0^{}"); tagged != releaseTip {
		t.Fatalf("v1.0.0 -> %s, want release branch tip %s", tagged, releaseTip)
	}
	if ok, err := gitIsAncestor(repo, "origin/release/1.0.0", "origin/main"); err != nil || ok {
		t.Fatalf("release branch merged back into main (%v, %v); trunk-based has no back-merge", ok, err)
	}
}
