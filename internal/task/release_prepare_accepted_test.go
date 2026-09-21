package task

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/git"
)

func acceptedPrepareFixture(t *testing.T, g *mockGitClient) (*manager, domain.Release) {
	t.Helper()
	m, _ := newReleasePlanTestManager(t, g)
	enableReleasePrepareTaskMerge(t, m)
	*m.cfg.Release.PushReleaseBranches = false
	*m.cfg.Release.KeepIntegrationWorktrees = true
	return m, domain.Release{ID: "rel-accepted", Dir: t.TempDir(), Status: domain.ReleaseStatusMerging,
		Services: []domain.ReleaseService{{Name: "api", RepoPath: "/repos/api", IntegrationBranch: "develop", ReleaseBranch: "release/1.2.3", PostIntegrationSHA: "accepted"}}}
}

func TestPrepareAccepted_TargetMovementBlocks(t *testing.T) {
	for _, stage := range []string{"before preparation", "after worktree", "during branch lookup", "second fetch"} {
		t.Run(stage, func(t *testing.T) {
			// Given: a persisted accepted frontier, followed by an unrelated push.
			tip := "accepted"
			g := &mockGitClient{resolveRefFn: func(_, ref string) (string, error) {
				if ref == "origin/develop" {
					return tip, nil
				}
				return "accepted", nil
			}}
			m, release := acceptedPrepareFixture(t, g)
			switch stage {
			case "before preparation":
				tip = "unrelated"
			case "after worktree":
				g.addWorktreeFn = func(_, _, _ string, _ bool, _ string) error { tip = "unrelated"; return nil }
			case "during branch lookup":
				g.branchExistsFn = func(_, _ string) (bool, error) { tip = "unrelated"; return false, nil }
			case "second fetch":
				fetches := 0
				g.fetchFn = func(string) error {
					fetches++
					if fetches == 2 {
						tip = "unrelated"
					}
					return nil
				}
			}
			// When
			err := m.executePrepareService(t.Context(), &release, &release.Services[0], nil)
			// Then
			if err == nil || len(g.createBranchFromBranchCalls) != 0 {
				t.Fatalf("error=%v branch calls=%v", err, g.createBranchFromBranchCalls)
			}
			if release.Services[0].PostIntegrationSHA != "accepted" {
				t.Fatal("accepted frontier overwritten")
			}
			stored, loadErr := m.loadReleaseManifest(release.ID)
			if loadErr != nil || stored.Services[0].PostIntegrationSHA != "accepted" {
				t.Fatalf("persisted frontier=%+v error=%v", stored, loadErr)
			}
		})
	}
}

func TestPrepareAccepted_UsesLiteralSHA(t *testing.T) {
	// Given: HEAD resolution is not authoritative for acceptance.
	g := &mockGitClient{resolveRefFn: func(_, ref string) (string, error) {
		if ref == "HEAD" {
			return "unrelated-head", nil
		}
		return "accepted", nil
	}}
	m, release := acceptedPrepareFixture(t, g)
	// When
	err := m.executePrepareService(t.Context(), &release, &release.Services[0], nil)
	// Then
	if err != nil {
		t.Fatal(err)
	}
	if len(g.createBranchFromBranchCalls) != 1 || g.createBranchFromBranchCalls[0].FromBranch != "accepted" {
		t.Fatalf("branch calls=%v", g.createBranchFromBranchCalls)
	}
	if len(g.addWorktreeCalls) == 0 || g.addWorktreeCalls[0].Branch != "accepted" {
		t.Fatalf("worktrees=%v", g.addWorktreeCalls)
	}
	if release.Services[0].PostIntegrationSHA != "accepted" || len(g.mergeFFOnlyCalls) != 0 {
		t.Fatal("accepted frontier mutated")
	}
}

