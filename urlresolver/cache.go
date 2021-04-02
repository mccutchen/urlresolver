package urlresolver

import (
	"context"

	lru "github.com/hashicorp/golang-lru"
)

// Cache is a generic cache interface
type Cache interface {
	Add(ctx context.Context, key string, value Result)
	Get(ctx context.Context, key string) (value Result, ok bool)
	Name() string
}

type LRUCache struct {
	cache *lru.ARCCache
}

func NewLRUCache(size int) (*LRUCache, error) {
	cache, err := lru.NewARC(size)
	if err != nil {
		return nil, err
	}
	return &LRUCache{cache: cache}, nil
}

func (c *LRUCache) Add(ctx context.Context, key string, value Result) {
	c.cache.Add(key, value)
}

func (c *LRUCache) Get(ctx context.Context, key string) (value Result, ok bool) {
	if val, ok := c.cache.Get(key); ok {
		return val.(Result), true
	}
	return Result{}, false
}

func (c *LRUCache) Name() string {
	return "LRUCache"
}
