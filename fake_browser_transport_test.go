//nolint:errcheck
package urlresolver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func addHeaders(t *testing.T, a, b map[string]string) map[string]string {
	result := make(map[string]string)
	for k, v := range a {
		result[k] = v
	}
	for k, v := range b {
		result[k] = v
	}
	return result
}

func TestFakeBrowserTransport(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		injectHeaders  map[string]string
		requestHeaders map[string]string
		wantHeaders    map[string]string
	}{
		"headers are injected": {
			requestHeaders: map[string]string{
				"X-1": "in request",
				"X-2": "in request",
			},
			wantHeaders: addHeaders(t, fakeBrowserHeaders, map[string]string{
				"Accept-Encoding": "gzip", // added by stdlib http client
				"X-1":             "in request",
				"X-2":             "in request",
			}),
		},
		"existing headers take precedence": {
			requestHeaders: map[string]string{
				"User-Agent": "in request",
				"X-1":        "in request",
				"X-2":        "in request",
			},
			wantHeaders: addHeaders(t, fakeBrowserHeaders, map[string]string{
				"Accept-Encoding": "gzip", // added by stdlib http client
				"User-Agent":      "in request",
				"X-1":             "in request",
				"X-2":             "in request",
			}),
		},
	}
	for name, tc := range testCases {
		tc := tc

		t.Run(name, func(t *testing.T) {
			t.Parallel()

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
				Transport: &fakeBrowserTransport{
					transport: http.DefaultTransport,
				},
			}
			resp, err := client.Do(req)
			assert.NoError(t, err)
			assert.Equal(t, resp.StatusCode, http.StatusOK)
		})
	}
}
