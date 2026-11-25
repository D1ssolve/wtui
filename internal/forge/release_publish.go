package forge

import (
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
	if !ok || host == "" || !strings.Contains(project, "/") || p.Tag == "" || p.Title == "" {
		return "", "", errors.New("publishing a release requires a host-qualified project, a tag and a title")
	}
	return host, project, nil
}

// PublishRelease returns the GitHub release of an existing remote tag, creating it only
// after gh reports it absent; --verify-tag refuses to create a missing tag.
func (c *GhClient) PublishRelease(ctx context.Context, p PublishParams) (string, error) {
	_, slug, err := splitProject(p)
	if err != nil {
		return "", err
	}
	worktree := pickWorktree(c.worktreePath, p.WorktreePath)
	// gh follows renames, so the canonical name must equal the project the tag was verified at.
	out, _, err := c.run(ctx, worktree, "repo", "view", p.Repo, "--json", "nameWithOwner")
	if err != nil {
		return "", fmt.Errorf("read GitHub repository %s: %w", p.Repo, err)
	}
	var repo struct {
		NameWithOwner string `json:"nameWithOwner"`
	}
	if err := json.Unmarshal([]byte(out), &repo); err != nil {
		return "", &ForgeError{Category: ErrCategoryParseError, Cause: err, Stderr: out}
	}
	if !strings.EqualFold(repo.NameWithOwner, slug) {
		return "", fmt.Errorf("GitHub resolves %s to %s; refusing to publish to a different project", p.Repo, repo.NameWithOwner)
	}

	out, stderr, err := c.run(ctx, worktree, "release", "view", p.Tag, "--repo", p.Repo, "--json", "url,tagName")
	if err == nil {
		var rel struct {
			URL     string `json:"url"`
			TagName string `json:"tagName"`
		}
		if err := json.Unmarshal([]byte(out), &rel); err != nil {
			return "", &ForgeError{Category: ErrCategoryParseError, Cause: err, Stderr: out}
		}
		if rel.TagName != p.Tag || rel.URL == "" {
			return "", fmt.Errorf("GitHub returned release %q for tag %s", rel.TagName, p.Tag)
		}
		return rel.URL, nil
	}
	if !strings.Contains(strings.ToLower(stderr), "release not found") {
		return "", fmt.Errorf("look up GitHub release %s: %w", p.Tag, err)
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
	out, stderr, err = c.run(ctx, worktree, "release", "create", p.Tag, "--verify-tag", "--title", p.Title, "--notes-file", notes.Name(), "--repo", p.Repo)
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

// PublishRelease returns the GitLab release of an existing tag, creating it only after the
// tag lookup returned 404 in a confirmed project. The create call sends no ref, so
// GitLab rejects it instead of creating a missing tag (glab release create would).
func (c *GlabClient) PublishRelease(ctx context.Context, p PublishParams) (string, error) {
	host, project, err := splitProject(p)
	if err != nil {
		return "", err
	}
	if p.CommitSHA == "" {
		return "", errors.New("publishing a GitLab release requires the accepted commit")
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

	releases := base + "/releases"
	out, stderr, err := c.run(ctx, worktree, "api", releases+"/"+url.PathEscape(p.Tag), "--hostname", host)
	if err == nil {
		return glabReleaseURL(out, p)
	}
	if !strings.Contains(stderr, "404") {
		return "", fmt.Errorf("look up GitLab release %s: %w", p.Tag, err)
	}
	out, _, err = c.run(ctx, worktree, "api", releases, "--hostname", host, "--method", "POST",
		"--raw-field", "tag_name="+p.Tag, "--raw-field", "name="+p.Title, "--raw-field", "description="+p.Notes)
	if err != nil {
		return "", fmt.Errorf("create GitLab release %s (the tag must already exist on GitLab): %w", p.Tag, err)
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
