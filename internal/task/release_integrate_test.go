package task

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/D1ssolve/wtui/internal/forge"
)

func TestCreateRelease_MergeErrorReconcilesProvenMergedMR(t *testing.T) {
	// Given
	targetTip := "d0"
	gitMock := &mockGitClient{remoteURLRes: "git@github.com:org/repo.git", resolveRefFn: func(_, ref string) (string, error) {
		if ref == "origin/develop" || ref == "HEAD" || ref == "release/1.2.3" || ref == "origin/release/1.2.3" {
			return targetTip, nil
		}
		return ref + "-sha", nil
	}}
	m, _ := newReleasePlanTestManager(t, gitMock)
	enableReleasePrepareTaskMerge(t, m)
	f := newReleaseTaskMergeForge()
	f.mergeErr = errors.New("timeout")
	f.readiness[1] = forge.MRReadiness{Number: 1, State: "open", SourceBranch: "feature/APP-1", TargetBranch: "develop", HeadSHA: "head-1", Ready: true, SupportsSHAPin: true}
	f.afterMerge = func(number int) {
		targetTip = "d1"
		f.readiness[number] = forge.MRReadiness{Number: number, State: "merged", SourceBranch: "feature/APP-1", TargetBranch: "develop", HeadSHA: "head-1", MergedSHA: "d1", Ready: true, SupportsSHAPin: true}
	}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitHub: f}
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
		releasePlanTaskService{TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-api")},
	)
	setTaskWorktreeHeads(gitMock, map[string]string{filepath.Join(m.cfg.TasksRoot, "APP-1", "api"): "head-1"})
	plan, err := m.PlanReleaseTaskMerges(context.Background(), CreateReleaseParams{TaskIDs: []string{"APP-1"}, ServiceVersions: map[string]string{"api": "1.2.3"}})
	if err != nil {
		t.Fatalf("PlanReleaseTaskMerges() error = %v", err)
	}

	// When
	release, err := m.CreateRelease(context.Background(), CreateReleaseParams{TaskIDs: []string{"APP-1"}, ServiceVersions: map[string]string{"api": "1.2.3"}, StartImmediately: true, ConfirmedTaskMergePlan: &plan})

	// Then
	if err != nil {
		t.Fatalf("CreateRelease() error = %v", err)
	}
	if f.mergeCalls != 1 {
		t.Fatalf("merge calls = %d, want one", f.mergeCalls)
	}
	if release.Services[0].FeatureBranches[0].TaskMergeStatus != taskMergeStatusMerged || release.Services[0].FeatureBranches[0].MergeRef != "d1" {
		t.Fatalf("feature branch = %#v", release.Services[0].FeatureBranches[0])
	}
}
