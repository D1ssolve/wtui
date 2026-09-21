package task

import (
	"errors"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestRetryRelease_TaskPrepareRejectsChangedEvidence(t *testing.T) {
	for _, change := range []string{"disabled", "source", "target", "head", "merge SHA", "open", "unknown", "tip", "missing metadata", "local", "remote", "prepared local", "prepared remote"} {
		t.Run(change, func(t *testing.T) {
			// Given: persisted evidence from a real failed preparation.
			x := newTaskPrepareRecovery(t)
			x.failure = "push"
			failed, err := x.m.CreateRelease(t.Context(), x.params)
			if err == nil || failed.Status != domain.ReleaseStatusFailed {
				t.Fatalf("release=%+v err=%v", failed, err)
			}
			x.failure = ""
			mr := x.f.readiness[1]
			switch change {
			case "disabled":
				x.m.cfg.GitFlow.TaskMerge = nil
			case "source":
				mr.SourceBranch = "feature/other"
			case "target":
				mr.TargetBranch = "other"
			case "head":
				mr.HeadSHA = "other"
			case "merge SHA":
				mr.MergedSHA = "other"
			case "open":
				mr.State = "open"
			case "unknown":
				mr.State = ""
			case "tip":
				x.tip = "other"
			case "missing metadata":
				failed.Services[0].FeatureBranches[0].TaskMergeHeadSHA = ""
			case "local":
				x.local = "other"
			case "remote":
				x.remote = "other"
			case "prepared local":
				failed.Services[0].Status = domain.ReleaseStatusPrepared
				x.local = "other"
			case "prepared remote":
				failed.Services[0].PushedReleaseBranch = true
				x.remote = "other"
			}
			x.f.readiness[1] = mr
			if _, err := x.m.writeReleaseManifest(failed); err != nil {
				t.Fatal(err)
			}
			x.reload()
			branches, pushes := len(x.g.createBranchFromBranchCalls), len(x.g.pushBranchExplicitCalls)
			// When
			_, err = x.m.RetryRelease(t.Context(), failed.ID)
			// Then
			if !errors.Is(err, ErrReleaseRetryUnsafe) {
				t.Fatalf("error=%v, want unsafe retry", err)
			}
			if len(x.g.createBranchFromBranchCalls) != branches || len(x.g.pushBranchExplicitCalls) != pushes || x.f.mergeCalls != 2 || len(x.g.mergeFFOnlyCalls) != 0 {
				t.Fatal("unsafe retry mutated repositories")
			}
		})
	}
}

func TestRetryRelease_TaskPrepareFailureCanRetryAgain(t *testing.T) {
	// Given
	x := newTaskPrepareRecovery(t)
	x.failure = "branch"
	failed, err := x.m.CreateRelease(t.Context(), x.params)
	if err == nil {
		t.Fatal("expected creation failure")
	}
	x.reload()
	// When
	failed, err = x.m.RetryRelease(t.Context(), failed.ID)
	// Then
	if err == nil || failed.Status != domain.ReleaseStatusFailed || failed.Error == nil || !failed.Error.Recoverable {
		t.Fatalf("release=%+v err=%v", failed, err)
	}
	x.failure = ""
	x.reload()
	got, err := x.m.RetryRelease(t.Context(), failed.ID)
	if err != nil || got.Status != domain.ReleaseStatusPrepared || x.f.mergeCalls != 2 {
		t.Fatalf("release=%+v err=%v", got, err)
	}
}
