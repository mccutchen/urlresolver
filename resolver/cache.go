package resolver

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/go-redis/cache/v8"
)

const redisCacheVersion = "1"

// Cache is a generic cache interface.
type Cache interface {
	Add(ctx context.Context, key string, value Result)
	Get(ctx context.Context, key string) (value Result, ok bool)
	Name() string
}

// RedisCache caches results in redis.
type RedisCache struct {
	cache *cache.Cache
	ttl   time.Duration
}

// NewRedisCache creates a new RedisCache whose entries will expire after the
// given TTL.
func NewRedisCache(cache *cache.Cache, ttl time.Duration) *RedisCache {
	return &RedisCache{
		cache: cache,
		ttl:   ttl,
	}
}

// Add adds a Result to the cache.
func (c *RedisCache) Add(ctx context.Context, key string, value Result) {
	c.cache.Set(&cache.Item{
		Ctx:   ctx,
		Key:   redisCacheKey(key),
		Value: value,
		TTL:   c.ttl,
	})
}

// Get gets a Result from the cache, returning a bool indicating whether it was
// present.
func (c *RedisCache) Get(ctx context.Context, key string) (Result, bool) {
	var result Result
	if err := c.cache.Get(ctx, redisCacheKey(key), &result); err != nil {
		return Result{}, false
	}
	return result, true
}

// Name returns the name of the cache, for instrumentation purposes.
func (c *RedisCache) Name() string {
	return "redis"
}

func redisCacheKey(key string) string {
	return fmt.Sprintf("cache:%s:%x", redisCacheVersion, sha256.Sum256([]byte(key)))
}
