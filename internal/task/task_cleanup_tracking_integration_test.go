//go:build integration

package task

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/discovery"
	"github.com/D1ssolve/wtui/internal/git"
)

func TestTaskCleanupTrackingRecovery_Integration(t *testing.T) {
	t.Setenv("GIT_ALLOW_PROTOCOL", "file")
	const branch = "feature/PORT"
	const tracking = "refs/remotes/origin/develop"
	for _, timing := range []string{"before-preview", "shadow-ref", "after-preview", "after-removal", "legacy-partial"} {
		t.Run(timing, func(t *testing.T) {
			client := &interruptedTaskCleanup{Client: git.NewCommandClient(newIntegrationLogger())}
			env := newReleaseIntegrationEnvWithGitClient(t, client)
			m, repo := env.manager, env.repoPath
			root := m.taskDir("PORT")
			tree := filepath.Join(root, "svc-api")
			mustGit(t, repo, "worktree", "add", "-b", branch, tree, "develop")
			const owner = `{"issue":{"key":"42"}}`
			metadata := filepath.Join(root, taskMetadataFileName)
			writeFile(t, metadata, owner)
			source := gitOutput(t, repo, "rev-parse", branch)
			mustGit(t, repo, "commit", "--allow-empty", "-m", "integrated target")
			target := gitOutput(t, repo, "rev-parse", "HEAD")
			bare := filepath.Join(filepath.Dir(repo), "origin.git")
			mustGit(t, bare, "fetch", repo, "HEAD:refs/heads/develop")
			mustGit(t, repo, "fetch", "origin")
			refs := gitOutput(t, bare, "show-ref")
			stale := func() { mustGit(t, repo, "update-ref", tracking, source, target) }
			if timing == "before-preview" {
				stale()
			} else if timing == "shadow-ref" {
				mustGit(t, repo, "update-ref", "-d", tracking, target)
				mustGit(t, repo, "branch", tracking, target)
			} else if timing == "legacy-partial" {
				// Older binaries removed the tree without recording its identity.
				mustGit(t, repo, "worktree", "remove", tree)
			} else if timing == "after-removal" {
				client.afterRemove = stale
			}
			req := TaskCleanupRequest{TaskID: "PORT"}
			plan, err := m.PlanTaskCleanup(t.Context(), req)
			if err != nil {
				t.Fatal(err)
			}
			if timing == "after-preview" {
				stale()
			}
			result, err := m.ExecuteTaskCleanup(t.Context(), plan, nil)
			t.Logf("first blockers=%v completed=%v err=%v", plan.Preview().Blockers, result.Completed, err)
			if err == nil {
				t.Fatal("stale tracking allowed cleanup")
			}
			registered := strings.Contains(gitOutput(t, repo, "worktree", "list", "--porcelain"), "worktree "+tree+"\n")
			if timing == "legacy-partial" {
				if !plan.Blocked() || len(result.Completed) != 0 || registered {
					t.Fatalf("legacy partial cleanup was not blocked: %+v", result)
				}
			} else if timing != "after-removal" {
				if plan.Blocked() != (timing != "after-preview") || len(result.Completed) != 0 || !registered {
					t.Errorf("stale preview blocked=%t registered=%t completed=%v", plan.Blocked(), registered, result.Completed)
				}
			} else if plan.Blocked() || len(result.Completed) != 1 || registered {
				t.Fatal("fixture did not interrupt after worktree removal")
			}
			if got := gitOutput(t, repo, "rev-parse", branch); got != source {
				t.Fatalf("pending branch=%s, want %s", got, source)
			}
			if data, err := os.ReadFile(metadata); err != nil || string(data) != owner {
				t.Fatalf("initial refusal lost owner file: %q, %v", data, err)
			}
			mustGit(t, repo, "fetch", "origin")
			freshGit := git.NewCommandClient(m.logger)
			fresh := New(m.cfg, freshGit, discovery.New(m.cfg, freshGit, m.logger), nil, nil, m.flow, nil, m.logger)
			retry := func() (TaskCleanupResult, error) {
				p, err := fresh.PlanTaskCleanup(t.Context(), req)
				if err != nil {
					t.Fatal(err)
				}
				return fresh.ExecuteTaskCleanup(t.Context(), p, nil)
			}
			if !registered {
				r, err := retry()
				data, readErr := os.ReadFile(metadata)
				t.Logf("fresh retry completed=%v err=%v owner=%q branch=%s", r.Completed, err, data, gitOutput(t, repo, "rev-parse", branch))
				if !errors.Is(err, ErrTaskCleanupBlocked) || len(r.Completed) != 0 || readErr != nil || string(data) != owner {
					t.Fatal("fresh retry lost pending branch or owner metadata")
				}
				if _, err := fresh.ForceTaskCleanup(t.Context(), req.TaskID, false, nil); !errors.Is(err, ErrTaskCleanupBlocked) {
					t.Fatalf("force bypassed pending branch: %v", err)
				}
				mustGit(t, repo, "worktree", "add", tree, branch)
			}
			r, err := retry()
			if err != nil || len(r.Completed) != 3 {
				t.Fatalf("accounted retry: %+v, %v", r, err)
			}
			if got := gitOutput(t, repo, "for-each-ref", "--format=%(objectname)", "refs/heads/"+branch); got != "" {
				t.Fatalf("completed cleanup left branch %s", got)
			}
			if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) || gitOutput(t, bare, "show-ref") != refs {
				t.Fatalf("completed cleanup: root stat=%v or bare refs changed", err)
			}
		})
	}
}
