package task

import (
	"errors"
	"slices"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
)

func TestReleaseMerge_DirectTargetMutationFailsClosed(t *testing.T) {
	// Given
	m, g, client := newReleaseMergeTestManager(t)
	mr := openProductionMR(1, "api", "api-source")
	mr.SupportsTargetBinding = false
	client.readiness = map[int]forge.MRReadiness{1: mr}
	configureDirectMergeGit(t, g, "origin/master-sha", "api-source")
	tip := "origin/master-sha"
	g.remoteRefSHAAtFn = func(_, _, _ string) (string, error) { return tip, nil }
	g.pushRefWithLeaseFn = func(_, _, _, _, _, _ string) error {
		tip = "competing-target"
		return errors.New("stale target lease")
	}
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, releaseMergeService("api", 1))

	// When
	got, result, err := m.MergeReleaseMRs(t.Context(), release.ID, nil)

	// Then
	if err != nil || !slices.Equal(result.Failed, []string{"api"}) || got.Services[0].AcceptedMergeSHA != "" || len(client.merges) != 0 || tip != "competing-target" {
		t.Fatalf("release=%+v result=%+v err=%v server merges=%v", got, result, err, client.merges)
	}
}

func TestReleaseMerge_DirectRestartReconcilesOpenMR(t *testing.T) {
	// Given: the direct push succeeded before release acceptance was persisted.
	m, g, client := newReleaseMergeTestManager(t)
	mr := openProductionMR(1, "api", "api-source")
	mr.SupportsTargetBinding = false
	client.readiness = map[int]forge.MRReadiness{1: mr}
	configureDirectMergeGit(t, g, "origin/master-sha", "api-source")
	g.resolveRefFn = func(repo, ref string) (string, error) {
		return g.remoteRefSHAAtFn(repo, g.remoteURLRes, ref)
	}
	release := writePromoteRelease(t, m, domain.ReleaseStatusAwaitingMasterMerge, releaseMergeService("api", 1))
	_, err := m.mergeMRWithLease(t.Context(), release.Services[0].RepoPath, forge.MergeMRParams{
		WorktreePath: release.Services[0].RepoPath, Repo: forge.ExtractRepoPath(g.remoteURLRes), Number: 1,
		Method: m.releaseMergeMethod(), ExpectedHeadSHA: mr.HeadSHA, ExpectedTargetBranch: "master", ExpectedTargetSHA: "origin/master-sha",
	}, client)
	if err != nil {
		t.Fatal(err)
	}
	g.buildMergeCommitFn = func(_, _, _, _ string) (string, error) {
		t.Fatal("restart built another merge")
		return "", nil
	}
	restarted := *m

	// When
	got, result, err := restarted.MergeReleaseMRs(t.Context(), release.ID, nil)

	// Then
	if err != nil || got.Status != domain.ReleaseStatusMasterMerged || got.Services[0].AcceptedMergeSHA != "direct-merge" || !slices.Equal(result.Skipped, []string{"api"}) || len(g.pushRefWithLeaseCalls) != 1 || len(client.merges) != 0 {
		t.Fatalf("release=%+v result=%+v err=%v pushes=%v", got, result, err, g.pushRefWithLeaseCalls)
	}
}

func TestIntegrateReleaseTaskMRs_DirectTargetMutationFailsClosed(t *testing.T) {
	// Given
	x := newTaskMergeSafetyFixture(t, []string{"develop"})
	mr := x.forge.readiness[1]
	mr.SupportsTargetBinding = false
	x.forge.readiness[1] = mr
	g := x.m.git.(*mockGitClient)
	configureDirectMergeGit(t, g, "base-develop", "head-1")
	tip := "base-develop"
	g.remoteRefSHAAtFn = func(_, _, _ string) (string, error) { return tip, nil }
	g.pushRefWithLeaseFn = func(_, _, _, _, _, _ string) error {
		tip = "competing-target"
		return errors.New("stale target lease")
	}

	// When
	err := x.m.integrateReleaseTaskMRs(t.Context(), &x.release, &x.plan, nil)

	// Then
	if err == nil || x.forge.mergeCalls != 0 || x.release.Services[0].PostIntegrationSHA != "" || x.release.Services[0].FeatureBranches[0].Merged || tip != "competing-target" {
		t.Fatalf("release=%+v err=%v server merges=%d", x.release, err, x.forge.mergeCalls)
	}
}

