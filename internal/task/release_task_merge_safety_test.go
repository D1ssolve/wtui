package task

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
)

type taskMergeSafetyFixture struct {
	m       *manager
	forge   *releaseTaskMergeForge
	release domain.Release
	plan    ReleaseTaskMergePlan
	tips    map[string]string
}

func newTaskMergeSafetyFixture(t *testing.T, targets []string) *taskMergeSafetyFixture {
	t.Helper()
	x := &taskMergeSafetyFixture{forge: newReleaseTaskMergeForge(), tips: map[string]string{}}
	g := &mockGitClient{remoteURLRes: "git@github.com:org/repo.git", resolveRefFn: func(_, ref string) (string, error) {
		return x.tips[ref], nil
	}}
	x.m, _ = newReleasePlanTestManager(t, g)
	enableReleasePrepareTaskMerge(t, x.m)
	x.m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitHub: x.forge}
	x.release = domain.Release{ID: "rel-safety", Status: domain.ReleaseStatusValidating}
	for i, target := range targets {
		n := i + 1
		id, name := fmt.Sprintf("APP-%d", n), fmt.Sprintf("api-%d", n)
		x.tips["origin/"+target] = "base-" + target
		x.release.TaskIDs = append(x.release.TaskIDs, id)
		x.release.Services = append(x.release.Services, domain.ReleaseService{
			Name: name, RepoPath: filepath.Join(x.m.cfg.RootDir, "repo"), IntegrationBranch: target,
			ReleaseBranch: "release/1.2.3", Version: "1.2.3", Tag: "v1.2.3",
			FeatureBranches: []domain.ReleaseFeatureBranch{{TaskID: id, ServiceName: name, Branch: "feature/" + id, WorktreePath: filepath.Join(x.m.cfg.TasksRoot, id, name)}},
		})
		x.forge.readiness[n] = forge.MRReadiness{Number: n, State: "open", SourceBranch: "feature/" + id, TargetBranch: target, HeadSHA: fmt.Sprintf("head-%d", n), Ready: true, SupportsSHAPin: true}
	}
	var err error
	heads := map[string]string{}
	for i, svc := range x.release.Services {
		heads[svc.FeatureBranches[0].WorktreePath] = fmt.Sprintf("head-%d", i+1)
	}
	setTaskWorktreeHeads(g, heads)
	x.plan, err = x.m.planReleaseTaskMerges(t.Context(), releasePlanFromRelease(x.release))
	if err != nil {
		t.Fatal(err)
	}
	x.forge.afterMerge = x.acceptMerge
	return x
}

func (x *taskMergeSafetyFixture) acceptMerge(number int) {
	mr := x.forge.readiness[number]
	mr.State, mr.MergedSHA = "merged", fmt.Sprintf("merged-%d", number)
	x.tips["origin/"+mr.TargetBranch] = mr.MergedSHA
	x.forge.readiness[number] = mr
}

func TestIntegrateReleaseTaskMRs_RequiresSHAPinning(t *testing.T) {
	// Given
	x := newTaskMergeSafetyFixture(t, []string{"develop"})
	mr := x.forge.readiness[1]
	mr.SupportsSHAPin = false
	x.forge.readiness[1] = mr

	// When
	err := x.m.integrateReleaseTaskMRs(t.Context(), &x.release, &x.plan, nil)

	// Then
	if err == nil || !strings.Contains(err.Error(), "head SHA pinning") {
		t.Errorf("error = %v, want explicit head SHA pinning rejection", err)
	}
	if x.forge.mergeCalls != 0 {
		t.Errorf("merge calls = %d, want zero", x.forge.mergeCalls)
	}
	stored, err := x.m.GetRelease(t.Context(), x.release.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != domain.ReleaseStatusTaskMergeBlocked || stored.Services[0].FeatureBranches[0].TaskMergeStatus != taskMergeStatusPending {
		t.Fatalf("unpinned release was not blocked before attempting: %+v", stored)
	}
}

func TestIntegrateReleaseTaskMRs_PostMergeIdentityDrift(t *testing.T) {
	for _, field := range []string{"source", "target", "head"} {
		t.Run(field, func(t *testing.T) {
			// Given
			x := newTaskMergeSafetyFixture(t, []string{"develop", "develop"})
			x.forge.afterMerge = func(number int) {
				x.acceptMerge(number)
				mr := x.forge.readiness[number]
				switch field {
				case "source":
					mr.SourceBranch = "feature/other"
				case "target":
					mr.TargetBranch = "other"
				case "head":
					mr.HeadSHA = "other-head"
				}
				x.forge.readiness[number] = mr
			}

			// When
			err := x.m.integrateReleaseTaskMRs(t.Context(), &x.release, &x.plan, nil)

			// Then
			if err == nil || !strings.Contains(err.Error(), "merge result is unknown") {
				t.Errorf("error = %v, want unknown merge result", err)
			}
			if x.forge.mergeCalls != 1 {
				t.Errorf("merge calls = %d, want one", x.forge.mergeCalls)
			}
			stored, err := x.m.GetRelease(t.Context(), x.release.ID)
			if err != nil {
				t.Fatal(err)
			}
			fb := stored.Services[0].FeatureBranches[0]
			if fb.TaskMergeStatus != taskMergeStatusUnknown || fb.Merged || fb.MergeRef != "" || fb.TaskMergeHeadSHA != "head-1" {
				t.Errorf("drift was accepted or lost confirmed evidence: %+v", fb)
			}
			if stored.Services[1].FeatureBranches[0].TaskMergeExpectedTarget != "base-develop" {
				t.Fatal("unproven merge advanced remaining frontier")
			}
		})
	}
}

func TestIntegrateReleaseTaskMRs_RejectsEmptyConfirmedHead(t *testing.T) {
	// Given
	x := newTaskMergeSafetyFixture(t, []string{"develop"})
	x.plan.steps[0].HeadSHA = ""
	mr := x.forge.readiness[1]
	mr.HeadSHA = ""
	x.forge.readiness[1] = mr

	// When
	err := x.m.integrateReleaseTaskMRs(t.Context(), &x.release, &x.plan, nil)

	// Then
	if err == nil || x.forge.mergeCalls != 0 {
		t.Fatalf("merges=%d err=%v, want empty head rejected before merge", x.forge.mergeCalls, err)
	}
}
