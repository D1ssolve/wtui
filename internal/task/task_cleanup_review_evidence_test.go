package task

import (
	"context"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

// cleanupEvidenceForgeClient backs production-targeted review_request cleanup
// tests with forge history plus numbered MR detail.
type cleanupEvidenceForgeClient struct {
	history      []forge.MRInfo
	historyCalls int
	historyErr   error
	byNumber     map[int]forge.MRReadiness
	numberCalls  int
}

func (f *cleanupEvidenceForgeClient) Provider() forge.ForgeProvider    { return forge.ForgeProviderGitLab }
func (f *cleanupEvidenceForgeClient) IsAvailable(context.Context) bool { return true }
func (f *cleanupEvidenceForgeClient) CreateMR(context.Context, forge.CreateMRParams) (forge.MRInfo, error) {
	return forge.MRInfo{}, nil
}
func (f *cleanupEvidenceForgeClient) MRStatus(context.Context, string, string) ([]forge.MRInfo, error) {
	return nil, nil
}
func (f *cleanupEvidenceForgeClient) MRReadiness(context.Context, string, string, string) (forge.MRReadiness, error) {
	return forge.MRReadiness{}, nil
}
func (f *cleanupEvidenceForgeClient) MRReadinessByNumber(_ context.Context, number int, _, _ string) (forge.MRReadiness, error) {
	f.numberCalls++
	return f.byNumber[number], nil
}
func (f *cleanupEvidenceForgeClient) MRHistory(context.Context, string, string) ([]forge.MRInfo, error) {
	f.historyCalls++
	return f.history, f.historyErr
}
func (f *cleanupEvidenceForgeClient) MergeMR(context.Context, forge.MergeMRParams) (forge.MRMergeResult, error) {
	return forge.MRMergeResult{}, nil
}
func (f *cleanupEvidenceForgeClient) PipelineStatus(context.Context, string, string) ([]forge.PipelineStatus, error) {
	return nil, nil
}
func (f *cleanupEvidenceForgeClient) TriggerPipeline(context.Context, forge.TriggerPipelineParams) error {
	return nil
}
func (f *cleanupEvidenceForgeClient) ListIssues(context.Context, forge.ListIssuesParams) ([]forge.IssueInfo, error) {
	return nil, nil
}

// useReviewToProductionFlow makes the feature branch type review_request with
// the configured production branch as its single review target.
func useReviewToProductionFlow(mgr *manager) {
	mgr.flow.BranchTypes[gitflow.BranchTypeFeature] = gitflow.BranchTypeRule{
		Prefixes:      []string{"feature/"},
		MergeTargets:  []string{"develop"},
		ReviewTargets: []string{"master"},
		CloseStrategy: gitflow.CloseStrategyReviewRequest,
	}
}

// setupReviewEvidenceCleanup configures a production-targeted review_request
// task whose source SHA is not contained in production (squash/rebase merge)
// and returns the manager plus a forge client carrying merged MR evidence.
func setupReviewEvidenceCleanup(t *testing.T, detail forge.MRReadiness, history ...forge.MRInfo) (*manager, *mockGitClient, *cleanupEvidenceForgeClient) {
	t.Helper()
	mgr, gitMock := taskCleanupTestManager(t)
	useReviewToProductionFlow(mgr)
	gitMock.remoteURLRes = "git@gitlab.com:group/repo.git"
	gitMock.isAncestorFn = func(_, ancestor, _ string) (bool, error) {
		return ancestor != taskCleanupTaskSHA, nil
	}
	client := &cleanupEvidenceForgeClient{
		history:  history,
		byNumber: map[int]forge.MRReadiness{detail.Number: detail},
	}
	mgr.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitLab: client}
	return mgr, gitMock, client
}

func mergedReviewDetail(number int) forge.MRReadiness {
	return forge.MRReadiness{
		Number:       number,
		State:        "merged",
		SourceBranch: "feature/APP-1",
		TargetBranch: "master",
		HeadSHA:      taskCleanupTaskSHA,
		MergedSHA:    taskCleanupSquashSHA,
	}
}

