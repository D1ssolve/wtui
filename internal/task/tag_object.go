package task

import (
	"context"
	"fmt"
)

// resolveTagObjectSHA resolves the unpeeled tag object OID at refs/tags/<tagName>
// once and verifies that captured immutable object peels to wantCommitSHA, so a
// tag ref replaced concurrently after capture is never published: the peel
// never re-reads the mutable ref name.
func (m *manager) resolveTagObjectSHA(ctx context.Context, repoPath, tagName, wantCommitSHA string) (string, error) {
	ref := "refs/tags/" + tagName
	oid, err := m.git.ResolveRef(ctx, repoPath, ref)
	if err != nil {
		return "", fmt.Errorf("resolve tag object %s: %w", tagName, err)
	}
	if oid == "" {
		return "", fmt.Errorf("resolve tag object %s: empty OID", tagName)
	}
	peeled, err := m.git.ResolveRef(ctx, repoPath, oid+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("peel tag %s: %w", tagName, err)
	}
	if peeled != wantCommitSHA {
		return "", fmt.Errorf("tag %s peels to %s, want %s", tagName, peeled, wantCommitSHA)
	}
	return oid, nil
}
