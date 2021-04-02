package resolver

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type CachedResolver struct {
	cache    Cache
	resolver Resolver
}

func NewCachedResolver(resolver Resolver, cache Cache) *CachedResolver {
	return &CachedResolver{
		cache:    cache,
		resolver: resolver,
	}
}

func (c *CachedResolver) Resolve(ctx context.Context, url string) (Result, error) {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("resolver.cache_name", c.cache.Name()))

	if result, ok := c.cache.Get(ctx, url); ok {
		span.SetAttributes(attribute.String("resolver.cache_result", "hit"))
		return result, nil
	}

	result, err := c.resolver.Resolve(ctx, url)
	if err == nil {
		c.cache.Add(ctx, url, result)
	}

	span.SetAttributes(attribute.String("resolver.cache_result", "miss"))
	return result, err
}
