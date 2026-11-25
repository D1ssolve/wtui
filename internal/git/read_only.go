package git

import "context"

type noLazyFetchKey struct{}

// WithoutLazyFetch prevents Git reads from fetching objects in partial clones.
func WithoutLazyFetch(ctx context.Context) context.Context {
	return context.WithValue(ctx, noLazyFetchKey{}, struct{}{})
}
