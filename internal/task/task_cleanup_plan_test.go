package task

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

const (
	taskCleanupTaskSHA    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	taskCleanupDevelopSHA = "dddddddddddddddddddddddddddddddddddddddd"
	taskCleanupMasterSHA  = "cccccccccccccccccccccccccccccccccccccccc"
	taskCleanupReleaseSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	taskCleanupAccepted   = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	taskCleanupSquashSHA  = "9999999999999999999999999999999999999999"
)

func TestPlanTaskCleanup_AllServicesProvenComplete_AuthorizesLocalOnly(t *testing.T) {
	mgr, gitMock := taskCleanupTestManager(t)

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	preview := plan.Preview()
	if len(preview.Blockers) != 0 {
		t.Fatalf("blockers = %q", preview.Blockers)
	}
	if len(preview.Services) != 1 || !preview.Services[0].Complete {
		t.Fatalf("services = %+v", preview.Services)
	}
	if len(preview.Remote) != 1 || preview.Remote[0].Branch != "feature/APP-1" || preview.Remote[0].ExpectedSHA != taskCleanupTaskSHA {
		t.Fatalf("remote candidates = %+v", preview.Remote)
	}

	kinds := map[releaseCleanupStepKind]int{}
	for _, step := range plan.steps {
		kinds[step.kind]++
	}
	if kinds[cleanupTaskWorktree] != 1 || kinds[cleanupLocalTaskBranch] != 1 || kinds[cleanupTaskDirectory] != 1 {
		t.Fatalf("local step kinds = %+v", kinds)
	}
	if kinds[cleanupRemoteTaskBranch] != 0 {
		t.Fatal("remote branch deletion must not be authorized")
	}
	for _, step := range plan.steps {
		if step.kind == cleanupLocalTaskBranch && step.expectedSHA != taskCleanupTaskSHA {
			t.Fatalf("local branch lease = %q", step.expectedSHA)
		}
	}
	if len(plan.proofs) != 1 || plan.proofs[0].SourceSHA != taskCleanupTaskSHA || len(plan.proofs[0].Targets) != 1 {
		t.Fatalf("proofs = %+v", plan.proofs)
	}
	if plan.proofs[0].Targets[0].ref != "refs/heads/master" || plan.proofs[0].Targets[0].plannedSHA != taskCleanupMasterSHA {
		t.Fatalf("proof targets = %+v", plan.proofs[0].Targets)
	}
	if plan.Fingerprint() == ([32]byte{}) {
		t.Fatal("fingerprint not finalized")
	}
	if gitMock.deleteBranchCalls != 0 || len(gitMock.removeWorktreeCalls) != 0 {
		t.Fatal("planning mutated git")
	}
}

// TestPlanTaskCleanup_ReviewRequestStrategy_ProvesEveryReviewTarget pins
// cleanup proof for a review_request branch type to every configured
// ReviewTarget: accepted merge evidence in only one target never authorizes
// cleanup, and duplicate or empty target entries collapse to one proof each.
func TestPlanTaskCleanup_ReviewRequestStrategy_ProvesEveryReviewTarget(t *testing.T) {
	useReviewFeatureFlow := func(mgr *manager) {
		mgr.flow.BranchTypes[gitflow.BranchTypeFeature] = gitflow.BranchTypeRule{
			Prefixes:      []string{"feature/"},
			MergeTargets:  []string{"develop"},
			ReviewTargets: []string{"master", "develop"},
			CloseStrategy: gitflow.CloseStrategyReviewRequest,
		}
	}
	assertBlockedNotContained := func(t *testing.T, plan TaskCleanupPlan) {
		t.Helper()
		if !plan.Blocked() || len(plan.steps) != 0 {
			t.Fatalf("blocked = %v, steps = %+v", plan.Blocked(), plan.steps)
		}
		if !strings.Contains(strings.Join(plan.Preview().Blockers, "\n"), "not contained") {
			t.Fatalf("blockers = %q", plan.Preview().Blockers)
		}
	}

	t.Run("proof in every review target authorizes against all of them", func(t *testing.T) {
		mgr, gitMock := taskCleanupTestManager(t)
		useReviewFeatureFlow(mgr)
		gitMock.isAncestorFn = func(_, _, descendant string) (bool, error) {
			return descendant == taskCleanupMasterSHA || descendant == taskCleanupDevelopSHA, nil
		}

		plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
		if err != nil {
			t.Fatal(err)
		}
		if plan.Blocked() {
			t.Fatalf("blockers = %q", plan.Preview().Blockers)
		}
		if len(plan.proofs) != 1 || len(plan.proofs[0].Targets) != 2 {
			t.Fatalf("proofs = %+v", plan.proofs)
		}
		byRef := map[string]string{}
		for _, target := range plan.proofs[0].Targets {
			byRef[target.ref] = target.plannedSHA
		}
		if byRef["refs/heads/master"] != taskCleanupMasterSHA || byRef["refs/heads/develop"] != taskCleanupDevelopSHA {
			t.Fatalf("proof targets = %+v", plan.proofs[0].Targets)
		}
		for _, step := range plan.steps {
			if step.kind == cleanupLocalTaskBranch && len(step.targets) != 2 {
				t.Fatalf("local branch step targets = %+v", step.targets)
			}
		}
		if plan.Fingerprint() == ([32]byte{}) {
			t.Fatal("fingerprint not finalized")
		}
	})

	t.Run("proof only in first review target blocks", func(t *testing.T) {
		mgr, gitMock := taskCleanupTestManager(t)
		useReviewFeatureFlow(mgr)
		gitMock.isAncestorFn = func(_, _, descendant string) (bool, error) {
			return descendant == taskCleanupMasterSHA, nil
		}

		plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
		if err != nil {
			t.Fatal(err)
		}
		assertBlockedNotContained(t, plan)
	})

	t.Run("proof only in merge target blocks", func(t *testing.T) {
		mgr, gitMock := taskCleanupTestManager(t)
		useReviewFeatureFlow(mgr)
		gitMock.isAncestorFn = func(_, _, descendant string) (bool, error) {
			return descendant == taskCleanupDevelopSHA, nil
		}

		plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
		if err != nil {
			t.Fatal(err)
		}
		assertBlockedNotContained(t, plan)
	})

	t.Run("duplicate and empty review targets collapse", func(t *testing.T) {
		mgr, gitMock := taskCleanupTestManager(t)
		mgr.flow.BranchTypes[gitflow.BranchTypeFeature] = gitflow.BranchTypeRule{
			Prefixes:      []string{"feature/"},
			ReviewTargets: []string{"master", "", "master"},
			CloseStrategy: gitflow.CloseStrategyReviewRequest,
		}
		gitMock.isAncestorFn = func(_, _, descendant string) (bool, error) {
			return descendant == taskCleanupMasterSHA, nil
		}

		plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
		if err != nil {
			t.Fatal(err)
		}
		if plan.Blocked() {
			t.Fatalf("blockers = %q", plan.Preview().Blockers)
		}
		if len(plan.proofs) != 1 || len(plan.proofs[0].Targets) != 1 || plan.proofs[0].Targets[0].ref != "refs/heads/master" {
			t.Fatalf("proofs = %+v", plan.proofs)
		}
	})
}

