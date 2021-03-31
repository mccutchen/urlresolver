package urlresolver

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
	"golang.org/x/text/encoding/charmap"
)

type titleTestCase struct {
	body          []byte
	expectedTitle string
}

func TestFindTitle(t *testing.T) {
	maxBodySize := 1024 // simulate maxBodySize of 1kb for testing purposes
	testCases := loadTitleTestCases(t, maxBodySize)
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			title := findTitle(tc.body)
			if title != tc.expectedTitle {
				t.Errorf("expected title %q, got %q", tc.expectedTitle, title)
			}
		})
	}
}

func TestResolver(t *testing.T) {
	testCases := []struct {
		name        string
		handlerFunc http.HandlerFunc
		givenURL    string
		wantResult  Result
		wantErr     error
	}{
		{
			name: "basic test",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(`<html><head><title>page title</title></head></html>`))
			},
			givenURL: "/foo",
			wantResult: Result{
				ResolvedURL: "/foo",
				Title:       "page title",
			},
		},
		{
			name: "basic redirect test",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/a" {
					http.Redirect(w, r, "/b", http.StatusFound)
					return
				}
				w.Write([]byte(`<html><head><title>page title</title></head></html>`))
			},
			givenURL: "/a",
			wantResult: Result{
				ResolvedURL: "/b",
				Title:       "page title",
			},
		},
		{
			name: "max redirect test",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				parts := strings.Split(r.URL.Path, "/")
				n, _ := strconv.Atoi(parts[1])
				http.Redirect(w, r, fmt.Sprintf("/%d", n+1), http.StatusFound)
			},
			givenURL: "/0",
			wantResult: Result{
				ResolvedURL: fmt.Sprintf("/%d", maxRedirects-1),
				Title:       "",
			},
		},
		{
			name: "cookies are respected",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				var (
					cookieName  = "foo"
					cookieValue = "bar"
				)

				c, err := r.Cookie(cookieName)
				if err != nil {
					expire := time.Now().Add(10 * time.Minute)
					http.SetCookie(w, &http.Cookie{
						Name:    cookieName,
						Value:   cookieValue,
						Path:    "/",
						Expires: expire,
						MaxAge:  90000,
					})
					http.Redirect(w, r, "/b", http.StatusFound)
					return
				}

				if c.Value != cookieValue {
					t.Errorf("unexpected cookie value: %#v", c.Value)
				}

				w.Write([]byte(`<html><head><title>🍪</title></head></html>`))
			},
			givenURL: "/a",
			wantResult: Result{
				ResolvedURL: "/b",
				Title:       "🍪",
			},
		},
		{
			// https://github.com/mccutchen/thresholderbot/pull/63
			name: "forbes interstitial detection",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/forbes" {
					http.Redirect(w, r, "https://www.forbes.com/forbes/welcome/", http.StatusFound)
					return
				}
				w.Write([]byte(`<html><head><title>forbes, yo</title></head></html>`))
			},
			givenURL: "/forbes",
			wantResult: Result{
				ResolvedURL: "/forbes",
				Title:       "",
			},
		},
		{
			name: "timeout waiting on response",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-time.After(10 * time.Second):
					w.Write([]byte(`<html><head><title>forbes, yo</title></head></html>`))
				case <-r.Context().Done():
					return
				}
			},
			givenURL: "/foo",
			wantErr:  context.DeadlineExceeded,
		},
		{
			name: "timeout reading body still resolves URL",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				// first request redirects
				if r.URL.Path == "/foo" {
					http.Redirect(w, r, "/bar", http.StatusFound)
					return
				}
				// second request should time out before the body can be read
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				select {
				case <-time.After(10 * time.Second):
					w.Write([]byte(`<head><title>forbes, yo</title></head></html>`))
				case <-r.Context().Done():
					return
				}
			},
			givenURL: "/foo",
			wantResult: Result{
				ResolvedURL: "/bar", // note, we still got a usefully resolved URL, despite the expected error
				Title:       "",
			},
			wantErr: context.DeadlineExceeded,
		},
		{
			name: "normal html content type parsed",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.Write([]byte(`<html><head><title>page title</title></head></html>`))
			},
			givenURL: "/foo",
			wantResult: Result{
				ResolvedURL: "/foo",
				Title:       "page title",
			},
		},
		{
			name: "weird html content type parsed",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/html")
				w.Write([]byte(`<html><head><title>page title</title></head></html>`))
			},
			givenURL: "/foo",
			wantResult: Result{
				ResolvedURL: "/foo",
				Title:       "page title",
			},
		},
		{
			name: "html content type w/ encoding parsed",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Write([]byte(`<html><head><title>page title</title></head></html>`))
			},
			givenURL: "/foo",
			wantResult: Result{
				ResolvedURL: "/foo",
				Title:       "page title",
			},
		},
		{
			name: "non-html content types ignored",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`<html><head><title>page title</title></head></html>`))
			},
			givenURL: "/foo",
			wantResult: Result{
				ResolvedURL: "/foo",
				Title:       "",
			},
		},
		{
			name: "non-utf8 charset in content type header",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=iso-8859-1")
				w2 := charmap.ISO8859_1.NewEncoder().Writer(w)
				w2.Write([]byte(`<html><head><title>Iñtërnâtiônàlizætiøn</title></head></html>`))
			},
			givenURL: "/foo",
			wantResult: Result{
				ResolvedURL: "/foo",
				Title:       "Iñtërnâtiônàlizætiøn",
			},
		},
		{
			name: "non-utf8 charset in meta charset tag",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w2 := charmap.ISO8859_1.NewEncoder().Writer(w)
				w2.Write([]byte(`<html><head><meta charset="iso-8859-1"><title>Iñtërnâtiônàlizætiøn</title></head></html>`))
			},
			givenURL: "/foo",
			wantResult: Result{
				ResolvedURL: "/foo",
				Title:       "Iñtërnâtiônàlizætiøn",
			},
		},
		{
			name: "non-utf8 charset in meta http-equiv tag",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w2 := charmap.ISO8859_1.NewEncoder().Writer(w)
				w2.Write([]byte(`<html><head><meta http-equiv="Content-Type" content="text/html; charset=iso-8859-1"><title>Iñtërnâtiônàlizætiøn</title></head></html>`))
			},
			givenURL: "/foo",
			wantResult: Result{
				ResolvedURL: "/foo",
				Title:       "Iñtërnâtiônàlizætiøn",
			},
		},
		{
			name: "non-utf8 charset autodetected",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w2 := charmap.ISO8859_1.NewEncoder().Writer(w)
				w2.Write([]byte(`<html><head><title>Iñtërnâtiônàlizætiøn</title></head></html>`))
			},
			givenURL: "/foo",
			wantResult: Result{
				ResolvedURL: "/foo",
				Title:       "Iñtërnâtiônàlizætiøn",
			},
		},
		{
			name: "gzipped utf-8",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.Header().Set("Content-Encoding", "gzip")
				w2 := gzip.NewWriter(w)
				w2.Write([]byte(`<html><head><title>Iñtërnâtiônàlizætiøn</title></head></html>`))
				w2.Close()
			},
			givenURL: "/foo",
			wantResult: Result{
				ResolvedURL: "/foo",
				Title:       "Iñtërnâtiônàlizætiøn",
			},
		},
		{
			name: "gzipped non-utf-8",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.Header().Set("Content-Encoding", "gzip")
				w2 := gzip.NewWriter(w)
				w3 := charmap.ISO8859_1.NewEncoder().Writer(w2)
				w3.Write([]byte(`<html><head><title>Iñtërnâtiônàlizætiøn</title></head></html>`))
				w2.Close()
			},
			givenURL: "/foo",
			wantResult: Result{
				ResolvedURL: "/foo",
				Title:       "Iñtërnâtiônàlizætiøn",
			},
		},
		{
			name: "deflated",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.Header().Set("Content-Encoding", "deflate")
				w2, _ := flate.NewWriter(w, 9) // If level is in the range [-2, 9] then the error returned will be nil.
				defer w2.Close()
				mustWriteAll(t, w2, `<html><head><title>Iñtërnâtiônàlizætiøn</title></head></html>`)
			},
			givenURL: "/foo",
			wantResult: Result{
				ResolvedURL: "/foo",
				Title:       "Iñtërnâtiônàlizætiøn",
			},
		},
		{
			name: "brotli",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.Header().Set("Content-Encoding", "br")
				w2 := brotli.NewWriter(w)
				defer w2.Close()
				mustWriteAll(t, w2, `<html><head><title>Iñtërnâtiônàlizætiøn</title></head></html>`)
			},
			givenURL: "/foo",
			wantResult: Result{
				ResolvedURL: "/foo",
				Title:       "Iñtërnâtiônàlizætiøn",
			},
		},
		{
			name: "gzipped larger than max body size",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.Header().Set("Content-Encoding", "gzip")
				w2 := gzip.NewWriter(w)
				defer w2.Close()
				body := fmt.Sprintf("<html><head><title>Iñtërnâtiônàlizætiøn</title></head><body>%s</body></html>", strings.Repeat("*", maxBodySize*2))
				mustWriteAll(t, w2, body)
			},
			givenURL: "/foo",
			wantResult: Result{
				ResolvedURL: "/foo",
				Title:       "Iñtërnâtiônàlizætiøn",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handlerFunc)
			defer srv.Close()

			resolver := New(http.DefaultTransport)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()

			givenURL := renderURL(srv.URL, tc.givenURL)
			if tc.wantResult.ResolvedURL != "" {
				tc.wantResult.ResolvedURL = renderURL(srv.URL, tc.wantResult.ResolvedURL)
			}

			result, err := resolver.Resolve(ctx, givenURL)
			if tc.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tc.wantErr)
				}
				if !(err == tc.wantErr || errors.Is(err, tc.wantErr) || tc.wantErr.Error() == err.Error()) {
					t.Fatalf("expected error %q, got %q", tc.wantErr, err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}

			if !reflect.DeepEqual(tc.wantResult, result) {
				t.Errorf("expected result %v, got %v", tc.wantResult, result)
			}
		})
	}

	t.Run("request coalescing", func(t *testing.T) {
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

		resolver := New(http.DefaultTransport)

		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				result, err := resolver.Resolve(context.Background(), srv.URL)
				if err != nil {
					t.Errorf("unexpected error: %s", err)
				}
				if !reflect.DeepEqual(wantResult, result) {
					t.Errorf("expected result %v, got %v", wantResult, result)
				}
			}()
		}
		wg.Wait()

		if counter != 1 {
			t.Fatalf("expected 1 total request, got %d", counter)
		}
	})
}

