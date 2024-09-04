//nolint:errcheck
package urlresolver

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"golang.org/x/text/encoding/charmap"
)

type titleTestCase struct {
	body          []byte
	expectedTitle string
}

func TestFindTitle(t *testing.T) {
	t.Parallel()

	maxBodySize := 1024 // simulate maxBodySize of 1kb for testing purposes
	testCases := loadTitleTestCases(t, maxBodySize)
	for name, tc := range testCases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			title := findTitle(tc.body)
			if title != tc.expectedTitle {
				t.Errorf("expected title %q, got %q", tc.expectedTitle, title)
			}
		})
	}
}

func TestResolver(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		handlerFunc http.HandlerFunc
		givenURL    string
		timeout     time.Duration
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
				ResolvedURL:      "/b",
				Title:            "page title",
				IntermediateURLs: []string{"/a"},
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
				ResolvedURL:      fmt.Sprintf("/%d", maxRedirects-1),
				Title:            "",
				IntermediateURLs: []string{"/0", "/1", "/2", "/3", "/4"},
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
				ResolvedURL:      "/b",
				Title:            "🍪",
				IntermediateURLs: []string{"/a"},
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
			},
			givenURL: "/forbes",
			wantResult: Result{
				ResolvedURL:      "/forbes",
				Title:            "",
				IntermediateURLs: []string{"/forbes"},
			},
		},
		{
			name: "instagram auth detection",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/instagram" {
					http.Redirect(w, r, "https://www.instagram.com/accounts/login/", http.StatusFound)
					return
				}
			},
			givenURL: "/instagram",
			wantResult: Result{
				ResolvedURL:      "/instagram",
				Title:            "",
				IntermediateURLs: []string{"/instagram"},
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
			timeout:  10 * time.Millisecond,
			wantResult: Result{
				ResolvedURL: "/foo",
			},
			wantErr: context.DeadlineExceeded,
		},
		{
			name: "request error after redirect still resolves URL",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/long-url" {
					http.Redirect(w, r, "/short-url?"+r.URL.RawQuery, http.StatusFound)
					return
				}
				select {
				case <-time.After(10 * time.Second):
					w.Write([]byte(`<html><head><title>title</title></head></html>`))
				case <-r.Context().Done():
					return
				}
			},
			givenURL: "/long-url?zzz=zzz&mmm=mmm&AAA=AAA&utm_campaign=spam",
			timeout:  50 * time.Millisecond,
			wantResult: Result{
				ResolvedURL: "/short-url?AAA=AAA&mmm=mmm&zzz=zzz", // note, we still got a resolved (and canonicalized) URL despite the error
				Title:       "",
				IntermediateURLs: []string{
					// params sorted and utm_campaign dropped because each hop
					// is canonicalized
					"/long-url?AAA=AAA&mmm=mmm&zzz=zzz",
				},
			},
			wantErr: context.DeadlineExceeded,
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
			timeout:  20 * time.Millisecond,
			wantResult: Result{
				ResolvedURL:      "/bar", // note, we still got a usefully resolved URL, despite the expected error
				Title:            "",
				IntermediateURLs: []string{"/foo"},
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
		{
			name: "invalid gzip stream",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.Header().Set("Content-Encoding", "gzip")
				w.Header().Set("Content-Encoding", "gzip")
				mustWriteAll(t, w, "<title>definitely not gzip</title>")
			},
			givenURL: "/foo",
			wantErr:  errors.New("error reading response: gzip: invalid header"),
			wantResult: Result{
				ResolvedURL: "/foo",
				Title:       "",
			},
		},
		{
			name: "no redirects",
			handlerFunc: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				mustWriteAll(t, w, "<title>OK</title>")
			},
			givenURL: "/foo",
			wantResult: Result{
				ResolvedURL: "/foo",
				Title:       "OK",
			},
		},
	}

	for _, tc := range testCases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(tc.handlerFunc)
			defer srv.Close()

			resolver := New(newSafeTestTransport(t), 0)

			timeout := tc.timeout
			if timeout == 0 {
				timeout = 1 * time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			givenURL := renderURL(srv.URL, tc.givenURL)
			if tc.wantResult.ResolvedURL != "" {
				tc.wantResult.ResolvedURL = renderURL(srv.URL, tc.wantResult.ResolvedURL)
			}

			result, err := resolver.Resolve(ctx, givenURL)
			assertErrorsMatch(t, tc.wantErr, err)

			// fixup relative intermediate URLs to include test server
			for idx, hop := range tc.wantResult.IntermediateURLs {
				tc.wantResult.IntermediateURLs[idx] = renderURL(srv.URL, hop)
			}

			assert.Equal(t, tc.wantResult, result)
		})
	}

	t.Run("multiple requests for the same URL are coalesced into one", func(t *testing.T) {
		t.Parallel()

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
			Coalesced:   true,
		}

		resolver := New(newSafeTestTransport(t), 0)

		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				// note: URL query param varies, but it's a param that will be
				// stripped by initial canonicalization before the singleflight
				// check happens, so all requests should be coalesced.
				url := fmt.Sprintf("%s?utm_campaign=%d", srv.URL, i)
				result, err := resolver.Resolve(context.Background(), url)
				assert.NoError(t, err)
				assert.Equal(t, wantResult, result)
			}(i)
		}
		wg.Wait()

		assert.Equal(t, int64(1), counter, "expected all requests coalesced into 1")
	})

	// an invalid URL is the only way to get an error out of Resolve
	t.Run("invalid URL error", func(t *testing.T) {
		t.Parallel()

		resolver := New(newSafeTestTransport(t), 0)
		result, err := resolver.Resolve(context.Background(), "%%")
		assertErrorsMatch(t, errors.New("invalid URL escape"), err)
		assert.Equal(t, Result{ResolvedURL: "%%"}, result)
	})
}