func TestPlanTaskCleanup_PreviewCloneSafe(t *testing.T) {
	mgr, _ := taskCleanupTestManager(t)
	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	preview := plan.Preview()
	preview.Services[0].Name = "CORRUPT"
	preview.Remote[0].Branch = "CORRUPT"
	again := plan.Preview()
	if again.Services[0].Name != "svc" || again.Remote[0].Branch != "feature/APP-1" {
		t.Fatalf("preview mutated: %+v", again)
	}
}

func TestPlanTaskCleanup_IncompleteAncestry_BlockedWithoutMutation(t *testing.T) {
	mgr, gitMock := taskCleanupTestManager(t)
	gitMock.isAncestorFn = func(_, _, _ string) (bool, error) { return false, nil }

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Blocked() || len(plan.steps) != 0 {
		t.Fatalf("blocked = %v, steps = %+v", plan.Blocked(), plan.steps)
	}
	if !strings.Contains(strings.Join(plan.Preview().Blockers, "\n"), "not contained") {
		t.Fatalf("blockers = %q", plan.Preview().Blockers)
	}
	if len(plan.Preview().Remote) != 0 {
		t.Fatalf("remote candidates = %+v", plan.Preview().Remote)
	}
	if gitMock.deleteBranchCalls != 0 || len(gitMock.removeWorktreeCalls) != 0 {
		t.Fatal("planning mutated git")
	}
}

func TestPlanTaskCleanup_ProtectedBranchNeverCandidate(t *testing.T) {
	mgr, _ := taskCleanupTestManager(t)
	mgr.git.(*mockGitClient).listWorktreesRes[0].Branch = "refs/heads/develop"
	base := mgr.git.(*mockGitClient).resolveRefFn
	mgr.git.(*mockGitClient).resolveRefFn = func(repo, ref string) (string, error) {
		if ref == "refs/heads/develop" {
			return taskCleanupDevelopSHA, nil
		}
		return base(repo, ref)
	}

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Blocked() || len(plan.steps) != 0 {
		t.Fatalf("blocked = %v, steps = %+v", plan.Blocked(), plan.steps)
	}
	if !strings.Contains(strings.Join(plan.Preview().Blockers, "\n"), "protected") {
		t.Fatalf("blockers = %q", plan.Preview().Blockers)
	}
}

func TestPlanTaskCleanup_UnsafeWorktreeBlocks(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*mockGitClient)
		want   string
	}{
		{"locked", func(g *mockGitClient) { g.listWorktreesRes[0].Locked = true }, "locked"},
		{"dirty", func(g *mockGitClient) { g.isDirtyRes = true }, "dirty"},
		{"untracked files", func(g *mockGitClient) {
			g.repoStatusFn = func(string) (git.RawStatus, error) {
				return git.RawStatus{UntrackedPaths: []string{"notes.txt"}}, nil
			}
		}, "dirty"},
		{"active operation", func(g *mockGitClient) {
			g.operationStateFn = func(string) ([]domain.RepoState, error) {
				return []domain.RepoState{domain.RepoStateMerging}, nil
			}
		}, "operation"},
		{"detached worktree", func(g *mockGitClient) { g.listWorktreesRes[0].Branch = "(detached)" }, "no task branch"},
		{"head moved", func(g *mockGitClient) { g.listWorktreesRes[0].HEAD = strings.Repeat("f", 40) }, "identity mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr, gitMock := taskCleanupTestManager(t)
			tc.mutate(gitMock)
			plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
			if err != nil {
				t.Fatal(err)
			}
			if !plan.Blocked() || len(plan.steps) != 0 {
				t.Fatalf("blocked = %v, steps = %+v", plan.Blocked(), plan.steps)
			}
			if !strings.Contains(strings.Join(plan.Preview().Blockers, "\n"), tc.want) {
				t.Fatalf("blockers = %q, want %q", plan.Preview().Blockers, tc.want)
			}
		})
	}
}

// TestPlanTaskCleanup_TaskOwnedHotfixBranch_AllowsCleanup pins that cleanup
// authorization uses exact task ownership: a hotfix branch owned by the task
// is accepted even though the generic protected policy covers hotfix prefixes.
func TestPlanTaskCleanup_TaskOwnedHotfixBranch_AllowsCleanup(t *testing.T) {
	mgr, gitMock := taskCleanupTestManager(t)
	gitMock.listWorktreesRes[0].Branch = "refs/heads/hotfix/APP-1"
	base := gitMock.resolveRefFn
	gitMock.resolveRefFn = func(repo, ref string) (string, error) {
		if ref == "refs/heads/hotfix/APP-1" {
			return taskCleanupTaskSHA, nil
		}
		return base(repo, ref)
	}

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Blocked() {
		t.Fatalf("task-owned hotfix blocked: %q", plan.Preview().Blockers)
	}
	if !plan.Preview().Services[0].Complete {
		t.Fatalf("services = %+v", plan.Preview().Services)
	}
}

