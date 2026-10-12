//go:build integration

package task

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

type directMergeIntegrationGit struct {
	git.Client
	remote     string
	beforePush func()
	pushes     int
}

const directIntegrationURL = "git@gitlab.com:group/svc-api.git"

func (c *directMergeIntegrationGit) RemoteURL(context.Context, string, string) (string, error) {
	return directIntegrationURL, nil
}

func (c *directMergeIntegrationGit) PushURL(context.Context, string, string) (string, error) {
	return directIntegrationURL, nil
}

func (c *directMergeIntegrationGit) RemoteRefSHAAt(ctx context.Context, repo, url, ref string) (string, error) {
	if url != directIntegrationURL {
		return "", fmt.Errorf("unexpected destination %q", url)
	}
	return c.Client.RemoteRefSHAAt(ctx, repo, c.remote, ref)
}

func (c *directMergeIntegrationGit) PushRefWithLease(ctx context.Context, repo, url, ref, oid, leaseRef, lease string) error {
	if url != directIntegrationURL {
		return fmt.Errorf("unexpected destination %q", url)
	}
	c.pushes++
	if c.beforePush != nil {
		c.beforePush()
	}
	return c.Client.PushRefWithLease(ctx, repo, c.remote, ref, oid, leaseRef, lease)
}

func TestIntegration_DirectTaskMerge_ExactTargetLeaseAndRestart(t *testing.T) {
	for _, method := range []gitflow.MergeStrategy{gitflow.MergeStrategyMerge, gitflow.MergeStrategySquash, gitflow.MergeStrategyRebase} {
		for _, compete := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/competitor=%v", method, compete), func(t *testing.T) {
				// Given
				g := &directMergeIntegrationGit{Client: &integrationGitClient{Client: git.NewCommandClient(newIntegrationLogger())}}
				env := newReleaseIntegrationEnvWithGitClient(t, g)
				g.remote = gitOutput(t, env.repoPath, "remote", "get-url", "origin")
				useReviewToProductionFlow(env.manager)
				rule := env.manager.flow.BranchTypes[gitflow.BranchTypeFeature]
				rule.MergeStrategy = method
				env.manager.flow.BranchTypes[gitflow.BranchTypeFeature] = rule
				worktree := filepath.Join(env.tasksRoot, "APP-90", "svc-api")
				if err := os.MkdirAll(filepath.Dir(worktree), 0o755); err != nil {
					t.Fatal(err)
				}
				mustGit(t, env.repoPath, "worktree", "add", "-b", "feature/APP-90", worktree, "master")
				writeFile(t, filepath.Join(worktree, "feature.txt"), "feature content\n")
				mustGit(t, worktree, "add", ".")
				mustGit(t, worktree, "commit", "-m", "feature")
				mustGit(t, worktree, "push", "origin", "feature/APP-90")
				head := gitOutput(t, worktree, "rev-parse", "HEAD")
				base := gitOutput(t, env.repoPath, "rev-parse", "master")
				develop := gitOutput(t, env.repoPath, "rev-parse", "origin/develop")
				mr := forge.MRReadiness{Number: 14, State: "open", SourceBranch: "feature/APP-90", TargetBranch: "master", HeadSHA: head, Ready: true}
				f := &mergeForgeClient{readiness: map[string]forge.MRReadiness{mr.SourceBranch: mr}, readinessByNumber: map[int]forge.MRReadiness{14: mr}}
				env.manager.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitLab: f}
				expectedTip := ""
				if compete {
					g.beforePush = func() {
						tree := gitOutput(t, env.repoPath, "rev-parse", base+"^{tree}")
						expectedTip = gitOutput(t, env.repoPath, "commit-tree", tree, "-p", base, "-m", "competing commit")
						mustGit(t, env.repoPath, "push", "origin", expectedTip+":refs/heads/master")
					}
				}

				// When
				result, err := env.manager.MergeTaskMRs(t.Context(), "APP-90")

				// Then
				if err != nil || g.pushes != 1 || len(f.merges) != 0 {
					t.Fatalf("result=%+v err=%v pushes=%d forge merges=%v", result, err, g.pushes, f.merges)
				}
				tip := gitOutput(t, g.remote, "rev-parse", "refs/heads/master")
				if compete {
					if result.Errs["svc-api"] == nil || len(result.Merged) != 0 || tip != expectedTip {
						t.Fatalf("stale lease overwrote competing target: result=%+v tip=%s want=%s", result, tip, expectedTip)
					}
				} else {
					if len(result.Errs) != 0 || !slices.Equal(result.Merged, []string{"svc-api"}) || gitOutput(t, g.remote, "show", tip+":feature.txt") != "feature content" {
						t.Fatalf("direct merge failed: result=%+v tip=%s", result, tip)
					}
					restarted := *env.manager
					// Forge still reports open. A restart must recognize the proof.
					inspection, err := restarted.InspectTaskMerge(t.Context(), "APP-90")
					if err != nil || len(inspection.Services) != 1 || inspection.Services[0].Status != "merged" || inspection.Services[0].MR.MergedSHA != tip {
						t.Fatalf("restart inspection=%+v err=%v", inspection, err)
					}
				}
				if gitOutput(t, env.repoPath, "rev-parse", "master") != base || gitOutput(t, g.remote, "rev-parse", "refs/heads/develop") != develop || gitOutput(t, worktree, "rev-parse", "HEAD") != head || gitOutput(t, worktree, "status", "--porcelain") != "" {
					t.Fatal("direct merge changed an unrelated branch or user checkout")
				}
			})
		}
	}
}
