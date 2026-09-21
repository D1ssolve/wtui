package task

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
)

func TestPlanReleaseTaskMergeRetry_ProvenFrontier(t *testing.T) {
	for _, tip := range []string{"d1", "unrelated"} {
		t.Run(tip, func(t *testing.T) {
			// Given: MR1 merged remotely before its accepted checkpoint was saved.
			g := &mockGitClient{remoteURLRes: "git@github.com:org/repo.git", resolveRefFn: func(_, _ string) (string, error) { return tip, nil }}
			m, _ := newReleasePlanTestManager(t, g)
			setTaskWorktreeHeads(g, map[string]string{
				filepath.Join(m.cfg.TasksRoot, "APP-1", "api"): "head-1",
				filepath.Join(m.cfg.TasksRoot, "APP-2", "api"): "head-2",
				filepath.Join(m.cfg.TasksRoot, "APP-3", "api"): "head-3",
			})
			enableReleasePrepareTaskMerge(t, m)
			f := newReleaseTaskMergeForge()
			var branches []domain.ReleaseFeatureBranch
			for n := 1; n <= 3; n++ {
				id := fmt.Sprintf("APP-%d", n)
				f.readiness[n] = forge.MRReadiness{Number: n, State: "open", SourceBranch: "feature/" + id, TargetBranch: "develop", HeadSHA: fmt.Sprintf("head-%d", n), Ready: true, SupportsSHAPin: true}
				branches = append(branches, domain.ReleaseFeatureBranch{TaskID: id, ServiceName: "api", Branch: "feature/" + id, WorktreePath: filepath.Join(m.cfg.TasksRoot, id, "api"), TaskMergeStatus: taskMergeStatusPending, TaskMergeMRNumber: n, TaskMergeHeadSHA: fmt.Sprintf("head-%d", n), TaskMergeTargetSHA: "d0", TaskMergeExpectedTarget: "d0"})
			}
			branches[0].TaskMergeStatus = taskMergeStatusUnknown
			mr := f.readiness[1]
			mr.State, mr.MergedSHA = "merged", "d1"
			f.readiness[1] = mr
			m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitHub: f}
			release := writeTaskMergeRetryRelease(t, m, domain.ReleaseStatusTaskMergePartial, branches...)

			// When
			plan, err := m.PlanReleaseTaskMergeRetry(t.Context(), release.ID)

			// Then
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range plan.Rows[1:] {
				if row.Ready != (tip == "d1") || row.TargetSHA != "d1" {
					t.Errorf("retry frontier tip=%s row=%+v", tip, row)
				}
			}
			stored, err := m.GetRelease(t.Context(), release.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Services[0].FeatureBranches[0].TaskMergeExpectedTarget != "d0" {
				t.Fatal("planning overwrote uncertain attempt evidence")
			}
			if tip == "d1" {
				tip = "unrelated-after-preview"
				if _, err := m.RetryReleaseTaskMerges(t.Context(), release.ID, &plan); err == nil {
					t.Fatal("retry accepted target movement after preview")
				}
			}
			if f.mergeCalls != 0 {
				t.Fatalf("unexpected merges: %d", f.mergeCalls)
			}
		})
	}
}

func TestTaskMergeConfirmation_PreservesAttemptEvidence(t *testing.T) {
	// Given
	branches := []domain.ReleaseFeatureBranch{
		{TaskID: "APP-1", TaskMergeStatus: taskMergeStatusMerged, Merged: true, MergeRef: "d1"},
		{TaskID: "APP-2", TaskMergeStatus: taskMergeStatusUnknown},
		{TaskID: "APP-3", TaskMergeStatus: taskMergeStatusAttempting},
		{TaskID: "APP-4", TaskMergeStatus: taskMergeStatusPending},
		{TaskID: "APP-5", TaskMergeStatus: taskMergeStatusPending},
	}
	plan := &ReleaseTaskMergePlan{}
	for i := range branches {
		fb := &branches[i]
		fb.TaskMergeMRNumber = i + 1
		fb.TaskMergeMRURL = fmt.Sprintf("https://example.com/mr/%d", i+1)
		fb.TaskMergeHeadSHA = fmt.Sprintf("head-%d", i+1)
		fb.TaskMergeTargetSHA, fb.TaskMergeExpectedTarget = "d0", "attempt-target"
		plan.steps = append(plan.steps, releaseTaskMergeStep{ServiceName: "api", TaskID: fb.TaskID, TargetSHA: "confirmed-frontier"})
	}
	evidence := append([]domain.ReleaseFeatureBranch(nil), branches[:3]...)
	release := domain.Release{Services: []domain.ReleaseService{{Name: "api", FeatureBranches: branches}}}

	// When
	if err := confirmReleaseTaskMergeRows(&release, plan); err != nil {
		t.Fatal(err)
	}
	advancePendingTaskMergeTargets(&release, releaseTaskMergeTargetKey("", ""), "accepted-frontier")

	// Then
	if !reflect.DeepEqual(branches[:3], evidence) {
		t.Fatalf("accepted/uncertain evidence overwritten: %+v", branches[:3])
	}
	for _, fb := range branches[3:] {
		if fb.TaskMergeExpectedTarget != "accepted-frontier" || fb.TaskMergeTargetSHA != "d0" {
			t.Errorf("pending frontier: %+v", fb)
		}
	}
}
