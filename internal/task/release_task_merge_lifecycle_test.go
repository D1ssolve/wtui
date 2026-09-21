package task

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
)

func TestCreateRelease_TaskMergeRecoveryAfterReload(t *testing.T) {
	// Given: three confirmed MRs, with MR2 losing readiness after MR1 merges.
	targetTip := "d0"
	g := &mockGitClient{remoteURLRes: "git@github.com:org/repo.git", resolveRefFn: func(_, ref string) (string, error) {
		switch ref {
		case "origin/develop", "HEAD", "release/1.2.3", "origin/release/1.2.3":
			return targetTip, nil
		default:
			return ref + "-sha", nil
		}
	}}
	m, _ := newReleasePlanTestManager(t, g)
	enableReleasePrepareTaskMerge(t, m)
	f := newReleaseTaskMergeForge()
	var specs []releasePlanTaskService
	for n := 1; n <= 3; n++ {
		id := fmt.Sprintf("APP-%d", n)
		f.readiness[n] = forge.MRReadiness{Number: n, URL: fmt.Sprintf("https://github.com/org/repo/pull/%d", n), State: "open", SourceBranch: "feature/" + id, TargetBranch: "develop", HeadSHA: fmt.Sprintf("head-%d", n), Ready: true, SupportsSHAPin: true}
		specs = append(specs, releasePlanTaskService{TaskID: id, ServiceName: "api", Branch: "feature/" + id, RepoPath: filepath.Join(m.cfg.RootDir, "repo-api")})
	}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitHub: f}
	seedReleasePlanTasks(t, m.cfg.TasksRoot, g, specs...)
	setTaskWorktreeHeads(g, map[string]string{
		filepath.Join(m.cfg.TasksRoot, "APP-1", "api"): "head-1",
		filepath.Join(m.cfg.TasksRoot, "APP-2", "api"): "head-2",
		filepath.Join(m.cfg.TasksRoot, "APP-3", "api"): "head-3",
	})
	params := CreateReleaseParams{TaskIDs: []string{"APP-1", "APP-2", "APP-3"}, ServiceVersions: map[string]string{"api": "1.2.3"}, StartImmediately: true}
	plan, err := m.PlanReleaseTaskMerges(t.Context(), params)
	if err != nil {
		t.Fatal(err)
	}
	// Display rows are not authoritative confirmation data.
	plan.Rows[2].MRNumber = 999
	params.ConfirmedTaskMergePlan = &plan
	f.afterMerge = func(number int) {
		stored, err := m.ListReleases(t.Context())
		if err != nil || len(stored) != 1 {
			t.Fatalf("load at merge boundary: %v, %v", stored, err)
		}
		for i, fb := range stored[0].Services[0].FeatureBranches {
			n := i + 1
			if fb.TaskMergeMRNumber != n || fb.TaskMergeMRURL != f.readiness[n].URL || fb.TaskMergeHeadSHA != fmt.Sprintf("head-%d", n) || fb.TaskMergeTargetSHA != "d0" {
				t.Errorf("MR%d confirmation missing/changed before merge %d: %+v", n, number, fb)
			}
			if n > number && (fb.TaskMergeStatus != taskMergeStatusPending || fb.TaskMergeExpectedTarget != targetTip) {
				t.Errorf("MR%d pending frontier before merge %d: %+v", n, number, fb)
			}
			if n < number && (!fb.Merged || fb.TaskMergeStatus != taskMergeStatusMerged || fb.MergeRef != fmt.Sprintf("d%d", n)) {
				t.Errorf("accepted MR%d lost at merge %d: %+v", n, number, fb)
			}
		}
		targetTip = fmt.Sprintf("d%d", number)
		mr := f.readiness[number]
		mr.State, mr.MergedSHA = "merged", targetTip
		f.readiness[number] = mr
		if number == 1 {
			mr = f.readiness[2]
			mr.Ready = false
			f.readiness[2] = mr
		}
	}

	// When: create stops partially, then a new manager reloads and retries.
	partial, err := m.CreateRelease(t.Context(), params)
	if err == nil || partial.Status != domain.ReleaseStatusTaskMergePartial {
		t.Fatalf("CreateRelease() status=%s err=%v, want partial", partial.Status, err)
	}
	reloaded := New(m.cfg, g, m.discoverer, m.slnMgr, m.validator, m.flow, m.forgeClients, m.logger)
	stored, err := reloaded.GetRelease(t.Context(), partial.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, fb := range stored.Services[0].FeatureBranches[1:] {
		if fb.TaskMergeExpectedTarget != "d1" || fb.TaskMergeTargetSHA != "d0" || fb.TaskMergeStatus != taskMergeStatusPending {
			t.Errorf("remaining row did not persist rolling frontier: %+v", fb)
		}
	}
	mr := f.readiness[2]
	mr.Ready = true
	f.readiness[2] = mr
	retry, err := reloaded.PlanReleaseTaskMergeRetry(t.Context(), partial.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reloaded.RetryReleaseTaskMerges(t.Context(), partial.ID, &retry)

	// Then: each MR merged exactly once; the original confirmation survives retry.
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.ReleaseStatusPrepared || !reflect.DeepEqual(f.mergeNumbers, []int{1, 2, 3}) {
		t.Fatalf("status=%s merges=%v", got.Status, f.mergeNumbers)
	}
	for i, fb := range got.Services[0].FeatureBranches {
		if fb.TaskMergeTargetSHA != "d0" || fb.TaskMergeExpectedTarget != fmt.Sprintf("d%d", i) || fb.MergeRef != fmt.Sprintf("d%d", i+1) {
			t.Errorf("final recovery evidence: %+v", fb)
		}
	}
}
