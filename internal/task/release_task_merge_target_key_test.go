package task

import (
	"errors"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestIntegrateReleaseTaskMRs_RepoTargetFrontier(t *testing.T) {
	for _, target := range []string{"develop", "stable"} {
		for _, reconcile := range []bool{false, true} {
			name := target
			if reconcile {
				name += "/reconcile"
			}
			t.Run(name, func(t *testing.T) {
				// Given
				x := newTaskMergeSafetyFixture(t, []string{"develop", target})
				if reconcile {
					x.forge.mergeErr = errors.New("timeout")
				}
				x.forge.afterMerge = func(number int) {
					if number == 2 {
						stored, err := x.m.GetRelease(t.Context(), x.release.ID)
						if err != nil {
							t.Fatal(err)
						}
						want := "base-stable"
						if target == "develop" {
							want = "merged-1"
						}
						if got := stored.Services[1].FeatureBranches[0].TaskMergeExpectedTarget; got != want {
							t.Errorf("attempt frontier = %s, want %s", got, want)
						}
					}
					x.acceptMerge(number)
				}

				// When
				err := x.m.integrateReleaseTaskMRs(t.Context(), &x.release, &x.plan, nil)

				// Then
				if err != nil || x.forge.mergeCalls != 2 {
					t.Fatalf("merges=%d err=%v, want two accepted merges", x.forge.mergeCalls, err)
				}
				for i, head := range x.forge.mergeExpectedHeads {
					if head != x.plan.steps[i].HeadSHA || head == "" {
						t.Errorf("merge %d was not pinned: %q", i+1, head)
					}
				}
			})
		}
	}
}

func TestIntegrateReleaseTaskMRs_PersistsAliasFrontier(t *testing.T) {
	// Given
	x := newTaskMergeSafetyFixture(t, []string{"develop", "develop", "stable"})
	x.forge.afterMerge = func(number int) {
		x.acceptMerge(number)
		mr := x.forge.readiness[2]
		mr.Ready = false
		x.forge.readiness[2] = mr
	}

	// When
	err := x.m.integrateReleaseTaskMRs(t.Context(), &x.release, &x.plan, nil)

	// Then
	if err == nil || x.forge.mergeCalls != 1 {
		t.Fatalf("merges=%d err=%v, want partial after MR1", x.forge.mergeCalls, err)
	}
	stored, err := x.m.GetRelease(t.Context(), x.release.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"base-develop", "merged-1", "base-stable"} {
		if got := stored.Services[i].FeatureBranches[0].TaskMergeExpectedTarget; got != want {
			t.Errorf("service %d expected target = %s, want %s", i+1, got, want)
		}
	}
}

func TestPlanReleaseTaskMergeRetry_RepoTargetFrontier(t *testing.T) {
	for _, target := range []string{"develop", "stable"} {
		t.Run(target, func(t *testing.T) {
			// Given: accepted remote merge with an unsaved checkpoint under another service name.
			x := newTaskMergeSafetyFixture(t, []string{"develop", target})
			if err := confirmReleaseTaskMergeRows(&x.release, &x.plan); err != nil {
				t.Fatal(err)
			}
			x.release.Status = domain.ReleaseStatusTaskMergePartial
			x.release.Services[0].FeatureBranches[0].TaskMergeStatus = taskMergeStatusUnknown
			x.acceptMerge(1)
			if _, err := x.m.writeReleaseManifest(x.release); err != nil {
				t.Fatal(err)
			}

			// When
			plan, err := x.m.PlanReleaseTaskMergeRetry(t.Context(), x.release.ID)

			// Then
			if err != nil {
				t.Fatal(err)
			}
			want := "base-stable"
			if target == "develop" {
				want = "merged-1"
			}
			if row := plan.Rows[1]; !row.Ready || row.TargetSHA != want {
				t.Fatalf("retry alias frontier = %+v, want ready at %s", row, want)
			}
		})
	}
}
