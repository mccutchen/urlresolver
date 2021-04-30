//nolint:errcheck
package httphandler

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mccutchen/urlresolver"
	"github.com/mccutchen/urlresolver/safedialer"
	"github.com/mccutchen/urlresolver/tracetransport"
)

func TestRouting(t *testing.T) {
	t.Parallel()

	type testCase struct {
		method   string
		url      string
		wantCode int
		wantBody string
	}
	testCases := map[string]testCase{
		"lookup valid url ok": {
			method:   "GET",
			url:      "/lookup?url={{remoteSrv}}",
			wantCode: http.StatusOK,
			wantBody: "{{remoteSrv}}",
		},
		"lookup allows HEAD requests": {
			method:   "HEAD", // uptime monitoring often uses HEAD requests
			url:      "/lookup?url={{remoteSrv}}",
			wantCode: http.StatusOK,
			wantBody: "{{remoteSrv}}",
		},
		"lookup arg required": {
			method:   "GET",
			url:      "/lookup?foo",
			wantCode: http.StatusBadRequest,
			wantBody: "Missing arg url",
		},
		"lookup arg must be valid URL": {
			method:   "GET",
			url:      "/lookup?url=" + url.QueryEscape("%%"),
			wantCode: http.StatusBadRequest,
			wantBody: "Invalid url",
		},
		"lookup arg must be absolute URL": {
			method:   "GET",
			url:      `/lookup?url=path/to/foo`,
			wantCode: http.StatusBadRequest,
			wantBody: "Invalid url",
		},
		"lookup arg must have hostname": {
			method:   "GET",
			url:      `/lookup?url=https:///path/to/foo`,
			wantCode: http.StatusBadRequest,
			wantBody: "Invalid url",
		},
	}

	for name, tc := range testCases {
		tc := tc

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			handler := New(urlresolver.New(http.DefaultTransport, 0))
			remoteSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("OK\n"))
			}))
			defer remoteSrv.Close()

			tc.url = strings.ReplaceAll(tc.url, "{{remoteSrv}}", remoteSrv.URL)
			tc.wantBody = strings.ReplaceAll(tc.wantBody, "{{remoteSrv}}", remoteSrv.URL)

			r, err := http.NewRequestWithContext(context.Background(), tc.method, tc.url, nil)
			if err != nil {
				t.Fatal(err)
			}

			resp, err := http.Get(remoteSrv.URL)
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, 200, resp.StatusCode)

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)

			if w.Code != tc.wantCode {
				t.Errorf("expected code %d, got %d", tc.wantCode, w.Code)
			}
			if !strings.Contains(w.Body.String(), tc.wantBody) {
				t.Errorf("expected %q in body %q", tc.wantBody, w.Body.String())
			}
		})
	}
}

