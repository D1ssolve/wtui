package task

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

func TestPlanReleaseCleanup_DefaultsAndPreviewClone(t *testing.T) {
	want := ReleaseCleanupSelection{
		RemoveTasks: true, DeleteLocalTaskBranches: true, RemoveRelease: true, DeleteLocalReleaseBranches: true,
	}
	if got := DefaultReleaseCleanupSelection(); got != want {
		t.Fatalf("defaults = %+v, want %+v", got, want)
	}

	mgr, _ := cleanupTestManager(t, domain.ReleaseStatusReleased)
	plan, err := mgr.PlanReleaseCleanup(t.Context(), "rel-1", want)
	if err != nil {
		t.Fatal(err)
	}
	preview := plan.Preview()
	if len(preview.Blockers) != 0 || len(preview.Tasks) != 1 || len(preview.Services) != 1 {
		t.Fatalf("preview = %+v", preview)
	}
	preview.Tasks[0] = "CORRUPT"
	preview.Services[0].TaskBranches[0] = "CORRUPT"
	again := plan.Preview()
	if again.Tasks[0] != "APP-1" || again.Services[0].TaskBranches[0] != "feature/APP-1" {
		t.Fatalf("plan preview mutated: %+v", again)
	}
}

func TestPlanReleaseCleanup_PreviewIncludesValidatedTaskWorktree(t *testing.T) {
	mgr, _ := cleanupTestManager(t, domain.ReleaseStatusReleased)
	plan, err := mgr.PlanReleaseCleanup(t.Context(), "rel-1", DefaultReleaseCleanupSelection())
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(mgr.cfg.TasksRoot, "APP-1", "svc")
	if !slices.Contains(plan.Preview().Services[0].Worktrees, want) {
		t.Fatalf("preview worktrees = %q, want %q", plan.Preview().Services[0].Worktrees, want)
	}
}

func TestPlanReleaseCleanup_NonReleasedBlockedWithoutMutation(t *testing.T) {
	mgr, gitMock := cleanupTestManager(t, domain.ReleaseStatusPrepared)
	plan, err := mgr.PlanReleaseCleanup(t.Context(), "rel-1", DefaultReleaseCleanupSelection())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Preview().Blockers) == 0 {
		t.Fatal("expected released-only blocker")
	}
	if len(gitMock.removeWorktreeCalls) != 0 || gitMock.deleteBranchCalls != 0 {
		t.Fatal("planner mutated git")
	}
}

func TestPlanReleaseCleanup_RejectsHotfixAsReleasePrefixException(t *testing.T) {
	mgr, gitMock := cleanupTestManager(t, domain.ReleaseStatusReleased)
	release, err := mgr.loadReleaseManifest("rel-1")
	if err != nil {
		t.Fatal(err)
	}
	release.Services[0].ReleaseBranch = "hotfix/owned-looking"
	if _, err := mgr.writeReleaseManifest(release); err != nil {
		t.Fatal(err)
	}
	gitMock.listWorktreesRes[1].Branch = "refs/heads/hotfix/owned-looking"
	gitMock.resolveRefFn = func(_ string, ref string) (string, error) {
		switch ref {
		case "refs/heads/feature/APP-1":
			return "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil
		case "refs/heads/hotfix/owned-looking":
			return "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", nil
		case "refs/tags/v1.0.0^{}":
			return "cccccccccccccccccccccccccccccccccccccccc", nil
		}
		return "", nil
	}
	gitMock.remoteRefSHAFn = func(_ string, ref string) (string, error) {
		switch ref {
		case "refs/heads/feature/APP-1":
			return "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil
		case "refs/heads/hotfix/owned-looking", "refs/heads/develop":
			return "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", nil
		case "refs/tags/v1.0.0^{}", "refs/heads/master":
			return "cccccccccccccccccccccccccccccccccccccccc", nil
		}
		return "", nil
	}
	plan, err := mgr.PlanReleaseCleanup(t.Context(), "rel-1", DefaultReleaseCleanupSelection())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Preview().Blockers) == 0 {
		t.Fatal("hotfix branch received release-prefix protection exception")
	}
}

func TestPlanReleaseCleanup_RejectsWrongManifestIntegrationBranch(t *testing.T) {
	mgr, _ := cleanupTestManager(t, domain.ReleaseStatusReleased)
	release, err := mgr.loadReleaseManifest("rel-1")
	if err != nil {
		t.Fatal(err)
	}
	release.Services[0].IntegrationBranch = "staging"
	if _, err := mgr.writeReleaseManifest(release); err != nil {
		t.Fatal(err)
	}
	plan, err := mgr.PlanReleaseCleanup(t.Context(), "rel-1", DefaultReleaseCleanupSelection())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(plan.Preview().Blockers, "\n"), "does not match resolved integration branch") {
		t.Fatalf("blockers = %q", plan.Preview().Blockers)
	}
}