// TestPlanTaskCleanup_HotfixRequiresEveryReviewTarget pins that a hotfix
// closing via review_request is proven against every configured ReviewTarget:
// accepted merge evidence in production alone never authorizes cleanup while
// integration is still missing it, and only evidence in both targets does.
func TestPlanTaskCleanup_HotfixRequiresEveryReviewTarget(t *testing.T) {
	setup := func(t *testing.T) (*manager, *mockGitClient) {
		t.Helper()
		mgr, gitMock := taskCleanupTestManager(t)
		mgr.flow.BranchTypes[gitflow.BranchTypeHotfix] = gitflow.BranchTypeRule{
			Prefixes:      []string{"hotfix/"},
			ReviewTargets: []string{"master", "develop"},
			CloseStrategy: gitflow.CloseStrategyReviewRequest,
		}
		gitMock.listWorktreesRes[0].Branch = "refs/heads/hotfix/APP-1"
		base := gitMock.resolveRefFn
		gitMock.resolveRefFn = func(repo, ref string) (string, error) {
			if ref == "refs/heads/hotfix/APP-1" {
				return taskCleanupTaskSHA, nil
			}
			return base(repo, ref)
		}
		return mgr, gitMock
	}

	t.Run("production merged and integration missing blocks without cleanup steps", func(t *testing.T) {
		mgr, gitMock := setup(t)
		gitMock.isAncestorFn = func(_, _, descendant string) (bool, error) {
			return descendant == taskCleanupMasterSHA, nil
		}

		plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
		if err != nil {
			t.Fatal(err)
		}
		if !plan.Blocked() || len(plan.steps) != 0 {
			t.Fatalf("blocked = %v, steps = %+v", plan.Blocked(), plan.steps)
		}
		if !strings.Contains(strings.Join(plan.Preview().Blockers, "\n"), "not contained") {
			t.Fatalf("blockers = %q", plan.Preview().Blockers)
		}
		if len(gitMock.removeWorktreeCalls) != 0 {
			t.Fatal("planning removed worktrees")
		}
	})

	t.Run("both targets merged authorizes worktree and directory cleanup", func(t *testing.T) {
		mgr, _ := setup(t)

		plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
		if err != nil {
			t.Fatal(err)
		}
		if plan.Blocked() {
			t.Fatalf("blockers = %q", plan.Preview().Blockers)
		}
		if len(plan.proofs) != 1 || len(plan.proofs[0].Targets) != 2 {
			t.Fatalf("proofs = %+v", plan.proofs)
		}
		kinds := map[releaseCleanupStepKind]int{}
		for _, step := range plan.steps {
			kinds[step.kind]++
		}
		if kinds[cleanupTaskWorktree] != 1 || kinds[cleanupTaskDirectory] != 1 {
			t.Fatalf("local step kinds = %+v", kinds)
		}
	})
}

// TestPlanTaskCleanup_UnrelatedBranchBlocks pins that a branch not exactly
// owned by the task is never a cleanup candidate, even under a registered
// prefix of another task.
func TestPlanTaskCleanup_UnrelatedBranchBlocks(t *testing.T) {
	mgr, gitMock := taskCleanupTestManager(t)
	gitMock.listWorktreesRes[0].Branch = "refs/heads/feature/OTHER-9"
	base := gitMock.resolveRefFn
	gitMock.resolveRefFn = func(repo, ref string) (string, error) {
		if ref == "refs/heads/feature/OTHER-9" {
			return taskCleanupTaskSHA, nil
		}
		return base(repo, ref)
	}

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Blocked() || len(plan.steps) != 0 {
		t.Fatalf("blocked = %v, steps = %+v", plan.Blocked(), plan.steps)
	}
	if !strings.Contains(strings.Join(plan.Preview().Blockers, "\n"), "not an exact task-owned branch") {
		t.Fatalf("blockers = %q", plan.Preview().Blockers)
	}
}

// TestPlanTaskCleanup_ReleaseNamespaceTaskBranchBlocks pins release-prefix
// precedence: a branch under the release namespace is blocked even when its
// suffix matches the task ID exactly.
func TestPlanTaskCleanup_ReleaseNamespaceTaskBranchBlocks(t *testing.T) {
	mgr, gitMock := taskCleanupTestManager(t)
	gitMock.listWorktreesRes[0].Branch = "refs/heads/release/APP-1"
	base := gitMock.resolveRefFn
	gitMock.resolveRefFn = func(repo, ref string) (string, error) {
		if ref == "refs/heads/release/APP-1" {
			return taskCleanupTaskSHA, nil
		}
		return base(repo, ref)
	}

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Blocked() || len(plan.steps) != 0 {
		t.Fatalf("blocked = %v, steps = %+v", plan.Blocked(), plan.steps)
	}
	if !strings.Contains(strings.Join(plan.Preview().Blockers, "\n"), "not an exact task-owned branch") {
		t.Fatalf("blockers = %q", plan.Preview().Blockers)
	}
}

// TestPlanTaskCleanup_ReleaseHotfixOverlapBlocks pins that a task-owned
// hotfix living under the release namespace stays blocked.
func TestPlanTaskCleanup_ReleaseHotfixOverlapBlocks(t *testing.T) {
	mgr, gitMock := taskCleanupTestManager(t)
	mgr.flow.BranchTypes[gitflow.BranchTypeHotfix] = gitflow.BranchTypeRule{Prefixes: []string{"release/hotfix/"}}
	gitMock.listWorktreesRes[0].Branch = "refs/heads/release/hotfix/APP-1"
	base := gitMock.resolveRefFn
	gitMock.resolveRefFn = func(repo, ref string) (string, error) {
		if ref == "refs/heads/release/hotfix/APP-1" {
			return taskCleanupTaskSHA, nil
		}
		return base(repo, ref)
	}

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Blocked() || len(plan.steps) != 0 {
		t.Fatalf("blocked = %v, steps = %+v", plan.Blocked(), plan.steps)
	}
}

