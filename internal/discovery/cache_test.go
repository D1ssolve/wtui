package discovery

import (
	"context"
	"errors"
	"testing"

	"github.com/diss0x/wtui/internal/domain"
)

type fakeResolver struct {
	repos        []domain.Repo
	findAllCalls int
	resolveCalls int
	resolvePath  string
	findAllErr   error
	findAllHook  func()
}

func (f *fakeResolver) Resolve(_ context.Context, _ string) (string, error) {
	f.resolveCalls++
	return f.resolvePath, nil
}

func (f *fakeResolver) FindAll(_ context.Context) ([]domain.Repo, error) {
	f.findAllCalls++
	if f.findAllHook != nil {
		f.findAllHook()
	}
	if f.findAllErr != nil {
		return nil, f.findAllErr
	}
	return append([]domain.Repo(nil), f.repos...), nil
}

func TestCachedDiscovererFindAllScansOnceThenUsesMemory(t *testing.T) {
	repos := []domain.Repo{{Name: "api", Path: "/repo/api"}}
	wrapped := &fakeResolver{repos: repos}
	cached := NewCached(wrapped)

	got, err := cached.FindAll(context.Background())
	if err != nil {
		t.Fatalf("FindAll first call: %v", err)
	}
	if wrapped.findAllCalls != 1 {
		t.Fatalf("first call should scan once, got %d", wrapped.findAllCalls)
	}
	if len(got) != 1 || got[0] != repos[0] {
		t.Fatalf("first call repos = %#v, want %#v", got, repos)
	}

	wrapped.repos = []domain.Repo{{Name: "changed", Path: "/repo/changed"}}
	got, err = cached.FindAll(context.Background())
	if err != nil {
		t.Fatalf("FindAll second call: %v", err)
	}
	if wrapped.findAllCalls != 1 {
		t.Fatalf("second call should use memory cache, scans = %d", wrapped.findAllCalls)
	}
	if len(got) != 1 || got[0] != repos[0] {
		t.Fatalf("memory repos = %#v, want %#v", got, repos)
	}
}