func TestPrepareAccepted_ReplayArtifacts(t *testing.T) {
	for _, mismatch := range []string{"", "local", "remote", "integration head", "release head", "release branch", "dirty", "operation", "locked", "unregistered"} {
		t.Run(mismatch, func(t *testing.T) {
			// Given: matching artifacts left by an interrupted preparation.
			g := &mockGitClient{branchExistsRes: true, resolveRefFn: func(_, _ string) (string, error) { return "accepted", nil }, remoteRefSHAFn: func(_, _ string) (string, error) { return "accepted", nil }}
			m, release := acceptedPrepareFixture(t, g)
			integration := filepath.Join(release.Dir, ".work", "api-integration")
			worktree := filepath.Join(release.Dir, "services", "api")
			g.listWorktreesRes = []git.WorktreeEntry{{Path: integration, HEAD: "accepted", Branch: "(detached)"}, {Path: worktree, HEAD: "accepted", Branch: "refs/heads/release/1.2.3"}}
			switch mismatch {
			case "local":
				g.resolveRefFn = func(_, ref string) (string, error) {
					if ref == "release/1.2.3" {
						return "wrong", nil
					}
					return "accepted", nil
				}
			case "remote":
				g.remoteRefSHAFn = func(_, _ string) (string, error) { return "wrong", nil }
			case "integration head":
				g.listWorktreesRes[0].HEAD = "wrong"
			case "release head":
				g.listWorktreesRes[1].HEAD = "wrong"
			case "release branch":
				g.listWorktreesRes[1].Branch = "refs/heads/other"
			case "dirty":
				g.isDirtyRes = true
			case "operation":
				g.operationStateFn = func(string) ([]domain.RepoState, error) { return []domain.RepoState{domain.RepoStateRebasing}, nil }
			case "locked":
				g.listWorktreesRes[0].Locked = true
			case "unregistered":
				g.listWorktreesRes = nil
				release.Services[0].IntegrationWorktreePath = integration
				if err := os.MkdirAll(integration, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			// When
			err := m.executePrepareService(t.Context(), &release, &release.Services[0], nil)
			// Then
			if (err != nil) != (mismatch != "") {
				t.Fatalf("error=%v", err)
			}
			if release.Services[0].PostIntegrationSHA != "accepted" {
				t.Fatal("replay replaced accepted SHA")
			}
			if len(g.createBranchFromBranchCalls) != 0 || len(g.removeWorktreeCalls) != 0 {
				t.Fatal("replay mutated existing artifacts")
			}
			if mismatch == "unregistered" && len(g.addWorktreeCalls) != 0 {
				t.Fatal("attempted to overwrite an unregistered path")
			}
			if mismatch == "" && (len(g.addWorktreeCalls) != 0 || release.Services[0].Status != domain.ReleaseStatusPrepared) {
				t.Fatalf("not resumed: %+v", release.Services[0])
			}
		})
	}
}

func TestPrepareAccepted_ErrorReplayKeepsFrontier(t *testing.T) {
	// Given: release branch creation succeeded, but its push failed.
	failure := errors.New("push unavailable")
	tip, local := "accepted", false
	g := &mockGitClient{
		resolveRefFn:             func(_, _ string) (string, error) { return tip, nil },
		branchExistsFn:           func(_, _ string) (bool, error) { return local, nil },
		createBranchFromBranchFn: func(_, _, _ string) error { local = true; return nil },
		pushBranchExplicitErr:    failure,
	}
	m, release := acceptedPrepareFixture(t, g)
	*m.cfg.Release.PushReleaseBranches = true
	if err := m.executePrepareService(t.Context(), &release, &release.Services[0], nil); err == nil {
		t.Fatal("first prepare must fail")
	}
	stored, err := m.loadReleaseManifest(release.ID)
	if err != nil {
		t.Fatal(err)
	}
	tip = "unrelated"
	// When: replay from disk after integration advances.
	err = m.executePrepareService(t.Context(), &stored, &stored.Services[0], nil)
	// Then: replay blocks rather than replacing the accepted frontier.
	if err == nil || stored.Services[0].PostIntegrationSHA != "accepted" {
		t.Fatalf("replay error=%v service=%+v", err, stored.Services[0])
	}
	if len(g.createBranchFromBranchCalls) != 1 || len(g.pushBranchExplicitCalls) != 1 {
		t.Fatal("replay repeated mutation")
	}
}

func TestPrepareAccepted_RemoteOnlyAdoption(t *testing.T) {
	// Given: only the remote release branch exists at the accepted SHA.
	g := &mockGitClient{resolveRefFn: func(_, _ string) (string, error) { return "accepted", nil }, remoteRefSHAFn: func(_, _ string) (string, error) { return "accepted", nil }}
	m, release := acceptedPrepareFixture(t, g)
	*m.cfg.Release.PushReleaseBranches = true
	// When
	err := m.executePrepareService(t.Context(), &release, &release.Services[0], nil)
	// Then
	if err != nil {
		t.Fatal(err)
	}
	if len(g.createBranchFromBranchCalls) != 1 || g.createBranchFromBranchCalls[0].FromBranch != "accepted" || len(g.pushBranchExplicitCalls) != 0 {
		t.Fatalf("create=%v push=%v", g.createBranchFromBranchCalls, g.pushBranchExplicitCalls)
	}
	if !release.Services[0].PushedReleaseBranch {
		t.Fatal("matching remote not adopted")
	}
}

func TestPrepareAccepted_MissingFrontierBlocks(t *testing.T) {
	// Given
	g := &mockGitClient{}
	m, release := acceptedPrepareFixture(t, g)
	release.Services[0].PostIntegrationSHA = ""
	// When
	err := m.executePrepareService(t.Context(), &release, &release.Services[0], nil)
	// Then
	if !errors.Is(err, ErrReleaseRetryUnsafe) || len(g.fetchCalls) != 0 || release.Services[0].PostIntegrationSHA != "" {
		t.Fatalf("error=%v service=%+v", err, release.Services[0])
	}
}