func TestPlanTaskCleanup_ProductionTargetedFeatureWithoutManifestAuthorizes(t *testing.T) {
	mgr, _ := taskCleanupTestManager(t)

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Blocked() {
		t.Fatalf("blockers = %q", plan.Preview().Blockers)
	}
	if plan.Preview().Services[0].ReleasedBy != "" {
		t.Fatalf("ReleasedBy = %q, want live ancestry only", plan.Preview().Services[0].ReleasedBy)
	}
	if len(plan.steps) == 0 {
		t.Fatal("expected authorized local steps")
	}
}

func TestPlanTaskCleanup_IntegrationOnlyFeatureWithoutManifestBlocks(t *testing.T) {
	mgr, _ := taskCleanupTestManager(t)
	useIntegrationOnlyFeatureFlow(mgr)

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Blocked() || len(plan.steps) != 0 {
		t.Fatalf("blocked = %v, steps = %+v", plan.Blocked(), plan.steps)
	}
	if !strings.Contains(strings.Join(plan.Preview().Blockers, "\n"), "released manifest") {
		t.Fatalf("blockers = %q", plan.Preview().Blockers)
	}
}

func TestPlanTaskCleanup_DivergentRemoteOmitsCandidatePreservesLocal(t *testing.T) {
	mgr, gitMock := taskCleanupTestManager(t)
	const divergentSHA = "9999999999999999999999999999999999999999"
	base := gitMock.remoteRefSHAFn
	gitMock.remoteRefSHAFn = func(repo, ref string) (string, error) {
		if ref == "refs/heads/feature/APP-1" {
			return divergentSHA, nil
		}
		return base(repo, ref)
	}

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Blocked() {
		t.Fatalf("divergent remote must not block local cleanup: %q", plan.Preview().Blockers)
	}
	if len(plan.Preview().Remote) != 0 {
		t.Fatalf("remote candidates = %+v", plan.Preview().Remote)
	}
	if len(plan.Preview().DeferredRemote) != 1 || plan.Preview().DeferredRemote[0].Branch != "feature/APP-1" || plan.Preview().DeferredRemote[0].ExpectedSHA != divergentSHA {
		t.Fatalf("deferred remote = %+v", plan.Preview().DeferredRemote)
	}
	kinds := map[releaseCleanupStepKind]int{}
	for _, step := range plan.steps {
		kinds[step.kind]++
	}
	if kinds[cleanupTaskWorktree] != 1 || kinds[cleanupLocalTaskBranch] != 1 || kinds[cleanupTaskDirectory] != 1 {
		t.Fatalf("local step kinds = %+v", kinds)
	}
}

func TestPlanTaskCleanup_ActiveNonReleasedManifestBlocks(t *testing.T) {
	mgr, _ := taskCleanupTestManager(t)
	useIntegrationOnlyFeatureFlow(mgr)
	writeTaskCleanupReleaseManifest(t, mgr, domain.ReleaseStatusPrepared)

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Blocked() || len(plan.steps) != 0 {
		t.Fatalf("blocked = %v, steps = %+v", plan.Blocked(), plan.steps)
	}
	if !strings.Contains(strings.Join(plan.Preview().Blockers, "\n"), "non-released") {
		t.Fatalf("blockers = %q", plan.Preview().Blockers)
	}
}

func TestPlanTaskCleanup_ReleasedManifestProvesFeatureCompletion(t *testing.T) {
	mgr, _ := taskCleanupTestManager(t)
	useIntegrationOnlyFeatureFlow(mgr)
	writeTaskCleanupReleaseManifest(t, mgr, domain.ReleaseStatusReleased)

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Blocked() {
		t.Fatalf("blockers = %q", plan.Preview().Blockers)
	}
	if plan.Preview().Services[0].ReleasedBy != "rel-1" {
		t.Fatalf("services = %+v", plan.Preview().Services)
	}
	if len(plan.proofs) != 1 || plan.proofs[0].ReleaseID != "rel-1" {
		t.Fatalf("proofs = %+v", plan.proofs)
	}
}

func TestPlanTaskCleanup_ReleasedManifestIdentityMismatchBlocks(t *testing.T) {
	mgr, _ := taskCleanupTestManager(t)
	useIntegrationOnlyFeatureFlow(mgr)
	writeTaskCleanupReleaseManifest(t, mgr, domain.ReleaseStatusReleased)
	release, err := mgr.loadReleaseManifest("rel-1")
	if err != nil {
		t.Fatal(err)
	}
	release.Services[0].FeatureBranches[0].MergeRef = strings.Repeat("f", 40)
	if _, err := mgr.writeReleaseManifest(release); err != nil {
		t.Fatal(err)
	}

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Blocked() || len(plan.steps) != 0 {
		t.Fatalf("blocked = %v, steps = %+v", plan.Blocked(), plan.steps)
	}
	if !strings.Contains(strings.Join(plan.Preview().Blockers, "\n"), "identity") {
		t.Fatalf("blockers = %q", plan.Preview().Blockers)
	}
}

func TestPlanTaskCleanup_ActiveNonReleasedManifestBlocksProductionTargetedFeature(t *testing.T) {
	mgr, _ := taskCleanupTestManager(t)
	writeTaskCleanupReleaseManifest(t, mgr, domain.ReleaseStatusPrepared)

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Blocked() || len(plan.steps) != 0 {
		t.Fatalf("blocked = %v, steps = %+v", plan.Blocked(), plan.steps)
	}
	if !strings.Contains(strings.Join(plan.Preview().Blockers, "\n"), "non-released") {
		t.Fatalf("blockers = %q", plan.Preview().Blockers)
	}
}