func TestCachedDiscovererFindAllReturnsCanceledErrorOnMemoryHit(t *testing.T) {
	repos := []domain.Repo{{Name: "api", Path: "/repo/api"}}
	wrapped := &fakeResolver{repos: repos}
	cached := NewCached(wrapped)
	if _, err := cached.FindAll(context.Background()); err != nil {
		t.Fatalf("seed memory cache: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := cached.FindAll(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("FindAll canceled memory hit error = %v, want %v", err, context.Canceled)
	}
	if got != nil {
		t.Fatalf("FindAll canceled memory hit repos = %#v, want nil", got)
	}
	if wrapped.findAllCalls != 1 {
		t.Fatalf("canceled memory hit should not scan, scans = %d", wrapped.findAllCalls)
	}

	got, err = cached.FindAll(context.Background())
	if err != nil {
		t.Fatalf("FindAll after canceled memory hit: %v", err)
	}
	if len(got) != 1 || got[0] != repos[0] {
		t.Fatalf("memory repos after cancellation = %#v, want %#v", got, repos)
	}
}

func TestCachedDiscovererFindAllReturnsCanceledErrorBeforeScan(t *testing.T) {
	wrapped := &fakeResolver{findAllErr: errors.New("scan should not run")}
	cached := NewCached(wrapped)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := cached.FindAll(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("FindAll canceled before scan error = %v, want %v", err, context.Canceled)
	}
	if got != nil {
		t.Fatalf("FindAll canceled before scan repos = %#v, want nil", got)
	}
	if wrapped.findAllCalls != 0 {
		t.Fatalf("canceled before scan should not call wrapped, calls = %d", wrapped.findAllCalls)
	}

	repos := []domain.Repo{{Name: "api", Path: "/repo/api"}}
	wrapped.repos = repos
	wrapped.findAllErr = nil
	got, err = cached.FindAll(context.Background())
	if err != nil {
		t.Fatalf("FindAll after canceled: %v", err)
	}
	if len(got) != 1 || got[0] != repos[0] {
		t.Fatalf("repos after cancellation = %#v, want %#v", got, repos)
	}
}

func TestCachedDiscovererDoesNotMutateMemoryCacheWhenCanceledAfterScan(t *testing.T) {
	seedRepos := []domain.Repo{{Name: "old", Path: "/repo/old"}}
	wrapped := &fakeResolver{repos: seedRepos}
	cached := NewCached(wrapped)
	if _, err := cached.FindAll(context.Background()); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	wrapped.repos = []domain.Repo{{Name: "new", Path: "/repo/new"}}
	wrapped.findAllHook = cancel
	got, err := cached.Refresh(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Refresh canceled after scan error = %v, want %v", err, context.Canceled)
	}
	if got != nil {
		t.Fatalf("Refresh canceled after scan repos = %#v, want nil", got)
	}

	wrapped.findAllHook = nil
	wrapped.findAllErr = errors.New("scan should not run")
	got, err = cached.FindAll(context.Background())
	if err != nil {
		t.Fatalf("FindAll after canceled refresh: %v", err)
	}
	if len(got) != 1 || got[0] != seedRepos[0] {
		t.Fatalf("memory repos after canceled refresh = %#v, want %#v", got, seedRepos)
	}
}

func TestCachedDiscovererRefreshBypassesMemoryAndRescans(t *testing.T) {
	wrapped := &fakeResolver{repos: []domain.Repo{{Name: "first", Path: "/repo/first"}}}
	cached := NewCached(wrapped)
	if _, err := cached.FindAll(context.Background()); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	wrapped.repos = []domain.Repo{{Name: "fresh", Path: "/repo/fresh"}}
	got, err := cached.Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if wrapped.findAllCalls != 2 {
		t.Fatalf("Refresh should scan again, scans = %d", wrapped.findAllCalls)
	}
	if len(got) != 1 || got[0].Name != "fresh" {
		t.Fatalf("refresh repos = %#v", got)
	}

	wrapped.findAllErr = errors.New("scan should not run")
	got, err = cached.FindAll(context.Background())
	if err != nil {
		t.Fatalf("FindAll after refresh: %v", err)
	}
	if len(got) != 1 || got[0].Name != "fresh" {
		t.Fatalf("memory repos after refresh = %#v", got)
	}
}

func TestCachedDiscovererInvalidateClearsMemory(t *testing.T) {
	wrapped := &fakeResolver{repos: []domain.Repo{{Name: "first", Path: "/repo/first"}}}
	cached := NewCached(wrapped)
	if _, err := cached.FindAll(context.Background()); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	cached.Invalidate()

	wrapped.repos = []domain.Repo{{Name: "second", Path: "/repo/second"}}
	got, err := cached.FindAll(context.Background())
	if err != nil {
		t.Fatalf("FindAll after invalidate: %v", err)
	}
	if len(got) != 1 || got[0].Name != "second" {
		t.Fatalf("repos after invalidate = %#v", got)
	}
	if wrapped.findAllCalls != 2 {
		t.Fatalf("expected 2 scans (seed + post-invalidate), got %d", wrapped.findAllCalls)
	}

	cached.Invalidate()
	if wrapped.findAllCalls != 2 {
		t.Fatalf("second Invalidate should not trigger scan, got %d calls", wrapped.findAllCalls)
	}
}

func TestCachedDiscovererResolveDelegatesToWrappedResolver(t *testing.T) {
	wrapped := &fakeResolver{resolvePath: "/repo/api"}
	cached := NewCached(wrapped)
	got, err := cached.Resolve(context.Background(), "api")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "/repo/api" || wrapped.resolveCalls != 1 {
		t.Fatalf("Resolve delegated path=%q calls=%d", got, wrapped.resolveCalls)
	}
}

func TestCachedDiscovererResolveUsesFindAllCache(t *testing.T) {
	repos := []domain.Repo{
		{Name: "api", Path: "/repo/api"},
		{Name: "web", Path: "/repo/web"},
	}
	wrapped := &fakeResolver{repos: repos, resolvePath: "/repo/fallback"}
	cached := NewCached(wrapped)

	if _, err := cached.FindAll(context.Background()); err != nil {
		t.Fatalf("FindAll: %v", err)
	}

	got, err := cached.Resolve(context.Background(), "api")
	if err != nil {
		t.Fatalf("Resolve from cache: %v", err)
	}
	if got != "/repo/api" {
		t.Fatalf("Resolve from cache got %q, want %q", got, "/repo/api")
	}
	if wrapped.resolveCalls != 0 {
		t.Fatalf("Resolve should not call wrapped when cache is loaded, calls=%d", wrapped.resolveCalls)
	}

	_, err = cached.Resolve(context.Background(), "missing")
	if err == nil {
		t.Fatal("Resolve for missing token should return error")
	}
}

func TestCachedDiscovererResolveFallsBackWhenCacheNotLoaded(t *testing.T) {
	wrapped := &fakeResolver{resolvePath: "/repo/api"}
	cached := NewCached(wrapped)

	got, err := cached.Resolve(context.Background(), "api")
	if err != nil {
		t.Fatalf("Resolve fallback: %v", err)
	}
	if got != "/repo/api" {
		t.Fatalf("Resolve fallback got %q, want %q", got, "/repo/api")
	}
	if wrapped.resolveCalls != 1 {
		t.Fatalf("Resolve fallback should call wrapped once, calls=%d", wrapped.resolveCalls)
	}
}