func TestPlanReleaseCleanup_UsesFreshMergeTargetSHA(t *testing.T) {
	mgr, gitMock := cleanupTestManager(t, domain.ReleaseStatusReleased)
	mgr.flow.BranchTypes[gitflow.BranchTypeFeature] = gitflow.BranchTypeRule{Prefixes: []string{"feature/"}, MergeTargets: []string{"qa"}}
	const targetSHA = "dddddddddddddddddddddddddddddddddddddddd"
	baseRemoteRef := gitMock.remoteRefSHAFn
	gitMock.remoteRefSHAFn = func(repoPath, ref string) (string, error) {
		if ref == "refs/heads/qa" {
			return targetSHA, nil
		}
		return baseRemoteRef(repoPath, ref)
	}
	plan, err := mgr.PlanReleaseCleanup(t.Context(), "rel-1", DefaultReleaseCleanupSelection())
	if err != nil || len(plan.Preview().Blockers) != 0 {
		t.Fatalf("plan = %+v, %v", plan.Preview(), err)
	}
	found := false
	for _, call := range gitMock.isAncestorCalls {
		if call.Ancestor == "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
			found = true
			if call.Descendant != targetSHA {
				t.Fatalf("task merge descendant = %q, want fresh SHA %q", call.Descendant, targetSHA)
			}
		}
	}
	if !found {
		t.Fatal("task merge ancestry check not called")
	}
	for _, step := range plan.steps {
		if step.kind == cleanupLocalTaskBranch {
			if len(step.targets) != 1 || step.targets[0].ref != "refs/heads/qa" || step.targets[0].plannedSHA != targetSHA {
				t.Fatalf("planned targets = %+v", step.targets)
			}
			return
		}
	}
	t.Fatal("local task branch step missing")
}

func TestPlanReleaseCleanup_SafetyBlockers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		want   string
		mutate func(*manager, *mockGitClient)
	}{
		{"locked worktree", "locked", func(_ *manager, g *mockGitClient) { g.listWorktreesRes[0].Locked = true }},
		{"dirty worktree", "dirty", func(_ *manager, g *mockGitClient) { g.isDirtyRes = true }},
		{"local tag moved", "local tag moved", func(_ *manager, g *mockGitClient) {
			base := g.resolveRefFn
			g.resolveRefFn = func(repo, ref string) (string, error) {
				if ref == "refs/tags/v1.0.0^{}" {
					return strings.Repeat("d", 40), nil
				}
				return base(repo, ref)
			}
		}},
		{"remote tag moved", "remote tag moved", func(_ *manager, g *mockGitClient) {
			base := g.remoteRefSHAFn
			g.remoteRefSHAFn = func(repo, ref string) (string, error) {
				if ref == "refs/tags/v1.0.0^{}" {
					return strings.Repeat("d", 40), nil
				}
				return base(repo, ref)
			}
		}},
		{"task path mismatch", "directory mismatch", func(m *manager, _ *mockGitClient) {
			r, _ := m.loadReleaseManifest("rel-1")
			r.Tasks[0].TaskDir = filepath.Join(m.cfg.TasksRoot, "OTHER")
			_, _ = m.writeReleaseManifest(r)
		}},
		{"mapping mismatch", "missing feature mapping", func(m *manager, _ *mockGitClient) {
			r, _ := m.loadReleaseManifest("rel-1")
			r.Services[0].FeatureBranches = nil
			_, _ = m.writeReleaseManifest(r)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr, gitMock := cleanupTestManager(t, domain.ReleaseStatusReleased)
			tc.mutate(mgr, gitMock)
			plan, err := mgr.PlanReleaseCleanup(t.Context(), "rel-1", DefaultReleaseCleanupSelection())
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.Join(plan.Preview().Blockers, "\n"), tc.want) {
				t.Fatalf("blockers = %q, want %q", plan.Preview().Blockers, tc.want)
			}
		})
	}
}

func TestPlanReleaseCleanup_RemoteOptionsCreateLeasedBranchSteps(t *testing.T) {
	mgr, _ := cleanupTestManager(t, domain.ReleaseStatusReleased)
	selection := ReleaseCleanupSelection{RemoveTasks: true, DeleteRemoteTaskBranches: true, RemoveRelease: true, DeleteRemoteReleaseBranches: true}
	plan, err := mgr.PlanReleaseCleanup(t.Context(), "rel-1", selection)
	if err != nil || len(plan.Preview().Blockers) != 0 {
		t.Fatalf("plan = %+v, %v", plan.Preview(), err)
	}
	kinds := map[releaseCleanupStepKind]int{}
	for _, step := range plan.steps {
		kinds[step.kind]++
	}
	if kinds[cleanupRemoteTaskBranch] != 1 || kinds[cleanupRemoteReleaseBranch] != 1 || kinds[cleanupLocalTaskBranch] != 0 || kinds[cleanupLocalReleaseBranch] != 0 {
		t.Fatalf("step kinds = %+v", kinds)
	}
}

