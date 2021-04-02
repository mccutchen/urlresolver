package urlresolver

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/singleflight"
)

type SingleflightResolver struct {
	group    *singleflight.Group
	resolver Resolver
}

func NewSingleflightResolver(resolver Resolver) *SingleflightResolver {
	return &SingleflightResolver{
		group:    &singleflight.Group{},
		resolver: resolver,
	}
}

func (r *SingleflightResolver) Resolve(ctx context.Context, url string) (Result, error) {
	span := trace.SpanFromContext(ctx)

	v, err, coalesced := r.group.Do(url, func() (interface{}, error) {
		return r.resolver.Resolve(ctx, url)
	})

	span.SetAttributes(attribute.Bool("urlresolver.request_coalesced", coalesced))
	if err != nil {
		span.SetAttributes(attribute.String("error", err.Error()))
	}

	return v.(Result), err
}