func TestPlanTaskCleanup_ReleasedSquashManifestUsesHeadSHASourceAndMergeRefIntegrated(t *testing.T) {
	mgr, gitMock := taskCleanupTestManager(t)
	useIntegrationOnlyFeatureFlow(mgr)
	writeTaskCleanupMRReleaseManifest(t, mgr, domain.ReleaseStatusReleased, domain.ReleaseFeatureBranch{
		TaskID: "APP-1", ServiceName: "svc", Branch: "feature/APP-1",
		WorktreePath: filepath.Join(mgr.cfg.TasksRoot, "APP-1", "svc"),
		Merged:       true, MergeRef: taskCleanupSquashSHA,
		TaskMergeStatus: "merged", TaskMergeMRNumber: 7, TaskMergeHeadSHA: taskCleanupTaskSHA,
	})
	gitMock.isAncestorFn = func(_, ancestor, _ string) (bool, error) {
		if ancestor == taskCleanupTaskSHA {
			return false, nil
		}
		return true, nil
	}

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Blocked() {
		t.Fatalf("blockers = %q", plan.Preview().Blockers)
	}
	if plan.Preview().Services[0].ReleasedBy != "rel-1" {
		t.Fatalf("services = %+v", plan.Preview().Services)
	}
	if len(plan.proofs) != 1 || plan.proofs[0].IntegratedSHA != taskCleanupSquashSHA {
		t.Fatalf("proofs = %+v", plan.proofs)
	}
	found := false
	for _, call := range gitMock.isAncestorCalls {
		if call.Ancestor == taskCleanupSquashSHA {
			found = true
		}
		if call.Ancestor == taskCleanupTaskSHA {
			t.Fatal("source SHA ancestry required for squash-merged manifest")
		}
	}
	if !found {
		t.Fatal("integrated identity ancestry check not called")
	}
}

func TestPlanTaskCleanup_PartialMRManifestBlocksWithoutFallback(t *testing.T) {
	mgr, _ := taskCleanupTestManager(t)
	useIntegrationOnlyFeatureFlow(mgr)
	writeTaskCleanupMRReleaseManifest(t, mgr, domain.ReleaseStatusReleased, domain.ReleaseFeatureBranch{
		TaskID: "APP-1", ServiceName: "svc", Branch: "feature/APP-1",
		WorktreePath: filepath.Join(mgr.cfg.TasksRoot, "APP-1", "svc"),
		Merged:       true, MergeRef: taskCleanupTaskSHA, TaskMergeMRNumber: 7,
	})

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Blocked() || len(plan.steps) != 0 {
		t.Fatalf("blocked = %v, steps = %+v", plan.Blocked(), plan.steps)
	}
	if !strings.Contains(strings.Join(plan.Preview().Blockers, "\n"), "MR head identity") {
		t.Fatalf("blockers = %q", plan.Preview().Blockers)
	}
}

func TestPlanTaskCleanup_MRHeadSHAMismatchBlocks(t *testing.T) {
	mgr, _ := taskCleanupTestManager(t)
	useIntegrationOnlyFeatureFlow(mgr)
	writeTaskCleanupMRReleaseManifest(t, mgr, domain.ReleaseStatusReleased, domain.ReleaseFeatureBranch{
		TaskID: "APP-1", ServiceName: "svc", Branch: "feature/APP-1",
		WorktreePath: filepath.Join(mgr.cfg.TasksRoot, "APP-1", "svc"),
		Merged:       true, MergeRef: taskCleanupSquashSHA,
		TaskMergeStatus: "merged", TaskMergeMRNumber: 7, TaskMergeHeadSHA: strings.Repeat("f", 40),
	})

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Blocked() || len(plan.steps) != 0 {
		t.Fatalf("blocked = %v, steps = %+v", plan.Blocked(), plan.steps)
	}
	if !strings.Contains(strings.Join(plan.Preview().Blockers, "\n"), "identity mismatch") {
		t.Fatalf("blockers = %q", plan.Preview().Blockers)
	}
}

func TestPlanTaskCleanup_RemoteTargetObjectFetchFailsClosed(t *testing.T) {
	mgr, gitMock := taskCleanupTestManager(t)
	gitMock.ensureCommitFn = func(_, _ string) error { return errors.New("object fetch failed") }

	_, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err == nil {
		t.Fatal("expected fail-closed error when remote target object is unavailable")
	}
}

func TestPlanTaskCleanup_EnsuresRemoteTargetObjectBeforeAncestry(t *testing.T) {
	mgr, gitMock := taskCleanupTestManager(t)
	var fetched []string
	gitMock.ensureCommitFn = func(_, sha string) error {
		fetched = append(fetched, sha)
		return nil
	}

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Blocked() {
		t.Fatalf("blockers = %q", plan.Preview().Blockers)
	}
	if len(fetched) != 1 || fetched[0] != taskCleanupMasterSHA {
		t.Fatalf("fetched objects = %q", fetched)
	}
}

func TestPlanTaskCleanup_CorruptManifestFailsClosed(t *testing.T) {
	mgr, _ := taskCleanupTestManager(t)
	corruptDir := filepath.Join(mgr.releasesRootDir(), "rel-corrupt")
	if err := os.MkdirAll(corruptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(corruptDir, releaseManifestFileName), []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Blocked() || len(plan.steps) != 0 {
		t.Fatalf("blocked = %v, steps = %+v", plan.Blocked(), plan.steps)
	}
	if !strings.Contains(strings.Join(plan.Preview().Blockers, "\n"), "corrupt") {
		t.Fatalf("blockers = %q", plan.Preview().Blockers)
	}
}

func TestPlanTaskCleanup_UnknownTask(t *testing.T) {
	mgr, _ := taskCleanupTestManager(t)
	if _, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "MISSING"}); err == nil {
		t.Fatal("expected error for unknown task")
	}
}

