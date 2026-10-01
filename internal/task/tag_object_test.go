package task

import (
	"strings"
	"testing"
)

// TestResolveTagObjectSHA_PeelsCapturedOID pins that verification peels the
// immutable tag object OID captured at resolution time, never the mutable tag
// ref name: a concurrent ref replacement after capture must not change what
// gets peeled.
func TestResolveTagObjectSHA_PeelsCapturedOID(t *testing.T) {
	const (
		wantCommit = "cccccccccccccccccccccccccccccccccccccccc"
		tagOID     = "tagoidaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		replaced   = "tagoidbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	m, gitMock := newReleasePlanTestManager(t, &mockGitClient{})
	tagRefCalls := 0
	gitMock.resolveRefFn = func(_ string, ref string) (string, error) {
		switch ref {
		case "refs/tags/v1.0.0":
			// The ref is replaced concurrently after the first resolution.
			tagRefCalls++
			if tagRefCalls == 1 {
				return tagOID, nil
			}
			return replaced, nil
		case tagOID + "^{commit}":
			return wantCommit, nil
		}
		return "", nil
	}

	got, err := m.resolveTagObjectSHA(t.Context(), "/repo", "v1.0.0", wantCommit)
	if err != nil {
		t.Fatalf("resolveTagObjectSHA() error = %v", err)
	}
	if got != tagOID {
		t.Fatalf("resolveTagObjectSHA() = %q, want captured OID %q", got, tagOID)
	}

	gitMock.mu.Lock()
	resolveCalls := append([]resolveRefCall(nil), gitMock.resolveRefCalls...)
	gitMock.mu.Unlock()
	peeledCaptured := false
	for _, call := range resolveCalls {
		if call.Ref == "refs/tags/v1.0.0^{commit}" {
			t.Fatalf("peeled mutable tag ref name; ResolveRef calls = %+v", resolveCalls)
		}
		if call.Ref == tagOID+"^{commit}" {
			peeledCaptured = true
		}
	}
	if !peeledCaptured {
		t.Fatalf("no peel of captured OID %q^{commit}; ResolveRef calls = %+v", tagOID, resolveCalls)
	}
}

// TestResolveTagObjectSHA_CapturedOIDPeelMismatchFailsClosed pins that when the
// captured immutable tag object peels to a different commit than expected, the
// tag is rejected even if the mutable ref now points at a matching tag.
func TestResolveTagObjectSHA_CapturedOIDPeelMismatchFailsClosed(t *testing.T) {
	const (
		wantCommit = "cccccccccccccccccccccccccccccccccccccccc"
		tagOID     = "tagoidaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	m, gitMock := newReleasePlanTestManager(t, &mockGitClient{})
	gitMock.resolveRefFn = func(_ string, ref string) (string, error) {
		switch ref {
		case "refs/tags/v1.0.0":
			return tagOID, nil
		case tagOID + "^{commit}":
			return "dddddddddddddddddddddddddddddddddddddddd", nil
		}
		return "", nil
	}

	if _, err := m.resolveTagObjectSHA(t.Context(), "/repo", "v1.0.0", wantCommit); err == nil || !strings.Contains(err.Error(), "peels to") {
		t.Fatalf("resolveTagObjectSHA() error = %v, want peel mismatch", err)
	}
}
