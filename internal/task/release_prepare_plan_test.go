package task

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
)

func TestPlanReleaseTaskMerges_BlockedMRPreventsCreateMutation(t *testing.T) {
	// Given
	gitMock := &mockGitClient{remoteURLRes: "git@github.com:org/repo.git"}
	m, _ := newReleasePlanTestManager(t, gitMock)
	setTaskWorktreeHeads(gitMock, map[string]string{filepath.Join(m.cfg.TasksRoot, "APP-1", "api"): "head-a"})
	enableReleasePrepareTaskMerge(t, m)
	f := newReleaseTaskMergeForge()
	f.readiness[1] = forge.MRReadiness{Number: 1, State: "open", SourceBranch: "feature/APP-1", TargetBranch: "develop", HeadSHA: "head-a", Ready: false, Blockers: []string{"not approved"}, SupportsSHAPin: true}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitHub: f}
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
		releasePlanTaskService{TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-api")},
	)

	// When
	plan, err := m.PlanReleaseTaskMerges(context.Background(), CreateReleaseParams{TaskIDs: []string{"APP-1"}, ServiceVersions: map[string]string{"api": "1.2.3"}})
	if err != nil {
		t.Fatalf("PlanReleaseTaskMerges() error = %v", err)
	}
	_, err = m.CreateRelease(context.Background(), CreateReleaseParams{TaskIDs: []string{"APP-1"}, ServiceVersions: map[string]string{"api": "1.2.3"}, StartImmediately: true, ConfirmedTaskMergePlan: &plan})

	// Then
	if err == nil {
		t.Fatalf("CreateRelease() error=nil, want blocked plan error")
	}
	if f.mergeCalls != 0 || len(gitMock.createBranchFromBranchCalls) != 0 {
		t.Fatalf("mutations: merges=%d branches=%#v, want none", f.mergeCalls, gitMock.createBranchFromBranchCalls)
	}
}

func TestCreateRelease_ConfirmedPlanRowsAreDisplayOnly(t *testing.T) {
	// Given
	gitMock := &mockGitClient{remoteURLRes: "git@github.com:org/repo.git"}
	m, _ := newReleasePlanTestManager(t, gitMock)
	setTaskWorktreeHeads(gitMock, map[string]string{filepath.Join(m.cfg.TasksRoot, "APP-1", "api"): "head-a"})
	enableReleasePrepareTaskMerge(t, m)
	f := newReleaseTaskMergeForge()
	f.readiness[1] = forge.MRReadiness{Number: 1, State: "open", SourceBranch: "feature/APP-1", TargetBranch: "develop", HeadSHA: "head-a", Ready: false, Blockers: []string{"not approved"}, SupportsSHAPin: true}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitHub: f}
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
		releasePlanTaskService{TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-api")},
	)
	plan, err := m.PlanReleaseTaskMerges(context.Background(), CreateReleaseParams{TaskIDs: []string{"APP-1"}, ServiceVersions: map[string]string{"api": "1.2.3"}})
	if err != nil {
		t.Fatalf("PlanReleaseTaskMerges() error = %v", err)
	}
	plan.Rows[0].Ready = true
	plan.Rows[0].Blockers = nil

	// When
	_, err = m.CreateRelease(context.Background(), CreateReleaseParams{TaskIDs: []string{"APP-1"}, ServiceVersions: map[string]string{"api": "1.2.3"}, StartImmediately: true, ConfirmedTaskMergePlan: &plan})

	// Then
	if err == nil {
		t.Fatalf("CreateRelease() error=nil, want private step to remain blocked")
	}
	if f.mergeCalls != 0 || len(gitMock.createBranchFromBranchCalls) != 0 {
		t.Fatalf("mutations: merges=%d branches=%#v, want none", f.mergeCalls, gitMock.createBranchFromBranchCalls)
	}
}