// writeTaskCleanupMRReleaseManifest writes a release manifest with an
// MR-backed feature branch so cleanup proof must honor source (head SHA) and
// integrated (merge ref) identities separately.
func writeTaskCleanupMRReleaseManifest(t *testing.T, mgr *manager, status domain.ReleaseStatus, fb domain.ReleaseFeatureBranch) {
	t.Helper()
	release := domain.Release{
		ManifestVersion: releaseManifestVersion, ID: "rel-1", Dir: filepath.Join(mgr.releasesRootDir(), "rel-1"), Status: status, TaskIDs: []string{"APP-1"},
		Tasks: []domain.ReleaseTaskRef{{TaskID: "APP-1", TaskDir: filepath.Join(mgr.cfg.TasksRoot, "APP-1"), ServiceNames: []string{"svc"}}},
		Services: []domain.ReleaseService{{
			Name: "svc", RepoPath: filepath.Join(mgr.cfg.RootDir, "svc"), IntegrationBranch: "develop", ReleaseBranch: "release/1.0.0", Tag: "v1.0.0", ReleaseSHA: taskCleanupReleaseSHA, AcceptedMergeSHA: taskCleanupAccepted, PushedTag: true,
			FeatureBranches: []domain.ReleaseFeatureBranch{fb},
		}},
	}
	if _, err := mgr.writeReleaseManifest(release); err != nil {
		t.Fatal(err)
	}
}

func taskCleanupTestManager(t *testing.T) (*manager, *mockGitClient) {
	t.Helper()
	return taskCleanupTestManagerIn(t, t.TempDir())
}

// taskCleanupTestManagerIn builds a manager over root so tests can simulate a
// process restart by constructing a second manager over the same on-disk
// state.
func taskCleanupTestManagerIn(t *testing.T, root string) (*manager, *mockGitClient) {
	t.Helper()
	tasksRoot := filepath.Join(root, ".tasks")
	releaseRoot := filepath.Join(root, ".releases")
	repo := filepath.Join(root, "svc")
	worktree := filepath.Join(tasksRoot, "APP-1", "svc")
	for _, dir := range []string{repo, worktree} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{RootDir: root, TasksRoot: tasksRoot, BaseBranch: "develop", Release: &config.ReleaseConfig{RootDir: releaseRoot}}
	flow := &gitflow.ResolvedGitFlow{ProductionBranch: "master", IntegrationBranch: "develop", BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
		gitflow.BranchTypeFeature: {Prefixes: []string{"feature/"}, MergeTargets: []string{"master"}, CloseStrategy: gitflow.CloseStrategyDirectMerge},
		gitflow.BranchTypeHotfix:  {Prefixes: []string{"hotfix/"}},
		gitflow.BranchTypeRelease: {Prefixes: []string{"release/"}},
	}}
	gitMock := &mockGitClient{commonDirResult: filepath.Join(repo, ".git")}
	gitMock.resolveRefFn = func(_ string, ref string) (string, error) {
		switch ref {
		case "refs/heads/feature/APP-1":
			return taskCleanupTaskSHA, nil
		case "refs/tags/v1.0.0^{}":
			return taskCleanupAccepted, nil
		}
		return "", nil
	}
	gitMock.remoteRefSHAFn = func(_ string, ref string) (string, error) {
		switch ref {
		case "refs/heads/feature/APP-1":
			return taskCleanupTaskSHA, nil
		case "refs/heads/develop":
			return taskCleanupDevelopSHA, nil
		case "refs/heads/master":
			return taskCleanupMasterSHA, nil
		case "refs/tags/v1.0.0^{}":
			return taskCleanupAccepted, nil
		}
		return "", nil
	}
	gitMock.isAncestorFn = func(_, _, _ string) (bool, error) { return true, nil }
	gitMock.remoteURLRes = "git@example.com:group/svc.git"
	gitMock.listWorktreesRes = []git.WorktreeEntry{
		{Path: worktree, HEAD: taskCleanupTaskSHA, Branch: "refs/heads/feature/APP-1"},
	}
	resolver := cleanupResolver{repo: repo}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &manager{cfg: cfg, git: gitMock, discoverer: resolver, flow: flow, logger: logger}, gitMock
}

// useIntegrationOnlyFeatureFlow resolves the feature close target to the
// integration branch only, so completion cannot be proven from live ancestry
// against production and the released-manifest gate applies.
func useIntegrationOnlyFeatureFlow(mgr *manager) {
	mgr.flow.BranchTypes[gitflow.BranchTypeFeature] = gitflow.BranchTypeRule{
		Prefixes:     []string{"feature/"},
		MergeTargets: []string{"develop"},
	}
}

// TestPlanTaskCleanup_UnprovenClosePostActionsBlockCleanup pins that a
// resolved branch rule requiring close post-actions (tag or pipeline) blocks
// cleanup unless the request carries proof of a successful close.
func TestPlanTaskCleanup_UnprovenClosePostActionsBlockCleanup(t *testing.T) {
	useTagOnCloseFlow := func(mgr *manager) {
		mgr.flow.BranchTypes[gitflow.BranchTypeFeature] = gitflow.BranchTypeRule{
			Prefixes:      []string{"feature/"},
			MergeTargets:  []string{"master"},
			CloseStrategy: gitflow.CloseStrategyDirectMerge,
			TagOnClose:    true,
		}
	}
	assertBlocked := func(t *testing.T, plan TaskCleanupPlan) {
		t.Helper()
		if !plan.Blocked() {
			t.Fatal("unproven close post-actions authorized cleanup")
		}
		if !strings.Contains(strings.Join(plan.Preview().Blockers, ";"), "post-actions") {
			t.Fatalf("blockers = %q", plan.Preview().Blockers)
		}
		if len(plan.steps) != 0 {
			t.Fatalf("blocked plan authorized steps: %+v", plan.steps)
		}
	}

	t.Run("tag on close without proof blocks", func(t *testing.T) {
		mgr, _ := taskCleanupTestManager(t)
		useTagOnCloseFlow(mgr)

		plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
		if err != nil {
			t.Fatal(err)
		}
		assertBlocked(t, plan)
	})

	t.Run("pipeline on close without proof blocks", func(t *testing.T) {
		mgr, _ := taskCleanupTestManager(t)
		mgr.flow.BranchTypes[gitflow.BranchTypeFeature] = gitflow.BranchTypeRule{
			Prefixes:               []string{"feature/"},
			MergeTargets:           []string{"master"},
			CloseStrategy:          gitflow.CloseStrategyDirectMerge,
			TriggerPipelineOnClose: true,
		}

		plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
		if err != nil {
			t.Fatal(err)
		}
		assertBlocked(t, plan)
	})

	t.Run("durable proof authorizes cleanup with tag on close", func(t *testing.T) {
		mgr, _ := taskCleanupTestManager(t)
		useTagOnCloseFlow(mgr)
		proveClosePostActionsForTest(t, mgr, closePostActionTag)

		plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
		if err != nil {
			t.Fatal(err)
		}
		if plan.Blocked() {
			t.Fatalf("proven close blocked: %q", plan.Preview().Blockers)
		}
		if len(plan.steps) == 0 {
			t.Fatal("proven cleanup authorized no steps")
		}
	})
}

