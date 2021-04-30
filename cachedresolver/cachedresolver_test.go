//nolint:errcheck
package cachedresolver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/cache/v8"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"

	"github.com/mccutchen/urlresolver"
)

func TestCachedResolver(t *testing.T) {
	t.Parallel()

	var counter int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&counter, 1)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>title</title></head></html>`))
	}))
	defer srv.Close()

	redisSrv, err := miniredis.Run()
	assert.NoError(t, err)
	defer redisSrv.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: redisSrv.Addr()})
	redisCache := cache.New(&cache.Options{Redis: redisClient})

	resolver := NewCachedResolver(
		urlresolver.New(http.DefaultTransport, 0),
		NewRedisCache(redisCache, 10*time.Minute),
	)

	wantResult := urlresolver.Result{
		Title:       "title",
		ResolvedURL: srv.URL,
	}

	// Make 5 sequential requests, 4 should be cached
	for i := 0; i < 5; i++ {
		result, err := resolver.Resolve(context.Background(), srv.URL)
		assert.NoError(t, err)
		assert.Equal(t, wantResult, result)
	}
	assert.Equal(t, int64(1), counter, "expected only 1 total request to upstream")
}