func TestCreateRelease_ConfirmedPlanForDifferentParamsFailsBeforeManifest(t *testing.T) {
	// Given
	gitMock := &mockGitClient{remoteURLRes: "git@github.com:org/repo.git"}
	m, _ := newReleasePlanTestManager(t, gitMock)
	setTaskWorktreeHeads(gitMock, map[string]string{filepath.Join(m.cfg.TasksRoot, "APP-1", "api"): "head-a"})
	enableReleasePrepareTaskMerge(t, m)
	f := newReleaseTaskMergeForge()
	f.readiness[1] = forge.MRReadiness{Number: 1, State: "open", SourceBranch: "feature/APP-1", TargetBranch: "develop", HeadSHA: "head-a", Ready: true, SupportsSHAPin: true}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitHub: f}
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
		releasePlanTaskService{TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-api")},
	)
	plan, err := m.PlanReleaseTaskMerges(context.Background(), CreateReleaseParams{TaskIDs: []string{"APP-1"}, ServiceVersions: map[string]string{"api": "1.2.3"}})
	if err != nil {
		t.Fatalf("PlanReleaseTaskMerges() error = %v", err)
	}

	// When
	_, err = m.CreateRelease(context.Background(), CreateReleaseParams{TaskIDs: []string{"APP-1"}, ServiceVersions: map[string]string{"api": "2.0.0"}, StartImmediately: true, ConfirmedTaskMergePlan: &plan})

	// Then
	if err == nil {
		t.Fatalf("CreateRelease() error=nil, want confirmed plan input mismatch")
	}
	if f.mergeCalls != 0 || len(gitMock.createBranchFromBranchCalls) != 0 {
		t.Fatalf("mutations: merges=%d branches=%#v, want none", f.mergeCalls, gitMock.createBranchFromBranchCalls)
	}
	releases, loadErr := m.listReleaseManifests()
	if loadErr != nil {
		t.Fatalf("listReleaseManifests() error = %v", loadErr)
	}
	if len(releases) != 0 {
		t.Fatalf("release manifests = %#v, want none", releases)
	}
}

func TestCreateRelease_ConfirmedPlanConfigDriftFailsBeforeManifest(t *testing.T) {
	// Given
	gitMock := &mockGitClient{remoteURLRes: "git@github.com:org/repo.git"}
	m, _ := newReleasePlanTestManager(t, gitMock)
	setTaskWorktreeHeads(gitMock, map[string]string{filepath.Join(m.cfg.TasksRoot, "APP-1", "api"): "head-a"})
	enableReleasePrepareTaskMerge(t, m)
	f := newReleaseTaskMergeForge()
	f.readiness[1] = forge.MRReadiness{Number: 1, State: "open", SourceBranch: "feature/APP-1", TargetBranch: "develop", HeadSHA: "head-a", Ready: true, SupportsSHAPin: true}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitHub: f}
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
		releasePlanTaskService{TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-api")},
	)
	plan, err := m.PlanReleaseTaskMerges(context.Background(), CreateReleaseParams{TaskIDs: []string{"APP-1"}, ServiceVersions: map[string]string{"api": "1.2.3"}})
	if err != nil {
		t.Fatalf("PlanReleaseTaskMerges() error = %v", err)
	}
	*m.cfg.Release.PushReleaseBranches = !*m.cfg.Release.PushReleaseBranches

	// When
	_, err = m.CreateRelease(context.Background(), CreateReleaseParams{TaskIDs: []string{"APP-1"}, ServiceVersions: map[string]string{"api": "1.2.3"}, StartImmediately: true, ConfirmedTaskMergePlan: &plan})

	// Then
	if err == nil {
		t.Fatalf("CreateRelease() error=nil, want config mismatch")
	}
	if f.mergeCalls != 0 {
		t.Fatalf("merge calls = %d, want none", f.mergeCalls)
	}
}

