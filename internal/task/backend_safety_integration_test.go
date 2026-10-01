//go:build integration

package task

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

// forgeRemoteURLClient makes forge provider detection see a GitLab remote
// while real git operations keep using the local bare origin configured in
// the repository.
type forgeRemoteURLClient struct {
	git.Client
	url string
}

func (c *forgeRemoteURLClient) RemoteURL(_ context.Context, _, _ string) (string, error) {
	return c.url, nil
}

func newSafetyIntegrationEnv(t *testing.T) releaseIntegrationEnv {
	t.Helper()
	return newReleaseIntegrationEnvWithGitClient(t, &forgeRemoteURLClient{
		Client: &integrationGitClient{Client: git.NewCommandClient(newIntegrationLogger())},
		url:    "git@gitlab.com:group/svc-api.git",
	})
}

// TestIntegration_TaskCleanup_ReviewToProductionSquashProof proves a
// production-targeted review_request task branch merged via squash: the plan
// reconstructs the authoritative merged SHA from forge evidence (no prior
// merge call in this process) and local cleanup executes against real
// repositories. Neither local nor remote branch deletion has an atomic remote
// target guard, so both branches must be retained: local cleanup removes only
// worktrees and task metadata and reports the remote candidates as retained.
func TestIntegration_TaskCleanup_ReviewToProductionSquashProof(t *testing.T) {
	env := newSafetyIntegrationEnv(t)
	env.manager.flow.BranchTypes[gitflow.BranchTypeFeature] = gitflow.BranchTypeRule{
		Prefixes:      []string{"feature/"},
		MergeTargets:  []string{"develop"},
		ReviewTargets: []string{"master"},
		CloseStrategy: gitflow.CloseStrategyReviewRequest,
	}

	env.addFeatureTask(t, "APP-70", func(worktreePath string) {
		writeFile(t, filepath.Join(worktreePath, "review-target.txt"), "review target work\n")
		mustGit(t, worktreePath, "add", "review-target.txt")
		mustGit(t, worktreePath, "commit", "-m", "feat(APP-70): review target work")
	})
	mustGit(t, env.repoPath, "push", "origin", "feature/APP-70")
	sourceSHA := gitOutput(t, env.repoPath, "rev-parse", "feature/APP-70")

	mustGit(t, env.repoPath, "merge", "--squash", "feature/APP-70")
	mustGit(t, env.repoPath, "commit", "-m", "squash merge feature/APP-70")
	mergedSHA := gitOutput(t, env.repoPath, "rev-parse", "master")
	mustGit(t, env.repoPath, "push", "origin", "master")

	if mergedSHA == sourceSHA {
		t.Fatal("fixture must use distinct source and merged SHAs")
	}
	if ok, err := gitIsAncestor(env.repoPath, sourceSHA, mergedSHA); err != nil || ok {
		t.Fatalf("source %s must not be an ancestor of squash merge %s: %v %v", sourceSHA, mergedSHA, ok, err)
	}

	client := &cleanupEvidenceForgeClient{
		history: []forge.MRInfo{{Number: 3, State: "merged", SourceBranch: "feature/APP-70", TargetBranch: "master"}},
		byNumber: map[int]forge.MRReadiness{
			3: {Number: 3, State: "merged", SourceBranch: "feature/APP-70", TargetBranch: "master", HeadSHA: sourceSHA, MergedSHA: mergedSHA},
		},
	}
	env.manager.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitLab: client}

	plan, err := env.manager.PlanTaskCleanup(context.Background(), TaskCleanupRequest{TaskID: "APP-70"})
	if err != nil {
		t.Fatalf("PlanTaskCleanup() error: %v", err)
	}
	if plan.Blocked() {
		t.Fatalf("plan blocked: %q", plan.Preview().Blockers)
	}
	if len(plan.proofs) != 1 || plan.proofs[0].IntegratedSHA != mergedSHA || plan.proofs[0].SourceSHA != sourceSHA {
		t.Fatalf("proofs = %+v, want source lease %s and integrated %s", plan.proofs, sourceSHA, mergedSHA)
	}
	if len(plan.Preview().Remote) != 1 || plan.Preview().Remote[0].ExpectedSHA != sourceSHA {
		t.Fatalf("remote candidates = %+v", plan.Preview().Remote)
	}

	result, err := env.manager.ExecuteTaskCleanup(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("ExecuteTaskCleanup() error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.tasksRoot, "APP-70")); !os.IsNotExist(err) {
		t.Fatalf("task directory remains: %v", err)
	}
	if branch := gitOutput(t, env.repoPath, "branch", "--list", "feature/APP-70"); branch == "" {
		t.Fatal("local branch must be retained: no atomic remote target guard exists")
	}
	if len(result.Remote) != 1 || result.Remote[0].ExpectedSHA != sourceSHA {
		t.Fatalf("retained remote candidates = %+v", result.Remote)
	}
	if out := gitOutput(t, env.repoPath, "ls-remote", "origin", "refs/heads/feature/APP-70"); out == "" {
		t.Fatal("remote branch must be retained: no remote deletion path exists")
	}
}

