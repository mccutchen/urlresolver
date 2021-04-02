package resolver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCachedResolver(t *testing.T) {
	var counter int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&counter, 1)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>title</title></head></html>`))
	}))
	defer srv.Close()

	cache, err := NewLRUCache(10)
	assert.NoError(t, err)
	resolver := NewCachedResolver(New(http.DefaultTransport, nil), cache)

	wantResult := Result{
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
