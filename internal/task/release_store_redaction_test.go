package task

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/domain"
)

func TestWriteReleaseManifest_RedactsErrorsWithoutChangingReleaseIdentity(t *testing.T) {
	root := t.TempDir()
	m := newTestManager(t, filepath.Join(root, ".tasks"), root, &mockGitClient{}).(*manager)
	const releaseURL = "https://rel-user:O'RELEASE-MARKER@host.invalid/repo"
	const serviceURL = "ssh://svc-user:SERVICE-MARKER@host.invalid:22/repo"
	const operationalURL = "https://operational:OPAQUE-MARKER@host.invalid/review/7"
	const oid = "1234567890123456789012345678901234567890"
	release := domain.Release{
		ID: "rel-fixture", Status: domain.ReleaseStatusFailed, FlowFingerprint: "flow-fixture",
		Error: &domain.ReleaseError{Code: "ERR_RELEASE", Stage: "push", Recoverable: true, Message: "push to " + releaseURL, Cause: "remote rejected " + releaseURL},
		Services: []domain.ReleaseService{{
			Name: "api", AcceptedMergeSHA: oid, ProductionMR: &domain.ProductionMRRef{URL: operationalURL, SourceSHA: oid},
			Error: &domain.ReleaseError{Code: "ERR_SERVICE", Stage: "merge", Message: "merge at " + serviceURL, Cause: "refused " + serviceURL},
		}},
	}

	written, err := m.writeReleaseManifest(release)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(m.releaseManifestPath(release.ID))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := m.loadReleaseManifest(release.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "RELEASE-MARKER") || strings.Contains(string(raw), "SERVICE-MARKER") {
		t.Error("manifest contains diagnostic URL credentials")
	}
	for _, got := range []domain.Release{written, loaded} {
		main, service := got.Error, got.Services[0].Error
		if main == nil || *main != (domain.ReleaseError{Code: "ERR_RELEASE", Stage: "push", Recoverable: true,
			Message: "push to https://***@host.invalid/repo", Cause: "remote rejected https://***@host.invalid/repo"}) ||
			service == nil || *service != (domain.ReleaseError{Code: "ERR_SERVICE", Stage: "merge",
			Message: "merge at ssh://***@host.invalid:22/repo", Cause: "refused ssh://***@host.invalid:22/repo"}) {
			t.Error("release and service error text must retain the nonsecret diagnostic and retry metadata")
		}
		svc := got.Services[0]
		if got.FlowFingerprint != "flow-fixture" || svc.ProductionMR == nil || svc.ProductionMR.URL != operationalURL ||
			svc.ProductionMR.SourceSHA != oid || svc.AcceptedMergeSHA != oid {
			t.Fatal("manifest changed operational URL, fingerprint, or ref identity")
		}
	}
	if release.Error.Cause != "remote rejected "+releaseURL || release.Services[0].Error.Cause != "refused "+serviceURL {
		t.Fatal("writer mutated caller-owned release or service error")
	}
}
