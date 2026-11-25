//go:build integration

package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

type interruptedTaskCleanup struct {
	git.Client
	stopAt      string
	afterRemove func()
}

func (g *interruptedTaskCleanup) RemoveWorktree(ctx context.Context, common, path string, force bool) error {
	if path == g.stopAt {
		return errors.New("fixture interruption")
	}
	if err := g.Client.RemoveWorktree(ctx, common, path, force); err != nil {
		return err
	}
	if g.afterRemove != nil {
		g.afterRemove()
	}
	return nil
}

func TestTaskCleanupRetry_PushDestination_Integration(t *testing.T) {
	t.Setenv("GIT_ALLOW_PROTOCOL", "file")
	const branch = "feature/APP-1"
	const ref = "refs/heads/" + branch
	for _, mode := range []string{"unchanged", "origin", "pushurl", "missing-on-replacement", "between-worktrees"} {
		t.Run(mode, func(t *testing.T) {
			client := &interruptedTaskCleanup{Client: git.NewCommandClient(newIntegrationLogger())}
			api := newReleaseIntegrationEnvWithGitClient(t, client)
			web := newReleaseIntegrationEnvWithGitClient(t, client)
			m := api.manager
			root := m.taskDir("APP-1")
			repos := map[string]string{"svc-api": api.repoPath, "web": web.repoPath}
			m.discoverer = &staticResolver{paths: repos}
			m.flow.BranchTypes[gitflow.BranchTypeFeature] = gitflow.BranchTypeRule{
				Prefixes: []string{"feature/"}, MergeTargets: []string{"master"}, CloseStrategy: gitflow.CloseStrategyDirectMerge,
			}
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			for name, repo := range repos {
				mustGit(t, repo, "worktree", "add", "-b", branch, filepath.Join(root, name), "master")
				mustGit(t, repo, "push", "origin", ref)
			}
			plan, err := m.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1", DeleteRemoteTaskBranches: true})
			if err != nil || plan.Blocked() {
				t.Fatalf("plan: %v, %v", plan.Preview().Blockers, err)
			}
			approved := gitOutput(t, api.repoPath, "remote", "get-url", "--push", "origin")
			replacement := filepath.Join(t.TempDir(), "replacement.git")
			mustGit(t, api.repoPath, "clone", "--bare", approved, replacement)
			if mode == "between-worktrees" {
				client.afterRemove = func() {
					mustGit(t, api.repoPath, "remote", "set-url", "--push", "origin", replacement)
				}
			} else {
				client.stopAt = filepath.Join(root, "web")
			}
			first, err := m.ExecuteTaskCleanup(t.Context(), plan, nil)
			if err == nil || len(first.Completed) != 1 {
				t.Fatalf("expected interruption after first removal: %+v, %v", first, err)
			}
			client.stopAt = ""
			tip := gitOutput(t, approved, "rev-parse", ref)
			switch mode {
			case "origin", "missing-on-replacement":
				mustGit(t, api.repoPath, "remote", "set-url", "origin", replacement)
			case "pushurl":
				mustGit(t, api.repoPath, "remote", "set-url", "--push", "origin", replacement)
			}
			if mode == "missing-on-replacement" {
				mustGit(t, replacement, "update-ref", "-d", ref, tip)
			}

			result, err := m.ExecuteTaskCleanup(t.Context(), plan, nil, TaskCleanupExecutionSelection{Force: true})
			drift := mode != "unchanged"
			t.Logf("first=%v; retry=%v; error=%v", first.Completed, result.Completed, err)
			if drift && (err == nil || len(result.Completed) != 0) || !drift && err != nil {
				t.Errorf("unexpected retry outcome for %s", mode)
			}
			for name, repo := range repos {
				oid := gitOutput(t, repo, "rev-parse", "master")
				want := ""
				if drift {
					want = oid
				}
				origin := filepath.Join(filepath.Dir(repo), "origin.git")
				for _, store := range []string{repo, origin} {
					if got := gitOutput(t, store, "for-each-ref", "--format=%(objectname)", ref); got != want {
						t.Errorf("%s %s: got %q, want %q", store, ref, got, want)
					}
				}
				tree := filepath.Join(root, name)
				_, statErr := os.Stat(tree)
				registered := strings.Contains(gitOutput(t, repo, "worktree", "list", "--porcelain"), "worktree "+tree+"\n")
				present := drift && name == "web"
				if registered != present || present && statErr != nil || !present && !errors.Is(statErr, os.ErrNotExist) {
					t.Errorf("%s: registered=%t stat=%v, want present=%t", tree, registered, statErr, present)
				}
			}
			if mode == "missing-on-replacement" {
				tip = ""
			}
			if got := gitOutput(t, replacement, "for-each-ref", "--format=%(objectname)", ref); got != tip {
				t.Errorf("replacement ref = %q, want %q", got, tip)
			}
			if _, err := os.Stat(root); drift && err != nil || !drift && !errors.Is(err, os.ErrNotExist) {
				t.Errorf("task root stat=%v, drift=%t", err, drift)
			}
		})
	}
}