// TestExecuteTaskCleanup_ProvenClosePostActionsPlanExecutes pins that execute
// revalidation re-plans from the durable on-disk proof, so a plan proven by a
// successful close still matches its fresh revalidation under a rule with
// tag-on-close configured, and that executing the final task-directory step
// removes the proof together with the other task metadata.
func TestExecuteTaskCleanup_ProvenClosePostActionsPlanExecutes(t *testing.T) {
	mgr, gitMock := taskCleanupTestManager(t)
	mgr.flow.BranchTypes[gitflow.BranchTypeFeature] = gitflow.BranchTypeRule{
		Prefixes:      []string{"feature/"},
		MergeTargets:  []string{"master"},
		CloseStrategy: gitflow.CloseStrategyDirectMerge,
		TagOnClose:    true,
	}
	gitMock.branchExistsRes = true
	gitMock.deleteBranchIfUnchangedFn = func(_, _, _ string) error { return nil }
	gitMock.listWorktreesFn = func(string) ([]git.WorktreeEntry, error) {
		return gitMock.listWorktreesRes, nil
	}
	gitMock.removeWorktreeFn = func(_, path string, _ bool) error {
		for i, entry := range gitMock.listWorktreesRes {
			if entry.Path == path {
				gitMock.listWorktreesRes = append(gitMock.listWorktreesRes[:i], gitMock.listWorktreesRes[i+1:]...)
				break
			}
		}
		return os.RemoveAll(path)
	}
	proveClosePostActionsForTest(t, mgr, closePostActionTag)

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Blocked() {
		t.Fatalf("proven close blocked: %q", plan.Preview().Blockers)
	}

	result, err := mgr.ExecuteTaskCleanup(t.Context(), plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Completed) == 0 {
		t.Fatal("proven cleanup completed no steps")
	}
	if _, err := os.Stat(filepath.Join(mgr.taskDir("APP-1"), closePostActionsProofFileName)); !os.IsNotExist(err) {
		t.Fatalf("executed cleanup retained close post-actions proof: %v", err)
	}
}

// proveClosePostActionsForTest records durable close post-action proof for the
// taskCleanupTestManager fixture service, standing in for a successful close.
func proveClosePostActionsForTest(t *testing.T, mgr *manager, actions ...string) {
	t.Helper()
	svc := domain.Service{
		Name:      "svc",
		RepoPath:  filepath.Join(mgr.cfg.RootDir, "svc"),
		Branch:    "feature/APP-1",
		RemoteURL: "git@example.com:group/svc.git",
	}
	sha, err := mgr.git.ResolveRef(t.Context(), svc.RepoPath, "refs/heads/"+svc.Branch)
	if err != nil || sha == "" {
		t.Fatalf("resolve source SHA: %q %v", sha, err)
	}
	for _, action := range actions {
		if err := mgr.proveClosePostAction(t.Context(), "APP-1", svc, action, sha); err != nil {
			t.Fatal(err)
		}
	}
}

