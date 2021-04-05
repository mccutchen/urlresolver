package resolver

import (
	"context"

	"github.com/honeycombio/beeline-go"
	"golang.org/x/sync/singleflight"
)

// SingleFlightResolver is a Resolver implementation that ensures concurrent
// requests to resolve the same URL result in a single request to the origin
// server.
type SingleFlightResolver struct {
	group    *singleflight.Group
	resolver Resolver
}

// NewSingleFlightResolver creates a new SingleFlightResolver.
func NewSingleFlightResolver(resolver Resolver) *SingleFlightResolver {
	return &SingleFlightResolver{
		group:    &singleflight.Group{},
		resolver: resolver,
	}
}

// Resolve resolves a URL, ensuring that concurrent requests result in a single
// request to the origin server.
func (r *SingleFlightResolver) Resolve(ctx context.Context, url string) (Result, error) {
	v, err, coalesced := r.group.Do(url, func() (interface{}, error) {
		return r.resolver.Resolve(ctx, url)
	})

	beeline.AddField(ctx, "resolver.request_coalesced", coalesced)
	if err != nil {
		beeline.AddField(ctx, "error", err.Error())
	}

	return v.(Result), err
}