func cleanupTestManager(t *testing.T, status domain.ReleaseStatus) (*manager, *mockGitClient) {
	t.Helper()
	root := t.TempDir()
	tasksRoot := filepath.Join(root, ".tasks")
	releaseRoot := filepath.Join(root, ".releases")
	repo := filepath.Join(root, "svc")
	for _, dir := range []string{repo, filepath.Join(tasksRoot, "APP-1", "svc"), filepath.Join(releaseRoot, "rel-1", "services", "svc")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{RootDir: root, TasksRoot: tasksRoot, BaseBranch: "develop", Release: &config.ReleaseConfig{RootDir: releaseRoot}}
	flow := &gitflow.ResolvedGitFlow{ProductionBranch: "master", IntegrationBranch: "develop", BranchTypes: map[gitflow.BranchType]gitflow.BranchTypeRule{
		gitflow.BranchTypeFeature: {Prefixes: []string{"feature/"}}, gitflow.BranchTypeRelease: {Prefixes: []string{"release/"}},
	}}
	const taskSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const releaseSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const acceptedSHA = "cccccccccccccccccccccccccccccccccccccccc"
	release := domain.Release{ManifestVersion: releaseManifestVersion, ID: "rel-1", Dir: filepath.Join(releaseRoot, "rel-1"), Status: status, TaskIDs: []string{"APP-1"},
		Tasks: []domain.ReleaseTaskRef{{TaskID: "APP-1", TaskDir: filepath.Join(tasksRoot, "APP-1"), ServiceNames: []string{"svc"}}},
		Services: []domain.ReleaseService{{Name: "svc", RepoPath: repo, ReleaseWorktreePath: filepath.Join(releaseRoot, "rel-1", "services", "svc"), IntegrationBranch: "develop", ReleaseBranch: "release/1.0.0", Tag: "v1.0.0", ReleaseSHA: releaseSHA, AcceptedMergeSHA: acceptedSHA, PushedTag: true, PushedReleaseBranch: true,
			FeatureBranches: []domain.ReleaseFeatureBranch{{TaskID: "APP-1", ServiceName: "svc", Branch: "feature/APP-1", WorktreePath: filepath.Join(tasksRoot, "APP-1", "svc"), Merged: true, MergeRef: taskSHA}}}},
	}
	manifestDir := filepath.Join(releaseRoot, "rel-1")
	data, _ := json.Marshal(release)
	if err := os.WriteFile(filepath.Join(manifestDir, releaseManifestFileName), data, 0o600); err != nil {
		t.Fatal(err)
	}

	gitMock := &mockGitClient{branchExistsRes: true}
	gitMock.resolveRefFn = func(_ string, ref string) (string, error) {
		switch ref {
		case "refs/heads/feature/APP-1":
			return taskSHA, nil
		case "refs/heads/release/1.0.0":
			return releaseSHA, nil
		case "refs/tags/v1.0.0^{}":
			return acceptedSHA, nil
		}
		return "", nil
	}
	gitMock.remoteRefSHAFn = func(_ string, ref string) (string, error) {
		switch ref {
		case "refs/heads/feature/APP-1":
			return taskSHA, nil
		case "refs/heads/release/1.0.0":
			return releaseSHA, nil
		case "refs/tags/v1.0.0^{}":
			return acceptedSHA, nil
		case "refs/heads/master":
			return acceptedSHA, nil
		case "refs/heads/develop":
			return releaseSHA, nil
		}
		return "", nil
	}
	gitMock.isAncestorFn = func(_, _, _ string) (bool, error) { return true, nil }
	gitMock.listWorktreesRes = []git.WorktreeEntry{
		{Path: filepath.Join(tasksRoot, "APP-1", "svc"), HEAD: taskSHA, Branch: "refs/heads/feature/APP-1"},
		{Path: filepath.Join(releaseRoot, "rel-1", "services", "svc"), HEAD: releaseSHA, Branch: "refs/heads/release/1.0.0"},
	}
	gitMock.commonDirResult = filepath.Join(repo, ".git")
	resolver := cleanupResolver{repo: repo}
	return &manager{cfg: cfg, git: gitMock, discoverer: resolver, flow: flow}, gitMock
}

type cleanupResolver struct{ repo string }

func (r cleanupResolver) Resolve(context.Context, string) (string, error) { return r.repo, nil }
func (r cleanupResolver) FindAll(context.Context) ([]domain.Repo, error)  { return nil, nil }
