//nolint:errcheck
package fakebrowser

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func mergeMaps(maps ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, m := range maps {
		for k, v := range m {
			result[k] = v
		}
	}
	return result
}

func TestHeaderInjection(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		transport      *Transport
		requestHeaders map[string]string
		wantHeaders    map[string]string
	}{
		"headers are injected": {
			transport: New(http.DefaultTransport),
			requestHeaders: map[string]string{
				"X-1": "in request",
				"X-2": "in request",
			},
			wantHeaders: mergeMaps(DefaultHeaders, map[string]string{
				"Accept-Encoding": "gzip", // added by stdlib http client
				"X-1":             "in request",
				"X-2":             "in request",
			}),
		},
		"existing headers override injected headers": {
			transport: New(http.DefaultTransport),
			requestHeaders: map[string]string{
				"User-Agent": "in request",
				"X-1":        "in request",
				"X-2":        "in request",
			},
			wantHeaders: mergeMaps(DefaultHeaders, map[string]string{
				"Accept-Encoding": "gzip",       // added by stdlib http client
				"User-Agent":      "in request", // will override value in DefaultHeaders
				"X-1":             "in request",
				"X-2":             "in request",
			}),
		},
		"injected headers can be customized": {
			transport: New(http.DefaultTransport, WithHeaders(map[string]string{
				"User-Agent": "custom-user-agent",
			})),
			requestHeaders: map[string]string{
				"X-1": "in request",
				"X-2": "in request",
			},
			wantHeaders: mergeMaps(map[string]string{
				"Accept-Encoding": "gzip", // added by stdlib http client
				"User-Agent":      "custom-user-agent",
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

			client := &http.Client{Transport: tc.transport}
			resp, err := client.Do(req)
			assert.NoError(t, err)
			assert.Equal(t, resp.StatusCode, http.StatusOK)
		})
	}
}
