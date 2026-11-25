package task

import (
	"context"
	"fmt"

	"github.com/D1ssolve/wtui/internal/conventional"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/Masterminds/semver/v3"
)

func (m *manager) conventionalBump() bool {
	return m.cfg != nil && m.cfg.Tag != nil && m.cfg.Tag.Bump == "conventional"
}

// nextVersion proposes the version after latest for the changes in latestTag..to:
// a patch with tag.bump manual, the Conventional Commits bump with tag.bump conventional.
func (m *manager) nextVersion(ctx context.Context, repoPath, latestTag string, latest *semver.Version, to string) (string, error) {
	bump := conventional.BumpPatch
	if m.conventionalBump() && latest != nil {
		commits, err := m.git.CommitLog(ctx, repoPath, latestTag, to)
		if err != nil {
			return "", fmt.Errorf("read commits %s..%s: %w", latestTag, to, err)
		}
		bump = conventional.BumpOf(commits)
	}
	return conventional.Next(latest, bump), nil
}

// releaseTagMessageFor uses the entered tag description; with tag.bump conventional and no
// description it lists the changes since the previous version tag as a changelog.
func (m *manager) releaseTagMessageFor(ctx context.Context, release *domain.Release, svc *domain.ReleaseService) (string, error) {
	message := releaseTagMessage(release.ID, svc.TagDescription)
	if svc.TagDescription != "" || !m.conventionalBump() {
		return message, nil
	}
	previous, err := m.git.LatestSemverTag(ctx, svc.RepoPath, svc.AcceptedMergeSHA)
	if err != nil || previous == "" {
		return message, err // the first version has no earlier tag to list changes since
	}
	commits, err := m.git.CommitLog(ctx, svc.RepoPath, previous, svc.AcceptedMergeSHA)
	if err != nil {
		return "", err
	}
	if changelog := conventional.Changelog(commits); changelog != "" {
		message += "\n\n" + changelog
	}
	return message, nil
}
