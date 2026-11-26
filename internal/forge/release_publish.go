package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

func splitProject(p PublishParams) (host, project string, err error) {
	host, project, ok := strings.Cut(p.Repo, "/")
	if !ok || host == "" || !strings.Contains(project, "/") || p.Tag == "" || p.Title == "" || p.CommitSHA == "" {
		return "", "", errors.New("publishing a release requires a host-qualified project, a tag, a title and the accepted commit")
	}
	return host, project, nil
}

const ghReleaseQuery = `query($owner: String!, $name: String!, $tag: String!, $ref: String!) {
  repository(owner: $owner, name: $name) {
    nameWithOwner
    release(tagName: $tag) { url tagName tagCommit { oid } }
    ref(qualifiedName: $ref) { target { oid ... on Tag { target { oid } } } }
  }
}`

// PublishRelease returns the GitHub release of an existing tag, creating it only when a
// successful GraphQL read shows the release absent. Error text never counts as absence:
// gh release view reports some failed lookups as "release not found".
func (c *GhClient) PublishRelease(ctx context.Context, p PublishParams) (string, error) {
	host, slug, err := splitProject(p)
	if err != nil {
		return "", err
	}
	owner, name, _ := strings.Cut(slug, "/")
	worktree := pickWorktree(c.worktreePath, p.WorktreePath)
	out, _, err := c.run(ctx, worktree, "api", "graphql", "--hostname", host, "-f", "query="+ghReleaseQuery,
		"-f", "owner="+owner, "-f", "name="+name, "-f", "tag="+p.Tag, "-f", "ref=refs/tags/"+p.Tag)
	if err != nil {
		return "", fmt.Errorf("look up GitHub release %s: %w", p.Tag, err)
	}
	var resp struct {
		Data struct {
			Repository *struct {
				NameWithOwner string `json:"nameWithOwner"`
				// Raw, so a missing field (unproven) differs from an explicit null (absent).
				Release json.RawMessage `json:"release"`
				Ref     *struct {
					Target struct {
						OID    string `json:"oid"`
						Target *struct {
							OID string `json:"oid"`
						} `json:"target"`
					} `json:"target"`
				} `json:"ref"`
			} `json:"repository"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return "", &ForgeError{Category: ErrCategoryParseError, Cause: err, Stderr: out}
	}
	repo := resp.Data.Repository
	if len(resp.Errors) > 0 || repo == nil {
		return "", fmt.Errorf("look up GitHub release %s: incomplete response for %s", p.Tag, p.Repo)
	}
	// gh follows renames, so the canonical name must equal the project the tag was verified at.
	if !strings.EqualFold(repo.NameWithOwner, slug) {
		return "", fmt.Errorf("GitHub resolves %s to %s; refusing to publish to a different project", p.Repo, repo.NameWithOwner)
	}
	if repo.Ref == nil {
		return "", fmt.Errorf("tag %s is not on GitHub %s; publish never creates tags", p.Tag, slug)
	}
	commit := repo.Ref.Target.OID
	if repo.Ref.Target.Target != nil {
		commit = repo.Ref.Target.Target.OID
	}
	if commit != p.CommitSHA {
		return "", fmt.Errorf("GitHub tag %s points to %s, want commit %s", p.Tag, commit, p.CommitSHA)
	}
	if len(repo.Release) == 0 {
		return "", fmt.Errorf("GitHub response for %s has no release field; absence is unproven", p.Tag)
	}
	if string(repo.Release) != "null" {
		var rel struct {
			URL       string `json:"url"`
			TagName   string `json:"tagName"`
			TagCommit struct {
				OID string `json:"oid"`
			} `json:"tagCommit"`
		}
		if err := json.Unmarshal(repo.Release, &rel); err != nil {
			return "", &ForgeError{Category: ErrCategoryParseError, Cause: err, Stderr: out}
		}
		if rel.TagName != p.Tag || rel.TagCommit.OID != p.CommitSHA || rel.URL == "" {
			return "", fmt.Errorf("GitHub release for %s is %q at commit %s, want commit %s", p.Tag, rel.TagName, rel.TagCommit.OID, p.CommitSHA)
		}
		return rel.URL, nil
	}

	notes, err := os.CreateTemp("", "wtui-release-notes-*.md")
	if err != nil {
		return "", err
	}
	defer os.Remove(notes.Name())
	_, werr := notes.WriteString(p.Notes)
	if err := errors.Join(werr, notes.Close()); err != nil {
		return "", fmt.Errorf("write release notes: %w", err)
	}
	out, stderr, err := c.run(ctx, worktree, "release", "create", p.Tag, "--verify-tag", "--title", p.Title, "--notes-file", notes.Name(), "--repo", p.Repo)
	if err != nil {
		return "", fmt.Errorf("create GitHub release %s (the tag must already exist on GitHub): %w", p.Tag, err)
	}
	link := extractFirstURL(out)
	if link == "" {
		link = extractFirstURL(stderr)
	}
	if link == "" {
		return "", &ForgeError{Category: ErrCategoryParseError, Cause: errors.New("gh release create: missing release URL in output"), Stderr: out}
	}
	return link, nil
}

// PublishRelease returns the GitLab release of an existing tag, creating it only when a
// successful Tags API read shows the tag at the accepted commit with "release": null. Error
// text never counts as absence. The create call sends no ref, so GitLab rejects it instead of
// creating a missing tag (glab release create would).
func (c *GlabClient) PublishRelease(ctx context.Context, p PublishParams) (string, error) {
	host, project, err := splitProject(p)
	if err != nil {
		return "", err
	}
	worktree := pickWorktree(c.worktreePath, p.WorktreePath)
	base := "projects/" + url.PathEscape(project)
	out, _, err := c.run(ctx, worktree, "api", base, "--hostname", host)
	if err != nil {
		return "", fmt.Errorf("read GitLab project %s: %w", p.Repo, err)
	}
	var proj struct {
		Path string `json:"path_with_namespace"`
	}
	if err := json.Unmarshal([]byte(out), &proj); err != nil {
		return "", &ForgeError{Category: ErrCategoryParseError, Cause: err, Stderr: out}
	}
	if !strings.EqualFold(proj.Path, project) {
		return "", fmt.Errorf("GitLab resolves %s to %s; refusing to publish to a different project", p.Repo, proj.Path)
	}

	escapedTag := url.PathEscape(p.Tag)
	out, _, err = c.run(ctx, worktree, "api", base+"/repository/tags/"+escapedTag, "--hostname", host)
	if err != nil {
		return "", fmt.Errorf("look up GitLab release %s: read the tag (it must exist on GitLab; publish never creates tags): %w", p.Tag, err)
	}
	var tag struct {
		Name   string `json:"name"`
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
		Release json.RawMessage `json:"release"`
	}
	if err := json.Unmarshal([]byte(out), &tag); err != nil {
		return "", &ForgeError{Category: ErrCategoryParseError, Cause: err, Stderr: out}
	}
	if tag.Name != p.Tag || tag.Commit.ID != p.CommitSHA {
		return "", fmt.Errorf("GitLab tag %s points to %s at commit %s, want commit %s", p.Tag, tag.Name, tag.Commit.ID, p.CommitSHA)
	}
	releases := base + "/releases"
	switch string(bytes.TrimSpace(tag.Release)) {
	case "":
		return "", fmt.Errorf("look up GitLab release %s: the tag response has no release field, so absence is unproven", p.Tag)
	case "null":
		out, _, err = c.run(ctx, worktree, "api", releases, "--hostname", host, "--method", "POST",
			"--raw-field", "tag_name="+p.Tag, "--raw-field", "name="+p.Title, "--raw-field", "description="+p.Notes)
		if err != nil {
			return "", fmt.Errorf("create GitLab release %s (the tag must already exist on GitLab): %w", p.Tag, err)
		}
	default:
		if out, _, err = c.run(ctx, worktree, "api", releases+"/"+escapedTag, "--hostname", host); err != nil {
			return "", fmt.Errorf("read existing GitLab release %s: %w", p.Tag, err)
		}
	}
	return glabReleaseURL(out, p)
}

func glabReleaseURL(out string, p PublishParams) (string, error) {
	var rel struct {
		TagName string `json:"tag_name"`
		Commit  struct {
			ID string `json:"id"`
		} `json:"commit"`
		Links struct {
			Self string `json:"self"`
		} `json:"_links"`
	}
	if err := json.Unmarshal([]byte(out), &rel); err != nil {
		return "", &ForgeError{Category: ErrCategoryParseError, Cause: err, Stderr: out}
	}
	if rel.TagName != p.Tag || rel.Commit.ID != p.CommitSHA {
		return "", fmt.Errorf("GitLab release %s points to %s at commit %s, want commit %s", p.Tag, rel.TagName, rel.Commit.ID, p.CommitSHA)
	}
	if rel.Links.Self == "" {
		return "", &ForgeError{Category: ErrCategoryParseError, Cause: errors.New("GitLab release: missing URL"), Stderr: out}
	}
	return rel.Links.Self, nil
}
