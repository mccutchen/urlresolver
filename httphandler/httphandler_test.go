package httphandler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

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