func TestCreateRelease_ReleasePrepareMergesTwoMRsSequentially(t *testing.T) {
	// Given
	targetTips := []string{"d0", "d1", "d2"}
	gitMock := &mockGitClient{remoteURLRes: "git@github.com:org/repo.git"}
	gitMock.resolveRefFn = func(_, ref string) (string, error) {
		if ref == "origin/develop" || ref == "release/1.2.3" || ref == "origin/release/1.2.3" {
			return targetTips[0], nil
		}
		if ref == "HEAD" {
			return targetTips[0], nil
		}
		return ref + "-sha", nil
	}
	m, _ := newReleasePlanTestManager(t, gitMock)
	setTaskWorktreeHeads(gitMock, map[string]string{
		filepath.Join(m.cfg.TasksRoot, "APP-1", "api"): "head-1",
		filepath.Join(m.cfg.TasksRoot, "APP-2", "api"): "head-2",
	})
	enableReleasePrepareTaskMerge(t, m)
	f := newReleaseTaskMergeForge()
	f.readiness[1] = forge.MRReadiness{Number: 1, State: "open", SourceBranch: "feature/APP-1", TargetBranch: "develop", HeadSHA: "head-1", Ready: true, SupportsSHAPin: true}
	f.readiness[2] = forge.MRReadiness{Number: 2, State: "open", SourceBranch: "feature/APP-2", TargetBranch: "develop", HeadSHA: "head-2", Ready: true, SupportsSHAPin: true}
	f.afterMerge = func(number int) {
		f.readiness[number] = forge.MRReadiness{Number: number, State: "merged", SourceBranch: f.readiness[number].SourceBranch, TargetBranch: "develop", HeadSHA: f.readiness[number].HeadSHA, MergedSHA: targetTips[1], Ready: true, SupportsSHAPin: true}
		targetTips = targetTips[1:]
	}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitHub: f}
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
		releasePlanTaskService{TaskID: "APP-2", ServiceName: "api", Branch: "feature/APP-2", RepoPath: filepath.Join(m.cfg.RootDir, "repo-api")},
		releasePlanTaskService{TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-api")},
	)

	// When
	plan, err := m.PlanReleaseTaskMerges(context.Background(), CreateReleaseParams{TaskIDs: []string{"APP-2", "APP-1"}, ServiceVersions: map[string]string{"api": "1.2.3"}})
	if err != nil {
		t.Fatalf("PlanReleaseTaskMerges() error = %v", err)
	}
	release, err := m.CreateRelease(context.Background(), CreateReleaseParams{TaskIDs: []string{"APP-2", "APP-1"}, ServiceVersions: map[string]string{"api": "1.2.3"}, StartImmediately: true, ConfirmedTaskMergePlan: &plan})

	// Then
	if err != nil {
		t.Fatalf("CreateRelease() error = %v", err)
	}
	if got := f.mergeNumbers; len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("merge order = %#v, want [1 2]", got)
	}
	if len(f.mergeExpectedHeads) != 2 || f.mergeExpectedHeads[0] != "head-1" || f.mergeExpectedHeads[1] != "head-2" {
		t.Fatalf("merge expected heads = %#v", f.mergeExpectedHeads)
	}
	if release.Services[0].PostIntegrationSHA != "d2" {
		t.Fatalf("PostIntegrationSHA = %q, want d2", release.Services[0].PostIntegrationSHA)
	}
	if len(gitMock.createBranchFromBranchCalls) != 1 || gitMock.createBranchFromBranchCalls[0].FromBranch != "d2" {
		t.Fatalf("release branch calls = %#v, want branch from d2", gitMock.createBranchFromBranchCalls)
	}
	if len(gitMock.pushBranchExplicitCalls) != 1 || gitMock.pushBranchExplicitCalls[0].Branch == "HEAD:develop" {
		t.Fatalf("push calls = %#v, want release branch push only", gitMock.pushBranchExplicitCalls)
	}
}