// renderURL takes a dynamic httptest.Server URL string src and an "expected"
// URL dst and ensures that dst is relative to the dynamic server URL.
func renderURL(src string, dst string) string {
	srcURL, _ := url.Parse(src)
	dstURL, _ := url.Parse(dst)
	return srcURL.ResolveReference(dstURL).String()
}

func mustWriteAll(t *testing.T, dst io.Writer, s string) {
	t.Helper()
	nr, err := dst.Write([]byte(s))
	if nr != len(s) {
		t.Fatalf("expected to write %d bytes, wrote %d", len(s), nr)
	}
	if err != nil {
		t.Fatalf("write error: %s", err)
	}
}

func TestPrepareRequest(t *testing.T) {
	ctx := context.Background()

	t.Run("default headers", func(t *testing.T) {
		req, err := prepareRequest(ctx, "http://example.org")
		if err != nil {
			t.Errorf("unexpected error: %s", err)
		}
		for key, expectedValue := range defaultHeaders {
			if value := req.Header.Get(key); value != expectedValue {
				t.Errorf("expected default header %s=%#v, got %#v", key, expectedValue, value)
			}
		}
	})

	t.Run("invalid url", func(t *testing.T) {
		_, err := prepareRequest(ctx, "http://example.org/foo%E")
		if err == nil {
			t.Error("did not get expected error")
		}
	})
}

func loadTitleTestCases(t *testing.T, maxBodySize int) map[string]titleTestCase {
	t.Helper()

	paths, err := filepath.Glob("./testdata/*.html")
	if err != nil {
		t.Fatalf("could not load test cases: %s", err)
	}

	testCases := make(map[string]titleTestCase, len(paths))
	for _, p := range paths {
		b, err := ioutil.ReadFile(p)
		if err != nil {
			t.Fatalf("error reading file %s: %s", p, err)
		}

		testParts := bytes.Split(b, []byte("\n###\n"))
		if len(testParts) > 2 {
			t.Fatalf("invalid test case in %q, expected at most 2 parts", p)
		}

		body := bytes.TrimSpace(testParts[0])

		var expectedTitle []byte
		if len(testParts) > 1 {
			expectedTitle = bytes.TrimSpace(testParts[1])
		}

		if len(body) > maxBodySize {
			body = body[:maxBodySize]
		}

		testCases[p] = titleTestCase{
			body:          body,
			expectedTitle: string(expectedTitle),
		}
	}
	return testCases
}