func TestRedirectHops(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			http.Redirect(w, r, "/a", http.StatusPermanentRedirect)
		case "/a":
			http.Redirect(w, r, "/b", http.StatusPermanentRedirect)
		case "/b":
			http.Redirect(w, r, "/c", http.StatusPermanentRedirect)
		case "/c":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<title>Success</title>`))
		}
	}))
	defer srv.Close()

	resolver := New(newSafeTestTransport(t), 0)
	result, err := resolver.Resolve(context.Background(), srv.URL)
	assert.NoError(t, err)
	assert.Equal(t, Result{
		ResolvedURL: renderURL(srv.URL, "/c"),
		Title:       "Success",
		IntermediateURLs: []string{
			renderURL(srv.URL, ""),
			renderURL(srv.URL, "/a"),
			renderURL(srv.URL, "/b"),
		},
	}, result)
}

func TestSailthruHandling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// note that wrapped sailthru links are not canonicalized before they
		// are fetched (so ?utm_campaign=foo comes through here)
		assert.Equal(t, "/wrapped-target", r.URL.Path)
		assert.Equal(t, "utm_campaign=foo", r.URL.RawQuery)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// construct a fake Sailthru tracking URL that wraps a URL pointing to our
	// local test server.
	var (
		targetURL  = srv.URL + "/wrapped-target?utm_campaign=foo"
		encodedURL = base64.RawURLEncoding.EncodeToString([]byte(targetURL))
		givenURL   = fmt.Sprintf("https://link.example.com/click/00000000.0000/%s/0000", encodedURL)
	)

	wantResult := Result{
		ResolvedURL:      srv.URL + "/wrapped-target",
		IntermediateURLs: []string{givenURL},
	}

	resolver := New(newSafeTestTransport(t), 0)
	gotResult, err := resolver.Resolve(context.Background(), givenURL)
	assert.NoError(t, err)
	assert.Equal(t, wantResult, gotResult)
}

// assertErrorsMatch is a helper for comparing two error values, mostly to hide
// the awkwardness of comparing error strings necessitated by the kinds of
// network errors we're dealing with containing random IP addresses.
func assertErrorsMatch(t *testing.T, want, got error) {
	t.Helper()
	if want != nil {
		if assert.Error(t, got) {
			assert.Contains(t, got.Error(), want.Error())
		}
	} else {
		assert.NoError(t, got, "got unexpected error")
	}
}

func TestResolveTweets(t *testing.T) {
	t.Parallel()

	okFetcher := &testTweetFetcher{
		fetch: func(ctx context.Context, tweetURL string) (tweetData, error) {
			return tweetData{
				URL:  tweetURL,
				Text: "tweet text",
			}, nil
		},
	}
	errFetcher := &testTweetFetcher{
		fetch: func(ctx context.Context, tweetURL string) (tweetData, error) {
			return tweetData{}, errors.New("twitter error")
		},
	}

	// this transport will prevent Resolve from making real requests to
	// Twitter, so that these tests may safely redirect to Twitter URLs without
	// actually triggering external requsts.
	twitterInterceptTransport := &testTransport{
		roundTrip: func(r *http.Request) (*http.Response, error) {
			if strings.Contains(r.URL.Host, "twitter.com") {
				return &http.Response{
					StatusCode: 200,
					Request:    r,
				}, nil
			}
			return http.DefaultTransport.RoundTrip(r)
		},
	}

	testCases := map[string]struct {
		fullTweetURL string
		tweetFetcher tweetFetcher
		wantErr      error
		wantResult   Result
	}{
		"ok": {
			fullTweetURL: "https://twitter.com/username/status/1234/photos/1?foo=bar",
			tweetFetcher: okFetcher,
			wantResult: Result{
				ResolvedURL:      "https://twitter.com/username/status/1234", // note that full URL above was trimmed
				Title:            "tweet text",
				IntermediateURLs: []string{""}, // will be rendered to match test server URL
			},
		},
		"error fetching tweet": {
			fullTweetURL: "https://twitter.com/username/status/1234/photos/1?foo=bar",
			tweetFetcher: errFetcher,
			wantErr:      errors.New("twitter error"),
			// despite expected error, we still want a partial result
			wantResult: Result{
				ResolvedURL:      "https://twitter.com/username/status/1234", // note that full URL above was trimmed
				Title:            "",
				IntermediateURLs: []string{""}, // will be rendered to match test server URL
			},
		},
	}

	for name, tc := range testCases {
		tc := tc

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, tc.fullTweetURL, http.StatusFound)
			}))
			defer srv.Close()

			resolver := New(twitterInterceptTransport, 0)
			resolver.tweetFetcher = tc.tweetFetcher

			result, err := resolver.Resolve(context.Background(), srv.URL)
			assertErrorsMatch(t, tc.wantErr, err)

			// fixup relative intermediate URLs to include test server
			for idx, hop := range tc.wantResult.IntermediateURLs {
				tc.wantResult.IntermediateURLs[idx] = renderURL(srv.URL, hop)
			}

			assert.Equal(t, tc.wantResult, result)
		})
	}

	t.Run("short circuit when given twitter URL as input", func(t *testing.T) {
		t.Parallel()

		resolver := New(twitterInterceptTransport, 0)
		resolver.tweetFetcher = okFetcher

		result, err := resolver.Resolve(context.Background(), "https://twitter.com/username/status/1234/photos/1?foo=bar")
		assert.NoError(t, err)
		assert.Equal(t, Result{
			ResolvedURL: "https://twitter.com/username/status/1234", // note that full URL above was trimmed
			Title:       "tweet text",
		}, result)
	})
}

type testTweetFetcher struct {
	fetch func(context.Context, string) (tweetData, error)
}

func (f *testTweetFetcher) Fetch(ctx context.Context, tweetURL string) (tweetData, error) {
	return f.fetch(ctx, tweetURL)
}

type testTransport struct {
	roundTrip func(*http.Request) (*http.Response, error)
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.roundTrip(req)
}

func newSafeTestTransport(t *testing.T) *testTransport {
	return &testTransport{
		roundTrip: func(r *http.Request) (*http.Response, error) {
			if r.URL.Hostname() != "127.0.0.1" {
				t.Fatalf("external request to %q forbidden in this test suite", r.URL)
			}
			return http.DefaultTransport.RoundTrip(r)
		},
	}
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

func loadTitleTestCases(t *testing.T, maxBodySize int) map[string]titleTestCase {
	t.Helper()

	paths, err := filepath.Glob("./testdata/*.html")
	if err != nil {
		t.Fatalf("could not load test cases: %s", err)
	}

	testCases := make(map[string]titleTestCase, len(paths))
	for _, p := range paths {
		b, err := os.ReadFile(p)
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