func TestLookup(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		remoteHandler func(http.ResponseWriter, *http.Request)
		remotePath    string
		transport     http.RoundTripper

		upstreamReqTimeout   time.Duration
		testClientReqTimeout time.Duration

		wantCode    int
		wantResult  ResolveResponse
		wantErr     error
		wantHeaders map[string]string
	}{
		"ok": {
			remoteHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(`<title>title</title>`))
			},
			remotePath: "/",
			wantCode:   http.StatusOK,
			wantResult: ResolveResponse{
				Title:       "title",
				ResolvedURL: "/",
			},
			wantHeaders: map[string]string{
				"Cache-Control": "public,max-age=31536000", // on success, cache for a long time
				"Content-Type":  "application/json",
			},
		},
		"timeout resolving URL": {
			remoteHandler: func(w http.ResponseWriter, r *http.Request) {
				select {
				// sleep longer than test timeout below, to ensure the resolve
				// request times out
				case <-time.After(100 * time.Millisecond):
				// but don't waste time sleeping after the request has been
				// canceled as expected
				case <-r.Context().Done():
				}
			},
			remotePath:         "/foo",
			upstreamReqTimeout: 5 * time.Millisecond,
			wantResult: ResolveResponse{
				Title:       "",
				ResolvedURL: "/foo",
				Error:       ErrRequestTimeout.Error(),
			},
			wantCode: http.StatusNonAuthoritativeInfo,
			wantHeaders: map[string]string{
				"Cache-Control": "public,max-age=300", // on error, cache for short ttl
				"Content-Type":  "application/json",
			},
		},
		"url gets resolved but title cannot be found": {
			remoteHandler: func(w http.ResponseWriter, r *http.Request) {
				// First request redirects immediately
				if r.URL.Path == "/redirect" {
					http.Redirect(w, r, "/resolved", http.StatusFound)
					return
				}

				// Second request immediately writes OK response, but sleeps
				// for too long before writing body, so the resolver will fail
				// to read a title but still return an OK partial response.
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()

				select {
				// sleep longer than test timeout below, to ensure the body
				// read while searching for title times out
				case <-time.After(100 * time.Millisecond):
					w.Write([]byte("<title>title</title>"))
				// but don't waste time sleeping after the request has been
				// canceled as expected
				case <-r.Context().Done():
				}
			},
			remotePath:         "/redirect",
			upstreamReqTimeout: 50 * time.Millisecond,
			wantCode:           http.StatusNonAuthoritativeInfo,
			wantResult: ResolveResponse{
				Title:       "",
				ResolvedURL: "/resolved",
				Error:       ErrRequestTimeout.Error(),
			},
		},
		"non-timeout error resolving url": {
			remoteHandler: func(w http.ResponseWriter, r *http.Request) {
				panic("abort request")
			},
			remotePath: "/foo?utm_param=bar",
			wantCode:   http.StatusNonAuthoritativeInfo,
			wantResult: ResolveResponse{
				Title:       "",
				ResolvedURL: "/foo",
				Error:       ErrResolveError.Error(),
			},
		},
		"request to unsafe upstream fails": {
			remoteHandler: func(w http.ResponseWriter, r *http.Request) {},
			remotePath:    "/foo?utm_param=bar",
			transport: &http.Transport{
				DialContext: (&net.Dialer{Control: safedialer.Control}).DialContext,
			},
			wantCode: http.StatusNonAuthoritativeInfo,
			wantResult: ResolveResponse{
				Title:       "",
				ResolvedURL: "/foo",
				Error:       ErrResolveError.Error(),
			},
		},

		// Note: This test exists to exercise the code path that handles
		// clients closing the request (look for "499" in httphandler.go), but
		// there's not a good way to directly test the actual result of that
		// code (since our test client will never see the response, because it
		// has already closed the request).
		"client closes connection before url is resolved": {
			remoteHandler: func(w http.ResponseWriter, r *http.Request) {
				select {
				// sleep longer than test timeout below, to ensure the resolve
				// request times out
				case <-time.After(100 * time.Millisecond):
				// but don't waste time sleeping after the request has been
				// canceled as expected
				case <-r.Context().Done():
				}
			},
			remotePath:           "/foo",
			testClientReqTimeout: 5 * time.Millisecond,

			// Here we get context.DeadlineExceeded, because that's what happened
			// from this test case's POV.  The srv running here will actually see
			// the client closing the request, but we don't have a good way to
			// directly test its behavior in that case, because our test client
			// here will never see the response.
			wantErr: context.DeadlineExceeded,
		},
	}

	for name, tc := range testCases {
		tc := tc

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			transport := tc.transport
			if transport == nil {
				transport = http.DefaultTransport
			}
			transport = tracetransport.New(transport)

			handler := New(urlresolver.New(transport, tc.upstreamReqTimeout))
			resolverSrv := httptest.NewServer(handler)
			defer resolverSrv.Close()

			remoteSrv := httptest.NewServer(http.HandlerFunc(tc.remoteHandler))
			defer remoteSrv.Close()

			timeout := tc.testClientReqTimeout
			if timeout == 0 {
				timeout = 100 * time.Millisecond
			}

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			req := newLookupRequest(ctx, t, resolverSrv, remoteSrv, tc.remotePath)
			resp, err := http.DefaultClient.Do(req)
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
				return
			}
			if !assert.NoError(t, err) {
				return
			}
			if !assert.Equal(t, tc.wantCode, resp.StatusCode) {
				return
			}

			for key, wantValue := range tc.wantHeaders {
				assert.Equal(t, wantValue, resp.Header.Get(key), "incorrect value for header key %q", key)
			}

			body, err := ioutil.ReadAll(resp.Body)
			assert.NoError(t, err)

			var result ResolveResponse
			if err := json.Unmarshal(body, &result); err != nil {
				t.Errorf("failed to unmarshal body: %s: %s", err, string(body))
			}

			// fix up expected URL
			tc.wantResult.ResolvedURL = renderURL(remoteSrv.URL, tc.wantResult.ResolvedURL)
			assert.Equal(t, tc.wantResult, result)
		})
	}
}

func newLookupRequest(ctx context.Context, t *testing.T, resolverSrv *httptest.Server, remoteSrv *httptest.Server, remotePath string) *http.Request {
	t.Helper()

	params := url.Values{}
	params.Add("url", remoteSrv.URL+remotePath)
	u := resolverSrv.URL + "/lookup?" + params.Encode()

	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	return req
}

// renderURL takes a dynamic httptest.Server URL string src and an "expected"
// URL dst and ensures that dst is relative to the dynamic server URL.
func renderURL(src string, dst string) string {
	srcURL, _ := url.Parse(src)
	dstURL, _ := url.Parse(dst)
	return srcURL.ResolveReference(dstURL).String()
}
