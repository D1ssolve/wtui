package discovery

import (
	"context"
	"sync"

	"github.com/diss0x/wtui/internal/domain"
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