func TestPlanReleaseTaskMergeRetry_DirectRestartReconcilesOpenMR(t *testing.T) {
	// Given
	x := newTaskMergeSafetyFixture(t, []string{"develop"})
	mr := x.forge.readiness[1]
	mr.SupportsTargetBinding = false
	x.forge.readiness[1] = mr
	g := x.m.git.(*mockGitClient)
	configureDirectMergeGit(t, g, "base-develop", "head-1")
	if err := confirmReleaseTaskMergeRows(&x.release, &x.plan); err != nil {
		t.Fatal(err)
	}
	step := x.plan.steps[0]
	_, err := x.m.mergeMRWithLease(t.Context(), step.RepoPath, forge.MergeMRParams{
		WorktreePath: step.WorktreePath, Repo: step.Repo, Number: step.MRNumber,
		Method: step.MergeMethod, ExpectedHeadSHA: step.HeadSHA, ExpectedTargetBranch: step.TargetBranch, ExpectedTargetSHA: step.TargetSHA,
	}, x.forge)
	if err != nil {
		t.Fatal(err)
	}
	x.tips["origin/develop"] = "direct-merge"
	x.release.Status = domain.ReleaseStatusTaskMergePartial
	x.release.Services[0].FeatureBranches[0].TaskMergeStatus = taskMergeStatusUnknown
	if _, err := x.m.writeReleaseManifest(x.release); err != nil {
		t.Fatal(err)
	}
	restarted := *x.m

	// When
	plan, err := restarted.PlanReleaseTaskMergeRetry(t.Context(), x.release.ID)

	// Then
	if err != nil || len(plan.steps) != 1 || plan.steps[0].Status != taskMergeStatusMerged || plan.steps[0].AcceptedSHA != "direct-merge" || !plan.steps[0].Ready || x.forge.mergeCalls != 0 || len(g.pushRefWithLeaseCalls) != 1 {
		t.Fatalf("plan=%+v err=%v pushes=%v", plan, err, g.pushRefWithLeaseCalls)
	}
	if err := restarted.validateReleaseTaskMergeStep(t.Context(), plan.steps[0], map[string]string{}); err != nil {
		t.Fatalf("reconciled retry gate: %v", err)
	}
}

func TestPlanReleaseTaskMerges_DirectProofSkipsOpenMR(t *testing.T) {
	// Given
	x := seedDirectReleaseTaskMerge(t)

	// When
	plan, err := x.m.planReleaseTaskMerges(t.Context(), releasePlanFromRelease(x.release))

	// Then
	if err != nil || len(plan.steps) != 1 || plan.steps[0].Status != taskMergeStatusMerged || !plan.steps[0].Ready || plan.steps[0].AcceptedSHA != "direct-merge" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
}

func TestProveAcceptedTaskPreparation_DirectProofAcceptsOpenMR(t *testing.T) {
	// Given
	x := seedDirectReleaseTaskMerge(t)
	svc := &x.release.Services[0]
	svc.PostIntegrationSHA = "direct-merge"
	fb := &svc.FeatureBranches[0]
	fb.TaskMergeStatus, fb.Merged, fb.MergeRef = taskMergeStatusMerged, true, "direct-merge"

	// When
	err := x.m.proveAcceptedTaskPreparation(t.Context(), *svc)

	// Then
	if err != nil {
		t.Fatalf("prove accepted direct merge with open MR: %v", err)
	}
}

func seedDirectReleaseTaskMerge(t *testing.T) *taskMergeSafetyFixture {
	t.Helper()
	x := newTaskMergeSafetyFixture(t, []string{"develop"})
	mr := x.forge.readiness[1]
	mr.SupportsTargetBinding = false
	x.forge.readiness[1] = mr
	g := x.m.git.(*mockGitClient)
	configureDirectMergeGit(t, g, "base-develop", "head-1")
	if err := confirmReleaseTaskMergeRows(&x.release, &x.plan); err != nil {
		t.Fatal(err)
	}
	step := x.plan.steps[0]
	if _, err := x.m.mergeMRWithLease(t.Context(), step.RepoPath, forge.MergeMRParams{
		WorktreePath: step.WorktreePath, Repo: step.Repo, Number: step.MRNumber,
		Method: step.MergeMethod, ExpectedHeadSHA: step.HeadSHA, ExpectedTargetBranch: step.TargetBranch, ExpectedTargetSHA: step.TargetSHA,
	}, x.forge); err != nil {
		t.Fatal(err)
	}
	x.tips["origin/develop"] = "direct-merge"
	return x
}
