package discovery

import (
	"context"
	"fmt"
	"sync"

	"github.com/D1ssolve/wtui/internal/domain"
)

type repoResolver interface {
	Resolve(ctx context.Context, token string) (string, error)
	FindAll(ctx context.Context) ([]domain.Repo, error)
}

type CachedDiscoverer struct {
	wrapped repoResolver

	mu     sync.Mutex
	loaded bool
	repos  []domain.Repo
}

func NewCached(wrapped repoResolver) *CachedDiscoverer {
	return &CachedDiscoverer{wrapped: wrapped}
}

func (c *CachedDiscoverer) Resolve(ctx context.Context, token string) (string, error) {
	c.mu.Lock()
	if c.loaded {
		repos := c.repos
		c.mu.Unlock()
		for _, r := range repos {
			if r.Name == token {
				return r.Path, nil
			}
		}
		return "", fmt.Errorf("%w: %s", errServiceNotFound, token)
	}
	c.mu.Unlock()
	return c.wrapped.Resolve(ctx, token)
}

func (c *CachedDiscoverer) FindAll(ctx context.Context) ([]domain.Repo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.loaded {
		return cloneRepos(c.repos), nil
	}

	return c.scanAndStoreLocked(ctx)
}

func (c *CachedDiscoverer) Refresh(ctx context.Context) ([]domain.Repo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.scanAndStoreLocked(ctx)
}

func (c *CachedDiscoverer) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loaded = false
	c.repos = nil
}

func (c *CachedDiscoverer) scanAndStoreLocked(ctx context.Context) ([]domain.Repo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	repos, err := c.wrapped.FindAll(ctx)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.loaded = true
	c.repos = cloneRepos(repos)
	return cloneRepos(repos), nil
}

func cloneRepos(repos []domain.Repo) []domain.Repo {
	return append([]domain.Repo(nil), repos...)
}
