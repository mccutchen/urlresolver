package httphandler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mccutchen/urlresolver/urlresolver"
)

func TestRouting(t *testing.T) {
	handler := New(urlresolver.New(http.DefaultTransport))
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
		"get index ok": {
			method:   "GET",
			path:     "/",
			wantCode: http.StatusOK,
			wantBody: "Hello, world",
		},
		"path not found": {
			method:   "GET",
			path:     "/foo",
			wantCode: http.StatusNotFound,
		},
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
		timeout       time.Duration
		wantCode      int
		wantResult    urlresolver.Result
	}{
		"ok": {
			remoteHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(`<title>title</title>`))
			},
			remotePath: "/",
			wantCode:   http.StatusOK,
			wantResult: urlresolver.Result{
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
			remotePath: "/",
			timeout:    5 * time.Millisecond,
			wantCode:   http.StatusBadGateway,
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
			remotePath: "/redirect",
			timeout:    25 * time.Millisecond,
			wantCode:   http.StatusOK,
			wantResult: urlresolver.Result{
				Title:       "",
				ResolvedURL: "/resolved",
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			handler := New(urlresolver.New(http.DefaultTransport))

			remoteSrv := httptest.NewServer(http.HandlerFunc(tc.remoteHandler))
			defer remoteSrv.Close()

			timeout := tc.timeout
			if timeout == 0 {
				timeout = 100 * time.Millisecond
			}

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			r := newLookupRequest(t, ctx, remoteSrv, tc.remotePath)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)

			if w.Code != tc.wantCode {
				t.Fatalf("expected code %d, got %d: %s", tc.wantCode, w.Code, w.Body.String())
			}

			if tc.wantCode != http.StatusOK {
				return
			}

			var result urlresolver.Result
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Errorf("failed to unmarshal body: %s: %s", err, w.Body.String())
			}

			// fix up expected URL
			tc.wantResult.ResolvedURL = renderURL(remoteSrv.URL, tc.wantResult.ResolvedURL)

			if !reflect.DeepEqual(result, tc.wantResult) {
				t.Errorf("expected result %#v, got %#v", tc.wantResult, result)
			}
		})
	}
}

func newLookupRequest(t *testing.T, ctx context.Context, remoteSrv *httptest.Server, remotePath string) *http.Request {
	t.Helper()

	params := url.Values{}
	params.Add("url", remoteSrv.URL+remotePath)
	u := "/lookup?" + params.Encode()

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