func mergedReviewHistoryRow(number int) forge.MRInfo {
	return forge.MRInfo{
		Number:       number,
		State:        "merged",
		SourceBranch: "feature/APP-1",
		TargetBranch: "master",
	}
}

func TestPlanTaskCleanup_ReviewToProductionSquashReconstructsMergedEvidence(t *testing.T) {
	mgr, gitMock, client := setupReviewEvidenceCleanup(t, mergedReviewDetail(7), mergedReviewHistoryRow(7))

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Blocked() {
		t.Fatalf("blockers = %q", plan.Preview().Blockers)
	}
	if len(plan.proofs) != 1 || plan.proofs[0].IntegratedSHA != taskCleanupSquashSHA {
		t.Fatalf("proofs = %+v, want integrated merged SHA", plan.proofs)
	}
	if plan.proofs[0].SourceSHA != taskCleanupTaskSHA {
		t.Fatalf("proof source = %q, want deletion lease source SHA", plan.proofs[0].SourceSHA)
	}
	if len(plan.proofs[0].Targets) != 1 || plan.proofs[0].Targets[0].ref != "refs/heads/master" {
		t.Fatalf("proof targets = %+v", plan.proofs[0].Targets)
	}
	var branchStep *releaseCleanupStep
	for i := range plan.steps {
		if plan.steps[i].kind == cleanupLocalTaskBranch {
			branchStep = &plan.steps[i]
		}
	}
	if branchStep == nil || branchStep.integratedSHA != taskCleanupSquashSHA || branchStep.expectedSHA != taskCleanupTaskSHA {
		t.Fatalf("branch step = %+v, want source lease plus integrated ancestry", branchStep)
	}
	var fetched []string
	for _, call := range gitMock.ensureCommitCalls {
		fetched = append(fetched, call.SHA)
	}
	if len(fetched) != 2 || fetched[0] != taskCleanupMasterSHA || fetched[1] != taskCleanupSquashSHA {
		t.Fatalf("ensured objects = %q, want target and merged SHAs", fetched)
	}
	if client.historyCalls != 1 || client.numberCalls != 1 {
		t.Fatalf("forge calls = history %d, number %d", client.historyCalls, client.numberCalls)
	}
}

func TestPlanTaskCleanup_ReviewToProductionMergeCommitSkipsForge(t *testing.T) {
	mgr, _, client := setupReviewEvidenceCleanup(t, mergedReviewDetail(7), mergedReviewHistoryRow(7))
	mgr.git.(*mockGitClient).isAncestorFn = func(_, _, _ string) (bool, error) { return true, nil }

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Blocked() {
		t.Fatalf("blockers = %q", plan.Preview().Blockers)
	}
	if len(plan.proofs) != 1 || plan.proofs[0].IntegratedSHA != "" {
		t.Fatalf("proofs = %+v, want source-only ancestry proof", plan.proofs)
	}
	if client.historyCalls != 0 || client.numberCalls != 0 {
		t.Fatal("forge consulted although source ancestry proves the merge")
	}
}

