package resolver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSingleflightResolver(t *testing.T) {
	var counter int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&counter, 1)
		<-time.After(250 * time.Millisecond)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>title</title></head></html>`))
	}))
	defer srv.Close()

	wantResult := Result{
		Title:       "title",
		ResolvedURL: srv.URL,
	}

	resolver := NewSingleFlightResolver(New(http.DefaultTransport, 0))

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := resolver.Resolve(context.Background(), srv.URL)
			assert.NoError(t, err)
			assert.Equal(t, wantResult, result)
		}()
	}
	wg.Wait()

	assert.Equal(t, int64(1), counter, "expected all requests coalesced into 1")
}
