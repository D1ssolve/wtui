package git

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/Masterminds/semver/v3"
)

const tagListDelimiter = "|"

// tagRef validates tag as a plain refs/tags/<tag> name via the shared
// check-ref-format wrapper; "refs/"-prefixed or option-like input is rejected.
func tagRef(tag string) (string, error) {
	ref := "refs/tags/" + tag
	if tag == "" || strings.HasPrefix(tag, "refs/") || strings.Contains(tag, "^{") || !validRemoteRef(ref) {
		return "", fmt.Errorf("invalid tag %q", tag)
	}
	return ref, nil
}

func (c *CommandClient) CreateLightweightTag(ctx context.Context, repoPath, tag, ref string) error {
	if _, err := tagRef(tag); err != nil {
		return err
	}
	_, err := c.execGit(ctx, "-C", repoPath, "tag", "--", tag, ref)
	return err
}

func (c *CommandClient) CreateTag(ctx context.Context, repoPath, tag, ref, message string) error {
	if _, err := tagRef(tag); err != nil {
		return err
	}
	_, err := c.execGit(ctx, "-C", repoPath, "tag", "-a", "-m", message, "--", tag, ref)
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// PushTag publishes the captured unpeeled tag object OID to refs/tags/<tag>
// on the captured remote URL (never the mutable remote name) without force;
// a tag that moved after verification is rejected by the remote.
func (c *CommandClient) PushTag(ctx context.Context, worktreePath, capturedRemoteURL, tag, tagObjectOID string) error {
	if !validPushRemoteURL(capturedRemoteURL) {
		return fmt.Errorf("invalid push remote URL %q", capturedRemoteURL)
	}
	ref, err := tagRef(tag)
	if err != nil {
		return err
	}
	if !validObjectID(tagObjectOID) {
		return fmt.Errorf("invalid tag object OID %q", tagObjectOID)
	}
	_, err = c.execGit(ctx, "-C", worktreePath, "push", "--", capturedRemoteURL, tagObjectOID+":"+ref)
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func (c *CommandClient) DeleteTag(ctx context.Context, repoPath, tag string) error {
	if _, err := tagRef(tag); err != nil {
		return err
	}
	_, err := c.execGit(ctx, "-C", repoPath, "tag", "-d", "--", tag)
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// DeleteTagIfUnchanged deletes refs/tags/<tag> only when it still points at
// expectedOID, so a concurrent replacement of the tag is never removed.
func (c *CommandClient) DeleteTagIfUnchanged(ctx context.Context, repoPath, tag, expectedOID string) error {
	ref, err := tagRef(tag)
	if err != nil {
		return err
	}
	if !validObjectID(expectedOID) {
		return fmt.Errorf("invalid expected tag OID %q", expectedOID)
	}
	_, err = c.execGit(ctx, "-C", repoPath, "update-ref", "-d", ref, expectedOID)
	return err
}

func (c *CommandClient) ListTags(ctx context.Context, repoPath string) ([]domain.TagInfo, error) {
	tags, err := c.listTags(ctx, repoPath)
	if err != nil {
		return nil, err
	}
	sortTags(tags)
	return tags, nil
}

func (c *CommandClient) TagExists(ctx context.Context, repoPath, tag string) (bool, error) {
	ref, err := tagRef(tag)
	if err != nil {
		return false, err
	}
	_, err = c.execGit(ctx, "-C", repoPath, "show-ref", "--tags", "--verify", "--quiet", ref)
	if err == nil {
		return true, nil
	}

	var execErr *ExecError
	if errors.As(err, &execErr) {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return false, nil
	}

	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	return false, err
}

func (c *CommandClient) LatestSemverTag(ctx context.Context, repoPath, branch string) (string, error) {
	out, err := c.execGit(ctx, "-C", repoPath, "tag", "--merged", branch, "-l", "--format=%(refname:short)")
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", err
	}

	var bestName string
	var bestVersion *semver.Version
	for line := range strings.SplitSeq(out, "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}

		v, parseErr := semver.NewVersion(name)
		if parseErr != nil {
			continue
		}

		if bestVersion == nil || v.GreaterThan(bestVersion) {
			bestName = name
			bestVersion = v
		}
	}

	return bestName, nil
}

func (c *CommandClient) listTags(ctx context.Context, repoPath string) ([]domain.TagInfo, error) {
	out, err := c.execGit(ctx, "-C", repoPath, "tag", "-l",
		"--format=%(refname:short)|%(objectname:short)|%(subject)|%(objecttype)")
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}

	tags := make([]domain.TagInfo, 0)
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, tagListDelimiter, 4)
		if len(parts) != 4 {
			return nil, fmt.Errorf("invalid tag line: %q", line)
		}

		tag := domain.TagInfo{
			Name:        parts[0],
			Ref:         parts[1],
			Message:     parts[2],
			IsAnnotated: parts[3] == "tag",
		}

		if v, parseErr := semver.NewVersion(tag.Name); parseErr == nil {
			tag.IsSemver = true
			tag.Version = v
		}

		tags = append(tags, tag)
	}

	return tags, nil
}

func sortTags(tags []domain.TagInfo) {
	slices.SortStableFunc(tags, func(a, b domain.TagInfo) int {
		switch {
		case a.IsSemver && b.IsSemver:
			return b.Version.Compare(a.Version)
		case a.IsSemver:
			return -1
		case b.IsSemver:
			return 1
		default:
			return strings.Compare(a.Name, b.Name)
		}
	})
}