func TestPlanTaskCleanup_ReviewToProductionWrongEvidenceBlocks(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*forge.MRReadiness)
		history []forge.MRInfo
		want    string
	}{
		{
			name:    "wrong target",
			mutate:  func(d *forge.MRReadiness) { d.TargetBranch = "develop" },
			history: []forge.MRInfo{mergedReviewHistoryRow(7)},
			want:    "no merged review evidence",
		},
		{
			name:    "wrong source",
			mutate:  func(d *forge.MRReadiness) { d.SourceBranch = "feature/other" },
			history: []forge.MRInfo{mergedReviewHistoryRow(7)},
			want:    "no merged review evidence",
		},
		{
			name:    "stale head",
			mutate:  func(d *forge.MRReadiness) { d.HeadSHA = strings.Repeat("f", 40) },
			history: []forge.MRInfo{mergedReviewHistoryRow(7)},
			want:    "does not match source",
		},
		{
			name:    "not merged",
			mutate:  func(d *forge.MRReadiness) { d.State = "open" },
			history: []forge.MRInfo{mergedReviewHistoryRow(7)},
			want:    "no merged review evidence",
		},
		{
			name:    "empty merge SHA",
			mutate:  func(d *forge.MRReadiness) { d.MergedSHA = "" },
			history: []forge.MRInfo{mergedReviewHistoryRow(7)},
			want:    "no merge SHA",
		},
		{
			name:    "ambiguous history",
			history: []forge.MRInfo{mergedReviewHistoryRow(7), mergedReviewHistoryRow(8)},
			want:    "ambiguous",
		},
		{
			name:    "no merged history",
			history: []forge.MRInfo{{Number: 3, State: "closed", SourceBranch: "feature/APP-1", TargetBranch: "master"}},
			want:    "no merged review evidence",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			detail := mergedReviewDetail(7)
			if tc.mutate != nil {
				tc.mutate(&detail)
			}
			mgr, _, _ := setupReviewEvidenceCleanup(t, detail, tc.history...)

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

func TestPlanTaskCleanup_ReviewToProductionUnrelatedHistoryIgnored(t *testing.T) {
	unrelated := []forge.MRInfo{
		{Number: 1, State: "merged", SourceBranch: "feature/other", TargetBranch: "master"},
		{Number: 2, State: "merged", SourceBranch: "feature/APP-1", TargetBranch: "develop"},
		{Number: 3, State: "open", SourceBranch: "feature/APP-1", TargetBranch: "master"},
	}
	mgr, _, _ := setupReviewEvidenceCleanup(t, mergedReviewDetail(7), append(unrelated, mergedReviewHistoryRow(7))...)

	plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Blocked() || len(plan.proofs) != 1 || plan.proofs[0].IntegratedSHA != taskCleanupSquashSHA {
		t.Fatalf("blocked = %v, proofs = %+v", plan.Blocked(), plan.proofs)
	}
}

func TestPlanTaskCleanup_ReviewToProductionTamperedProofBlocks(t *testing.T) {
	mgr, gitMock, _ := setupReviewEvidenceCleanup(t, mergedReviewDetail(7), mergedReviewHistoryRow(7))
	gitMock.isAncestorFn = func(_, ancestor, _ string) (bool, error) { return false, nil }

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
}

// useMultiTargetReviewFlow configures the feature branch type as
// review_request into both master and develop so every review target must be
// proven independently.
func useMultiTargetReviewFlow(mgr *manager) {
	mgr.flow.BranchTypes[gitflow.BranchTypeFeature] = gitflow.BranchTypeRule{
		Prefixes:      []string{"feature/"},
		MergeTargets:  []string{"develop"},
		ReviewTargets: []string{"master", "develop"},
		CloseStrategy: gitflow.CloseStrategyReviewRequest,
	}
}

// Squash/rebase evidence is resolved per review target: a target holding the
// source via merge commit needs no forge evidence, while a squash-merged
// target reconstructs its own integrated SHA authoritatively.
func TestPlanTaskCleanup_MultiTargetReviewSquashUsesPerTargetEvidence(t *testing.T) {
	mgr, gitMock, client := setupReviewEvidenceCleanup(t, mergedReviewDetail(7), mergedReviewHistoryRow(7))
	useMultiTargetReviewFlow(mgr)
	gitMock.isAncestorFn = func(_, ancestor, descendant string) (bool, error) {
		if descendant == taskCleanupDevelopSHA {
			return true, nil // develop holds the exact source via merge commit
		}
		return ancestor == taskCleanupSquashSHA, nil
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
	byRef := map[string]releaseCleanupTarget{}
	for _, target := range plan.proofs[0].Targets {
		byRef[target.ref] = target
	}
	master, ok := byRef["refs/heads/master"]
	if !ok || master.integratedSHA != taskCleanupSquashSHA {
		t.Fatalf("master target = %+v, want reconstructed integrated SHA", master)
	}
	develop, ok := byRef["refs/heads/develop"]
	if !ok || develop.integratedSHA != "" {
		t.Fatalf("develop target = %+v, want ancestry-only proof without integrated SHA", develop)
	}
	if plan.proofs[0].IntegratedSHA != "" {
		t.Fatalf("service-level integrated identity = %q, want per-target only for multi-target proofs", plan.proofs[0].IntegratedSHA)
	}
	if client.historyCalls != 1 || client.numberCalls != 1 {
		t.Fatalf("forge calls = history %d, number %d, want one history plus one numbered detail", client.historyCalls, client.numberCalls)
	}
}

// Squash merges into every review target record one authoritative integrated
// SHA per target; no service-level single identity can represent both.
func TestPlanTaskCleanup_MultiTargetReviewSquashEveryTargetRecordsDistinctIntegratedSHAs(t *testing.T) {
	const developSquashSHA = "8888888888888888888888888888888888888888"
	masterDetail := mergedReviewDetail(7)
	developDetail := mergedReviewDetail(8)
	developDetail.TargetBranch = "develop"
	developDetail.MergedSHA = developSquashSHA
	mgr, gitMock, client := setupReviewEvidenceCleanup(t, masterDetail,
		mergedReviewHistoryRow(7), forge.MRInfo{Number: 8, State: "merged", SourceBranch: "feature/APP-1", TargetBranch: "develop"})
	useMultiTargetReviewFlow(mgr)
	client.byNumber[8] = developDetail
	gitMock.isAncestorFn = func(_, ancestor, _ string) (bool, error) { return ancestor != taskCleanupTaskSHA, nil }

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
		byRef[target.ref] = target.integratedSHA
	}
	if byRef["refs/heads/master"] != taskCleanupSquashSHA || byRef["refs/heads/develop"] != developSquashSHA {
		t.Fatalf("per-target integrated SHAs = %+v", byRef)
	}
	if client.numberCalls != 2 {
		t.Fatalf("numbered detail calls = %d, want one per squash-merged target", client.numberCalls)
	}
}

// Every configured review target is required: evidence for one target never
// authorizes cleanup while another target lacks both ancestry and merged MR
// evidence.
func TestPlanTaskCleanup_MultiTargetReviewBlocksPerTargetEvidenceFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*forge.MRReadiness)
		history []forge.MRInfo
		want    string
		wantRef string
	}{
		{
			name:    "missing evidence for second target",
			history: []forge.MRInfo{mergedReviewHistoryRow(7)},
			want:    "no merged review evidence",
			wantRef: "refs/heads/develop",
		},
		{
			name:    "ambiguous history for squash target",
			history: []forge.MRInfo{mergedReviewHistoryRow(7), mergedReviewHistoryRow(8)},
			want:    "ambiguous",
		},
		{
			name:    "stale head for squash target",
			mutate:  func(d *forge.MRReadiness) { d.HeadSHA = strings.Repeat("f", 40) },
			history: []forge.MRInfo{mergedReviewHistoryRow(7)},
			want:    "does not match source",
		},
		{
			name:    "empty merged SHA for squash target",
			mutate:  func(d *forge.MRReadiness) { d.MergedSHA = "" },
			history: []forge.MRInfo{mergedReviewHistoryRow(7)},
			want:    "no merge SHA",
		},
		{
			name:    "closed MR for squash target",
			history: []forge.MRInfo{{Number: 7, State: "closed", SourceBranch: "feature/APP-1", TargetBranch: "master"}},
			want:    "no merged review evidence",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			detail := mergedReviewDetail(7)
			if tc.mutate != nil {
				tc.mutate(&detail)
			}
			mgr, gitMock, _ := setupReviewEvidenceCleanup(t, detail, tc.history...)
			useMultiTargetReviewFlow(mgr)
			gitMock.isAncestorFn = func(_, ancestor, _ string) (bool, error) {
				return ancestor == taskCleanupSquashSHA, nil
			}

			plan, err := mgr.PlanTaskCleanup(t.Context(), TaskCleanupRequest{TaskID: "APP-1"})
			if err != nil {
				t.Fatal(err)
			}
			if !plan.Blocked() || len(plan.steps) != 0 {
				t.Fatalf("blocked = %v, steps = %+v", plan.Blocked(), plan.steps)
			}
			blockers := strings.Join(plan.Preview().Blockers, "\n")
			wantRef := tc.wantRef
			if wantRef == "" {
				wantRef = "refs/heads/master"
			}
			if !strings.Contains(blockers, tc.want) || !strings.Contains(blockers, wantRef) {
				t.Fatalf("blockers = %q, want %q naming %s", blockers, tc.want, wantRef)
			}
		})
	}
}