func TestCreateRelease_ReleasePrepareStopsWhenSecondMRLosesReadiness(t *testing.T) {
	// Given
	targetTip := "d0"
	gitMock := &mockGitClient{remoteURLRes: "git@github.com:org/repo.git", resolveRefFn: func(_, ref string) (string, error) {
		if ref == "origin/develop" || ref == "HEAD" || ref == "release/1.2.3" || ref == "origin/release/1.2.3" {
			return targetTip, nil
		}
		return ref + "-sha", nil
	}}
	m, _ := newReleasePlanTestManager(t, gitMock)
	setTaskWorktreeHeads(gitMock, map[string]string{
		filepath.Join(m.cfg.TasksRoot, "APP-1", "api"): "head-1",
		filepath.Join(m.cfg.TasksRoot, "APP-2", "api"): "head-2",
	})
	enableReleasePrepareTaskMerge(t, m)
	f := newReleaseTaskMergeForge()
	f.readiness[1] = forge.MRReadiness{Number: 1, State: "open", SourceBranch: "feature/APP-1", TargetBranch: "develop", HeadSHA: "head-1", Ready: true, SupportsSHAPin: true}
	f.readiness[2] = forge.MRReadiness{Number: 2, State: "open", SourceBranch: "feature/APP-2", TargetBranch: "develop", HeadSHA: "head-2", Ready: true, SupportsSHAPin: true}
	f.afterMerge = func(number int) {
		if number == 1 {
			targetTip = "d1"
			f.readiness[1] = forge.MRReadiness{Number: 1, State: "merged", SourceBranch: "feature/APP-1", TargetBranch: "develop", HeadSHA: "head-1", MergedSHA: "d1", Ready: true, SupportsSHAPin: true}
			f.readiness[2] = forge.MRReadiness{Number: 2, State: "open", SourceBranch: "feature/APP-2", TargetBranch: "develop", HeadSHA: "head-2", Ready: false, Blockers: []string{"checks pending"}, SupportsSHAPin: true}
		}
	}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitHub: f}
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
		releasePlanTaskService{TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-api")},
		releasePlanTaskService{TaskID: "APP-2", ServiceName: "api", Branch: "feature/APP-2", RepoPath: filepath.Join(m.cfg.RootDir, "repo-api")},
	)

	// When
	plan, err := m.PlanReleaseTaskMerges(context.Background(), CreateReleaseParams{TaskIDs: []string{"APP-1", "APP-2"}, ServiceVersions: map[string]string{"api": "1.2.3"}})
	if err != nil {
		t.Fatalf("PlanReleaseTaskMerges() error = %v", err)
	}
	_, err = m.CreateRelease(context.Background(), CreateReleaseParams{TaskIDs: []string{"APP-1", "APP-2"}, ServiceVersions: map[string]string{"api": "1.2.3"}, StartImmediately: true, ConfirmedTaskMergePlan: &plan})

	// Then
	if err == nil {
		t.Fatalf("CreateRelease() error=nil, want readiness drift error")
	}
	if got := f.mergeNumbers; len(got) != 1 || got[0] != 1 {
		t.Fatalf("merge order = %#v, want only [1]", got)
	}
	if len(gitMock.createBranchFromBranchCalls) != 0 {
		t.Fatalf("release branch calls = %#v, want none", gitMock.createBranchFromBranchCalls)
	}
	releases, loadErr := m.listReleaseManifests()
	if loadErr != nil {
		t.Fatalf("listReleaseManifests() error = %v", loadErr)
	}
	if releases[0].Status != domain.ReleaseStatusTaskMergePartial {
		t.Fatalf("release status = %q, want partial", releases[0].Status)
	}
}

func TestCreateRelease_ReleasePrepareFreshGateBlocksSourceDriftBeforeManifest(t *testing.T) {
	// Given
	gitMock := &mockGitClient{remoteURLRes: "git@github.com:org/repo.git"}
	m, _ := newReleasePlanTestManager(t, gitMock)
	setTaskWorktreeHeads(gitMock, map[string]string{filepath.Join(m.cfg.TasksRoot, "APP-1", "api"): "head-a"})
	enableReleasePrepareTaskMerge(t, m)
	f := newReleaseTaskMergeForge()
	f.readiness[1] = forge.MRReadiness{Number: 1, State: "open", SourceBranch: "feature/APP-1", TargetBranch: "develop", HeadSHA: "head-a", Ready: true, SupportsSHAPin: true}
	m.forgeClients = map[forge.ForgeProvider]forge.ForgeClient{forge.ForgeProviderGitHub: f}
	seedReleasePlanTasks(t, m.cfg.TasksRoot, gitMock,
		releasePlanTaskService{TaskID: "APP-1", ServiceName: "api", Branch: "feature/APP-1", RepoPath: filepath.Join(m.cfg.RootDir, "repo-api")},
	)
	plan, err := m.PlanReleaseTaskMerges(context.Background(), CreateReleaseParams{TaskIDs: []string{"APP-1"}, ServiceVersions: map[string]string{"api": "1.2.3"}})
	if err != nil {
		t.Fatalf("PlanReleaseTaskMerges() error = %v", err)
	}
	f.readiness[1] = forge.MRReadiness{Number: 1, State: "open", SourceBranch: "feature/APP-1", TargetBranch: "develop", HeadSHA: "head-b", Ready: true, SupportsSHAPin: true}

	// When
	_, err = m.CreateRelease(context.Background(), CreateReleaseParams{TaskIDs: []string{"APP-1"}, ServiceVersions: map[string]string{"api": "1.2.3"}, StartImmediately: true, ConfirmedTaskMergePlan: &plan})

	// Then
	if err == nil {
		t.Fatalf("CreateRelease() error=nil, want source drift error")
	}
	if f.mergeCalls != 0 || len(gitMock.createBranchFromBranchCalls) != 0 {
		t.Fatalf("mutations: merges=%d branches=%#v, want none", f.mergeCalls, gitMock.createBranchFromBranchCalls)
	}
	releases, loadErr := m.listReleaseManifests()
	if loadErr != nil {
		t.Fatalf("listReleaseManifests() error = %v", loadErr)
	}
	if len(releases) != 0 {
		t.Fatalf("release manifests = %#v, want none", releases)
	}
}
