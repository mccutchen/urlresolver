//nolint:errcheck
package urlresolver

import (
	"io/ioutil"
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
				"X-1": "in request",
				"X-2": "in request",
			}),
		},
		"existing headers take precedence": {
			requestHeaders: map[string]string{
				"User-Agent": "in request",
				"X-1":        "in request",
				"X-2":        "in request",
			},
			wantHeaders: addHeaders(t, fakeBrowserHeaders, map[string]string{
				"User-Agent": "in request",
				"X-1":        "in request",
				"X-2":        "in request",
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

func TestDecodingBodyReader(t *testing.T) {
	t.Parallel()

	t.Run("invalid gzip stream", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Encoding", "gzip")
			w.Write([]byte("definitely not gzip"))
		}))
		defer srv.Close()

		client := &http.Client{
			Transport: &fakeBrowserTransport{http.DefaultTransport},
		}
		resp, err := client.Get(srv.URL)
		assert.Nil(t, resp)
		assert.Error(t, err)
	})

	t.Run("invalid flate stream", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Encoding", "deflate")
			w.Write([]byte("definitely not flate"))
		}))
		defer srv.Close()

		client := &http.Client{
			Transport: &fakeBrowserTransport{http.DefaultTransport},
		}
		resp, err := client.Get(srv.URL)
		if !assert.NoError(t, err) {
			return
		}

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		body, err := ioutil.ReadAll(resp.Body)
		assert.Error(t, err)
		assert.Len(t, body, 0)

		err = resp.Body.Close()
		assert.Error(t, err)
	})
}
