package urlresolver

import "context"

// Resolver is the interface for resolving a URL
type Resolver interface {
	Resolve(ctx context.Context, url string) (Result, error)
}

// Result is the result of resolving a URL.
type Result struct {
	ResolvedURL string `json:"resolved_url"`
	Title       string `json:"title"`
}