// TestIntegration_ReleaseMerge_ProductionContainmentAcceptance drives the
// production merge acceptance gate against real repositories: a merged SHA
// that is not contained in production fails closed without persisting success,
// and a retry with the authoritative squash SHA succeeds even after production
// advanced beyond the merge commit.
func TestIntegration_ReleaseMerge_ProductionContainmentAcceptance(t *testing.T) {
	env := newSafetyIntegrationEnv(t)
	rule := env.manager.flow.BranchTypes[gitflow.BranchTypeRelease]
	rule.MergeStrategy = gitflow.MergeStrategySquash
	env.manager.flow.BranchTypes[gitflow.BranchTypeRelease] = rule

	mustGit(t, env.repoPath, "checkout", "-b", "release/1.2.3")
	writeFile(t, filepath.Join(env.repoPath, "release-work.txt"), "release work\n")
	mustGit(t, env.repoPath, "add", "release-work.txt")
	mustGit(t, env.repoPath, "commit", "-m", "release: staged work")
	mustGit(t, env.repoPath, "push", "-u", "origin", "release/1.2.3")
	releaseSHA := gitOutput(t, env.repoPath, "rev-parse", "release/1.2.3")

	mustGit(t, env.repoPath, "checkout", "master")
	mustGit(t, env.repoPath, "merge", "--squash", "release/1.2.3")
	mustGit(t, env.repoPath, "commit", "-m", "squash merge release/1.2.3")
	squashSHA := gitOutput(t, env.repoPath, "rev-parse", "master")
	mustGit(t, env.repoPath, "push", "origin", "master")

	writeFile(t, filepath.Join(env.repoPath, "after-release.txt"), "production moved on\n")
	mustGit(t, env.repoPath, "add", "after-release.txt")
	mustGit(t, env.repoPath, "commit", "-m", "chore: production advances")
	mustGit(t, env.repoPath, "push", "origin", "master")

	if squashSHA == releaseSHA {
		t.Fatal("fixture must use distinct release and merged SHAs")
	}

	client := &releaseMergeForgeClient{
		mergedNumbers: map[int]bool{},
		readiness: map[int]forge.MRReadiness{
			5: {Number: 5, State: "open", SourceBranch: "release/1.2.3", TargetBranch: "master", HeadSHA: releaseSHA, Ready: true, SupportsSHAPin: true, SupportsTargetBinding: true},
		},
		postMerge: map[int]forge.MRReadiness{
			5: {Number: 5, State: "merged", SourceBranch: "release/1.2.3", TargetBranch: "master", HeadSHA: releaseSHA, MergedSHA: releaseSHA},
		},
		mergeSHAs: map[int]string{5: releaseSHA},
	}
	env.manager.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitLab: client}

	svc := domain.ReleaseService{
		Name:          "svc-api",
		RepoPath:      env.repoPath,
		ReleaseBranch: "release/1.2.3",
		Status:        domain.ReleaseStatusAwaitingMasterMerge,
		ProductionMR: &domain.ProductionMRRef{
			Number: 5, URL: "mr", SourceSHA: releaseSHA, State: "open",
			Repo: "gitlab.com/group/svc-api", ProviderHost: "gitlab.com",
		},
	}
	release := writePromoteRelease(t, env.manager, domain.ReleaseStatusAwaitingMasterMerge, svc)

	got, result, err := env.manager.MergeReleaseMRs(context.Background(), release.ID, nil)
	if err != nil {
		t.Fatalf("MergeReleaseMRs() error: %v", err)
	}
	if len(client.merges) != 1 || len(result.Failed) != 1 || result.Failed[0] != "svc-api" {
		t.Fatalf("merges = %#v, result = %#v", client.merges, result)
	}
	failedSvc := got.Services[0]
	if failedSvc.AcceptedMergeSHA != "" || failedSvc.Status != domain.ReleaseStatusFailed || failedSvc.Error == nil || !failedSvc.Error.Recoverable {
		t.Fatalf("service = %#v, want retryable failure without accepted SHA", failedSvc)
	}
	if got.Status != domain.ReleaseStatusAwaitingMasterMerge {
		t.Fatalf("release status = %q", got.Status)
	}

	merged := client.postMerge[5]
	merged.MergedSHA = squashSHA
	client.postMerge[5] = merged

	got, result, err = env.manager.MergeReleaseMRs(context.Background(), release.ID, nil)
	if err != nil {
		t.Fatalf("retry MergeReleaseMRs() error: %v", err)
	}
	if got.Status != domain.ReleaseStatusMasterMerged || got.Services[0].AcceptedMergeSHA != squashSHA || len(result.Skipped) != 1 {
		t.Fatalf("release = %#v, result = %#v", got, result)
	}
	persisted, err := env.manager.GetRelease(context.Background(), release.ID)
	if err != nil {
		t.Fatalf("GetRelease() error: %v", err)
	}
	if persisted.Services[0].AcceptedMergeSHA != squashSHA {
		t.Fatalf("persisted accepted SHA = %q, want %s", persisted.Services[0].AcceptedMergeSHA, squashSHA)
	}
}
