package task

import (
	"errors"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"gopkg.in/yaml.v3"
)

func TestEnsureReleaseFlowUnchanged_RemoteSwitchBlocks(t *testing.T) {
	m, _ := newReleasePlanTestManager(t, &mockGitClient{})
	setRemote := func(doc string) {
		t.Helper()
		var forge config.ForgeConfig
		if err := yaml.Unmarshal([]byte(doc), &forge); err != nil {
			t.Fatal(err)
		}
		m.cfg.Forge = &forge
	}
	setRemote("{}")
	if got, want := m.releaseFingerprint(), m.flow.ReleaseFingerprint(); got != want {
		t.Fatalf("default remote changed the fingerprint of in-flight releases: %q != %q", got, want)
	}
	underOrigin := domain.Release{ID: "rel-origin", FlowFingerprint: m.releaseFingerprint()}
	setRemote("remote: upstream")
	underUpstream := domain.Release{ID: "rel-upstream", FlowFingerprint: m.releaseFingerprint()}
	if err := m.ensureReleaseFlowUnchanged(underOrigin); !errors.Is(err, ErrReleaseFlowChanged) {
		t.Fatalf("release started on origin continued on upstream: %v", err)
	}
	if err := m.ensureReleaseFlowUnchanged(underUpstream); err != nil {
		t.Fatalf("same remote refused: %v", err)
	}
	setRemote("{}")
	if err := m.ensureReleaseFlowUnchanged(underUpstream); !errors.Is(err, ErrReleaseFlowChanged) {
		t.Fatalf("release started on upstream continued on origin: %v", err)
	}
}

func TestReleaseDeletion_RefusesAnotherForgeRemote(t *testing.T) {
	mgr, _ := cleanupTestManager(t, domain.ReleaseStatusPrepared)
	var forge config.ForgeConfig
	if err := yaml.Unmarshal([]byte("remote: upstream"), &forge); err != nil {
		t.Fatal(err)
	}
	mgr.cfg.Forge = &forge // the release was started under the default origin
	plan, err := mgr.PlanReleaseCleanup(t.Context(), "rel-1", ReleaseDeletionSelection())
	if err != nil {
		t.Fatal(err)
	}
	if blockers := strings.Join(plan.Preview().Blockers, "; "); !strings.Contains(blockers, "forge.remote") {
		t.Fatalf("deletion under another remote was not blocked: %q", blockers)
	}
}

func TestReleaseDeletion_RemoteIdentityFollowsRepositoryNotName(t *testing.T) {
	mgr, g := cleanupTestManager(t, domain.ReleaseStatusPrepared)
	release, err := mgr.loadReleaseManifest("rel-1")
	if err != nil {
		t.Fatal(err)
	}
	release.Services[0].RemoteIdentity = "https://example.com/team/svc.git"
	if _, err := mgr.writeReleaseManifest(release); err != nil {
		t.Fatal(err)
	}
	var forge config.ForgeConfig
	if err := yaml.Unmarshal([]byte("remote: upstream"), &forge); err != nil {
		t.Fatal(err)
	}
	mgr.cfg.Forge = &forge
	for _, tc := range []struct {
		url     string
		blocked bool
	}{
		{"https://token@example.com/team/svc.git", false}, // renamed remote, same repository
		{"https://example.com/fork/svc.git", true},
	} {
		g.remoteURLRes = tc.url
		plan, err := mgr.PlanReleaseCleanup(t.Context(), "rel-1", ReleaseDeletionSelection())
		if err != nil {
			t.Fatal(err)
		}
		if blocked := strings.Contains(strings.Join(plan.Preview().Blockers, "; "), "where the release was created"); blocked != tc.blocked {
			t.Errorf("%s: blocked=%t, want %t (%q)", tc.url, blocked, tc.blocked, plan.Preview().Blockers)
		}
	}
}
