package resolver

import (
	"context"

	"github.com/honeycombio/beeline-go"
)

// CachedResolver is a Resolver implementation that caches its results.
type CachedResolver struct {
	cache    Cache
	resolver Resolver
}

// NewCachedResolver creates a new CachedResolver.
func NewCachedResolver(resolver Resolver, cache Cache) *CachedResolver {
	return &CachedResolver{
		cache:    cache,
		resolver: resolver,
	}
}

// Resolve resolves a URL if it is not already cached.
func (c *CachedResolver) Resolve(ctx context.Context, url string) (Result, error) {
	beeline.AddField(ctx, "resolver.cache_name", c.cache.Name())

	if result, ok := c.cache.Get(ctx, url); ok {
		beeline.AddField(ctx, "resolver.cache_result", "hit")
		return result, nil
	}

	result, err := c.resolver.Resolve(ctx, url)
	if err == nil {
		c.cache.Add(ctx, url, result)
	}

	beeline.AddField(ctx, "resolver.cache_result", "miss")
	return result, err
}
