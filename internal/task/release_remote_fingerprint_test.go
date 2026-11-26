package task

import (
	"errors"
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
