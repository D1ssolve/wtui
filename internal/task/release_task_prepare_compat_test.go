package task

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestRetryRelease_MetadataFreeKeepsLegacyPreparation(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			// Given
			g := &mockGitClient{}
			m, _ := newReleasePlanTestManager(t, g)
			if enabled {
				enableReleasePrepareTaskMerge(t, m)
			}
			*m.cfg.Release.CreateReleaseWorktrees = false
			*m.cfg.Release.PushIntegration = true
			release := writeTaskMergeRetryRelease(t, m, domain.ReleaseStatusFailed,
				domain.ReleaseFeatureBranch{TaskID: "APP-1", Branch: "feature/APP-1"})
			release.Error = &domain.ReleaseError{Recoverable: true}
			if _, err := m.writeReleaseManifest(release); err != nil {
				t.Fatal(err)
			}
			// When
			got, err := m.RetryRelease(t.Context(), release.ID)
			// Then
			if err != nil || got.Status != domain.ReleaseStatusPrepared || !got.Services[0].PushedIntegration || len(g.mergeFFOnlyCalls) != 1 {
				t.Fatalf("legacy recovery=%+v err=%v", got, err)
			}
		})
	}
}

func TestRetryReleaseTaskMerges_PrepareFailureReportsPersistenceError(t *testing.T) {
	// Given
	x := newTaskPrepareRecovery(t)
	after := x.f.afterMerge
	x.f.afterMerge = func(n int) {
		after(n)
		if n == 1 {
			mr := x.f.readiness[2]
			mr.Ready = false
			x.f.readiness[2] = mr
		}
	}
	release, err := x.m.CreateRelease(t.Context(), x.params)
	if err == nil || release.Status != domain.ReleaseStatusTaskMergePartial {
		t.Fatalf("release=%+v err=%v", release, err)
	}
	mr := x.f.readiness[2]
	mr.Ready = true
	x.f.readiness[2] = mr
	plan, err := x.m.PlanReleaseTaskMergeRetry(t.Context(), release.ID)
	if err != nil {
		t.Fatal(err)
	}
	pushErr := errors.New("push failed")
	x.g.pushBranchExplicitFn = func(_, _ string) error {
		manifest := filepath.Join(release.Dir, releaseManifestFileName)
		if err := os.Rename(manifest, manifest+".saved"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(manifest, 0o755); err != nil {
			t.Fatal(err)
		}
		return pushErr
	}
	// When
	_, err = x.m.RetryReleaseTaskMerges(t.Context(), release.ID, &plan)
	// Then
	if !errors.Is(err, pushErr) || !errors.Is(err, ErrReleaseManifestInvalid) {
		t.Fatalf("error=%v, want operation and persistence failures", err)
	}
}
