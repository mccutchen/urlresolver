package resolver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHeaderInjectingTransport(t *testing.T) {
	testCases := map[string]struct {
		injectHeaders  map[string]string
		requestHeaders map[string]string
		wantHeaders    map[string]string
	}{
		"headers are injected": {
			injectHeaders: map[string]string{
				"Accept-Encoding": "injected",
				"User-Agent":      "injected",
				"X-1":             "injected",
			},
			requestHeaders: map[string]string{
				"X-2": "in request",
				"X-3": "in request",
			},
			wantHeaders: map[string]string{
				"Accept-Encoding": "injected",
				"User-Agent":      "injected",
				"X-1":             "injected",
				"X-2":             "in request",
				"X-3":             "in request",
			},
		},
		"injected headers take precedence": {
			injectHeaders: map[string]string{
				"Accept-Encoding": "injected",
				"User-Agent":      "injected",
				"X-1":             "injected",
			},
			requestHeaders: map[string]string{
				"User-Agent": "in request",
				"X-1":        "in request",
				"X-2":        "in request",
			},
			wantHeaders: map[string]string{
				"Accept-Encoding": "injected",
				"User-Agent":      "injected",
				"X-1":             "injected",
				"X-2":             "in request",
			},
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotHeaders := make(map[string]string, len(r.Header))
				for k := range r.Header {
					gotHeaders[k] = r.Header.Get(k)
				}
				assert.Equal(t, tc.wantHeaders, gotHeaders)
			}))
			defer srv.Close()

			req, err := http.NewRequest("GET", srv.URL, nil)
			assert.NoError(t, err)
			for k, v := range tc.requestHeaders {
				req.Header.Set(k, v)
			}

			client := &http.Client{
				Transport: &headerInjectingTransport{
					injectHeaders: tc.injectHeaders,
					transport:     http.DefaultTransport,
				},
			}
			resp, err := client.Do(req)
			assert.NoError(t, err)
			assert.Equal(t, resp.StatusCode, http.StatusOK)
		})
	}
}
