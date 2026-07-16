package task

import (
	"context"
	"sync"

	"github.com/D1ssolve/wtui/internal/git"
	"golang.org/x/sync/singleflight"
)

type gitCache struct {
	mu           sync.Mutex
	worktrees    map[string][]git.WorktreeEntry
	baseBranches map[string]string
	group        singleflight.Group
}

func newGitCache() *gitCache {
	return &gitCache{
		worktrees:    make(map[string][]git.WorktreeEntry),
		baseBranches: make(map[string]string),
	}
}

func cloneWorktreeEntries(in []git.WorktreeEntry) []git.WorktreeEntry {
	if in == nil {
		return nil
	}
	out := make([]git.WorktreeEntry, len(in))
	copy(out, in)
	return out
}

func (c *gitCache) listWorktrees(ctx context.Context, g git.Client, repoPath string) ([]git.WorktreeEntry, error) {
	c.mu.Lock()
	if entries, ok := c.worktrees[repoPath]; ok {
		c.mu.Unlock()
		return cloneWorktreeEntries(entries), nil
	}
	c.mu.Unlock()

	v, err, _ := c.group.Do("worktrees:"+repoPath, func() (any, error) {
		c.mu.Lock()
		if entries, ok := c.worktrees[repoPath]; ok {
			c.mu.Unlock()
			return cloneWorktreeEntries(entries), nil
		}
		c.mu.Unlock()

		entries, err := g.ListWorktrees(ctx, repoPath)
		if err != nil {
			return nil, err
		}

		cloned := cloneWorktreeEntries(entries)

		c.mu.Lock()
		c.worktrees[repoPath] = cloned
		c.mu.Unlock()

		return cloneWorktreeEntries(cloned), nil
	})
	if err != nil {
		return nil, err
	}

	entries, _ := v.([]git.WorktreeEntry)
	return cloneWorktreeEntries(entries), nil
}

func (c *gitCache) getBaseBranch(ctx context.Context, g git.Client, repoPath string) (string, error) {
	c.mu.Lock()
	if branch, ok := c.baseBranches[repoPath]; ok {
		c.mu.Unlock()
		return branch, nil
	}
	c.mu.Unlock()

	v, err, _ := c.group.Do("base-branch:"+repoPath, func() (any, error) {
		c.mu.Lock()
		if branch, ok := c.baseBranches[repoPath]; ok {
			c.mu.Unlock()
			return branch, nil
		}
		c.mu.Unlock()

		branch, err := g.BaseBranch(ctx, repoPath)
		if err != nil {
			return "", err
		}

		c.mu.Lock()
		c.baseBranches[repoPath] = branch
		c.mu.Unlock()

		return branch, nil
	})
	if err != nil {
		return "", err
	}

	branch, _ := v.(string)
	return branch, nil
}