// TestPlanTaskCleanup_DurableClosePostActionsProof pins that cleanup
// authorization is bound to the on-disk proof: it survives process restart,
// invalidates on source identity or action config change, fails closed on
// corrupt or future-versioned proof, is retained through blocked inspection,
// and binds the verified proof into the plan fingerprint.
func TestPlanTaskCleanup_DurableClosePostActionsProof(t *testing.T) {
	useTagOnCloseFlow := func(mgr *manager) {
		mgr.flow.BranchTypes[gitflow.BranchTypeFeature] = gitflow.BranchTypeRule{
			Prefixes:      []string{"feature/"},
			MergeTargets:  []string{"master"},
			CloseStrategy: gitflow.CloseStrategyDirectMerge,
			TagOnClose:    true,
		}
	}
	postActionsBlocked := func(t *testing.T, plan TaskCleanupPlan) {
		t.Helper()
		if !plan.Blocked() || len(plan.steps) != 0 {
			t.Fatalf("blocked = %v, steps = %+v", plan.Blocked(), plan.steps)
		}
		if !strings.Contains(strings.Join(plan.Preview().Blockers, ";"), "post-actions") {
			t.Fatalf("blockers = %q", plan.Preview().Blockers)
		}
	}

	t.Run("proof survives restart", func(t *testing.T) {
		root := t.TempDir()
		mgr, _ := taskCleanupTestManagerIn(t, root)
		useTagOnCloseFlow(mgr)
		proveClosePostActionsForTest(t, mgr, closePostActionTag)

		restarted, _ := taskCleanupTestManagerIn(t, root)
		useTagOnCloseFlow(restarted)
		plan, err := restarted.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
		if err != nil {
			t.Fatal(err)
		}
		if plan.Blocked() {
			t.Fatalf("durable proof did not survive restart: %q", plan.Preview().Blockers)
		}
	})

	t.Run("changed source identity invalidates", func(t *testing.T) {
		mgr, gitMock := taskCleanupTestManager(t)
		useTagOnCloseFlow(mgr)
		proveClosePostActionsForTest(t, mgr, closePostActionTag)

		base := gitMock.resolveRefFn
		gitMock.resolveRefFn = func(repo, ref string) (string, error) {
			if ref == "refs/heads/feature/APP-1" {
				return strings.Repeat("f", 40), nil
			}
			return base(repo, ref)
		}

		plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
		if err != nil {
			t.Fatal(err)
		}
		postActionsBlocked(t, plan)
	})

	t.Run("changed remote URL invalidates", func(t *testing.T) {
		mgr, gitMock := taskCleanupTestManager(t)
		useTagOnCloseFlow(mgr)
		proveClosePostActionsForTest(t, mgr, closePostActionTag)
		gitMock.remoteURLRes = "git@example.com:group/other.git"

		plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
		if err != nil {
			t.Fatal(err)
		}
		postActionsBlocked(t, plan)
	})

	t.Run("changed action config invalidates", func(t *testing.T) {
		mgr, _ := taskCleanupTestManager(t)
		useTagOnCloseFlow(mgr)
		proveClosePostActionsForTest(t, mgr, closePostActionTag)

		mgr.cfg.Tag = &config.TagConfig{Format: "r{{.Version}}"}

		plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
		if err != nil {
			t.Fatal(err)
		}
		postActionsBlocked(t, plan)
	})

	t.Run("future proof version fails closed", func(t *testing.T) {
		mgr, _ := taskCleanupTestManager(t)
		useTagOnCloseFlow(mgr)
		proofPath := filepath.Join(mgr.taskDir("APP-1"), closePostActionsProofFileName)
		if err := os.WriteFile(proofPath, []byte(`{"version":999,"services":[]}`), 0o600); err != nil {
			t.Fatal(err)
		}

		plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
		if err != nil {
			t.Fatal(err)
		}
		postActionsBlocked(t, plan)
	})

	t.Run("corrupt proof fails closed", func(t *testing.T) {
		mgr, _ := taskCleanupTestManager(t)
		useTagOnCloseFlow(mgr)
		proofPath := filepath.Join(mgr.taskDir("APP-1"), closePostActionsProofFileName)
		if err := os.WriteFile(proofPath, []byte("{not-json"), 0o600); err != nil {
			t.Fatal(err)
		}

		plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
		if err != nil {
			t.Fatal(err)
		}
		postActionsBlocked(t, plan)
	})

	t.Run("blocked inspection retains proof for retry", func(t *testing.T) {
		mgr, gitMock := taskCleanupTestManager(t)
		useTagOnCloseFlow(mgr)
		proveClosePostActionsForTest(t, mgr, closePostActionTag)
		gitMock.isDirtyRes = true

		plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
		if err != nil {
			t.Fatal(err)
		}
		if !plan.Blocked() {
			t.Fatal("dirty worktree must block")
		}
		proofPath := filepath.Join(mgr.taskDir("APP-1"), closePostActionsProofFileName)
		if _, err := os.Stat(proofPath); err != nil {
			t.Fatalf("blocked inspection dropped proof: %v", err)
		}

		gitMock.isDirtyRes = false
		retry, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
		if err != nil {
			t.Fatal(err)
		}
		if retry.Blocked() {
			t.Fatalf("retried inspection lost authorization: %q", retry.Preview().Blockers)
		}
	})

	t.Run("verified proof binds plan fingerprint", func(t *testing.T) {
		root := t.TempDir()
		mgr, _ := taskCleanupTestManagerIn(t, root)
		useTagOnCloseFlow(mgr)

		blocked, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
		if err != nil {
			t.Fatal(err)
		}
		proveClosePostActionsForTest(t, mgr, closePostActionTag)
		proven, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
		if err != nil {
			t.Fatal(err)
		}
		if blocked.Fingerprint() == proven.Fingerprint() {
			t.Fatal("fingerprint unchanged after proof authorization")
		}

		again, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
		if err != nil {
			t.Fatal(err)
		}
		if again.Fingerprint() != proven.Fingerprint() {
			t.Fatal("fingerprint unstable across revalidation of unchanged state")
		}

		mgr.cfg.Tag = &config.TagConfig{Format: "r{{.Version}}"}
		invalidated, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
		if err != nil {
			t.Fatal(err)
		}
		if invalidated.Fingerprint() == proven.Fingerprint() {
			t.Fatal("fingerprint unchanged after proof invalidation")
		}
	})
}

func writeTaskCleanupReleaseManifest(t *testing.T, mgr *manager, status domain.ReleaseStatus) {
	t.Helper()
	release := domain.Release{
		ManifestVersion: releaseManifestVersion, ID: "rel-1", Dir: filepath.Join(mgr.releasesRootDir(), "rel-1"), Status: status, TaskIDs: []string{"APP-1"},
		Tasks: []domain.ReleaseTaskRef{{TaskID: "APP-1", TaskDir: filepath.Join(mgr.cfg.TasksRoot, "APP-1"), ServiceNames: []string{"svc"}}},
		Services: []domain.ReleaseService{{
			Name: "svc", RepoPath: filepath.Join(mgr.cfg.RootDir, "svc"), IntegrationBranch: "develop", ReleaseBranch: "release/1.0.0", Tag: "v1.0.0", ReleaseSHA: taskCleanupReleaseSHA, AcceptedMergeSHA: taskCleanupAccepted, PushedTag: true,
			FeatureBranches: []domain.ReleaseFeatureBranch{{TaskID: "APP-1", ServiceName: "svc", Branch: "feature/APP-1", WorktreePath: filepath.Join(mgr.cfg.TasksRoot, "APP-1", "svc"), Merged: true, MergeRef: taskCleanupTaskSHA}},
		}},
	}
	if _, err := mgr.writeReleaseManifest(release); err != nil {
		t.Fatal(err)
	}
}
