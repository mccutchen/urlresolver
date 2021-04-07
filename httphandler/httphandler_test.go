package httphandler

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mccutchen/urlresolver/resolver"
	"github.com/mccutchen/urlresolver/safedialer"
	"github.com/mccutchen/urlresolver/tracetransport"
	"github.com/stretchr/testify/assert"
)

func TestRouting(t *testing.T) {
	handler := New(resolver.New(http.DefaultTransport, 0))
	remoteSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK\n"))
	}))
	defer remoteSrv.Close()

	type testCase struct {
		method   string
		path     string
		wantCode int
		wantBody string
	}
	testCases := map[string]testCase{
		"lookup valid url ok": {
			method:   "GET",
			path:     "/lookup?url=" + remoteSrv.URL,
			wantCode: http.StatusOK,
			wantBody: remoteSrv.URL,
		},
		"lookup arg required": {
			method:   "GET",
			path:     "/lookup?foo",
			wantCode: http.StatusBadRequest,
			wantBody: "Missing arg url",
		},
		"lookup arg must be valid URL": {
			method:   "GET",
			path:     "/lookup?url=" + url.QueryEscape("%%"),
			wantCode: http.StatusBadRequest,
			wantBody: "Invalid url",
		},
		"lookup arg must be absolute URL": {
			method:   "GET",
			path:     `/lookup?url=path/to/foo`,
			wantCode: http.StatusBadRequest,
			wantBody: "Invalid url",
		},
		"lookup arg must have hostname": {
			method:   "GET",
			path:     `/lookup?url=https:///path/to/foo`,
			wantCode: http.StatusBadRequest,
			wantBody: "Invalid url",
		},
	}

	// add negative test cases for disallowed methods
	for _, method := range []string{"POST", "PUT", "DELETE", "OPTIONS"} {
		for _, path := range []string{"/", "/lookup"} {
			name := fmt.Sprintf("%s %s not allowed", method, path)
			testCases[name] = testCase{
				method:   method,
				path:     path,
				wantCode: http.StatusMethodNotAllowed,
			}
		}
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Logf("method=%q path=%q", tc.method, tc.path)

			r, err := http.NewRequestWithContext(context.Background(), tc.method, tc.path, nil)
			if err != nil {
				t.Fatal(err)
			}

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
	testCases := map[string]struct {
		remoteHandler func(http.ResponseWriter, *http.Request)
		remotePath    string
		transport     http.RoundTripper

		upstreamReqTimeout   time.Duration
		testClientReqTimeout time.Duration

		wantCode   int
		wantResult resolveResponse
		wantErr    error
	}{
		"ok": {
			remoteHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(`<title>title</title>`))
			},
			remotePath: "/",
			wantCode:   http.StatusOK,
			wantResult: resolveResponse{
				Title:       "title",
				ResolvedURL: "/",
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
			wantResult: resolveResponse{
				Title:       "",
				ResolvedURL: "/foo",
				Error:       ErrRequestTimeout.Error(),
			},
			wantCode: http.StatusNonAuthoritativeInfo,
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
			wantResult: resolveResponse{
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
			wantResult: resolveResponse{
				Title:       "",
				ResolvedURL: "/foo",
				Error:       ErrResolveError.Error(),
			},
		},
		"request to unsafe upstream fails": {
			remoteHandler: func(w http.ResponseWriter, r *http.Request) {},
			remotePath:    "/foo?utm_param=bar",
			transport: &http.Transport{
				DialContext: safedialer.New(net.Dialer{}).DialContext,
			},
			wantCode: http.StatusNonAuthoritativeInfo,
			wantResult: resolveResponse{
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
		t.Run(name, func(t *testing.T) {
			transport := tc.transport
			if transport == nil {
				transport = http.DefaultTransport
			}
			transport = tracetransport.New(transport)

			handler := New(resolver.New(transport, tc.upstreamReqTimeout))
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

			body, err := ioutil.ReadAll(resp.Body)
			assert.NoError(t, err)

			var result resolveResponse
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
