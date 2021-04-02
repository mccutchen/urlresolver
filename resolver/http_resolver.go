package resolver

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/andybalholm/brotli"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
	"golang.org/x/net/publicsuffix"
	"golang.org/x/sync/singleflight"

	"github.com/mccutchen/urlresolver/twitter"
)

const (
	maxRedirects   = 10
	maxBodySize    = 500 * 1024 // we'll read 500kb of body to find title
	requestTimeout = 5 * time.Second
)

// Not very sportsmanlike, but basically effective at letting us fetch page
// titles.
var defaultHeaders = map[string]string{
	"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8",
	"Accept-Encoding": "gzip, deflate, br",
	"Accept-Language": "en-US,en;q=0.5",
	"Referer":         "https://duckduckgo.com/",
	"User-Agent":      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:84.0) Gecko/20100101 Firefox/84.0",
}

// New creates a new HTTPResolver that will use the given transport.
func New(transport http.RoundTripper, tweetFetcher twitter.TweetFetcher) *HTTPResolver {
	return &HTTPResolver{
		transport:    transport,
		resolveGroup: &singleflight.Group{},
		tweetFetcher: tweetFetcher,
	}
}

// HTTPResolver resolves a URL by following any redirects; cleaning, normalizing,
// and canonicalizing the resulting final URL, and attempting to extract the
// title from URLs that resolve to HTML content.
type HTTPResolver struct {
	transport    http.RoundTripper
	resolveGroup *singleflight.Group
	tweetFetcher twitter.TweetFetcher
}

// Resolve resolves any redirects for a URL and attempts to extract the title
// from the final response body
func (r *HTTPResolver) Resolve(ctx context.Context, givenURL string) (Result, error) {
	req, err := prepareRequest(ctx, givenURL)
	if err != nil {
		return Result{}, err
	}

	resp, err := r.httpClient().Do(req)
	if err != nil {
		if urlErr, ok := err.(*url.Error); ok && urlErr.Err == context.DeadlineExceeded {
			err = context.DeadlineExceeded
		}
		return Result{}, fmt.Errorf("error making http request: %w", err)
	}
	defer resp.Body.Close()

	// At this point, we have at least resolved and canonicalized the URL,
	// whether or not we can successfully extract a title.
	resolvedURL := Canonicalize(resp.Request.URL)

	// Special case for tweet URLs, which we ask Twitter to help us resolve
	if tweetURL, ok := twitter.MatchTweetURL(resolvedURL); ok {
		return r.resolveTweet(ctx, tweetURL)
	}

	result := Result{
		ResolvedURL: resolvedURL,
	}
	title, err := maybeParseTitle(resp)
	if err != nil {
		return result, err
	}

	result.Title = title
	return result, err
}

func (r *HTTPResolver) resolveTweet(ctx context.Context, tweetURL string) (Result, error) {
	tweet, err := r.tweetFetcher.Fetch(ctx, tweetURL)
	if err != nil {
		// We have a resolved tweet URL, so we return a partial result along
		// with the error
		return Result{ResolvedURL: tweetURL}, err
	}

	return Result{
		ResolvedURL: tweet.URL,
		Title:       tweet.Text,
	}, nil
}

func (r *HTTPResolver) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return http.ErrUseLastResponse
	}
	// Work around instagram auth redirect
	if strings.Contains(req.URL.String(), "instagram.com/accounts/login/") {
		return http.ErrUseLastResponse
	}
	// Work around forbes paywall interstitial
	if strings.Contains(req.URL.String(), "forbes.com/forbes/welcome") {
		return http.ErrUseLastResponse
	}
	return nil

}

func (r *HTTPResolver) httpClient() *http.Client {
	cookieJar, _ := cookiejar.New(&cookiejar.Options{
		PublicSuffixList: publicsuffix.List,
	})
	return &http.Client{
		CheckRedirect: r.checkRedirect,
		Jar:           cookieJar,
		Transport:     r.transport,
		Timeout:       requestTimeout,
	}
}

func prepareRequest(ctx context.Context, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	for k, v := range defaultHeaders {
		req.Header.Set(k, v)
	}

	return req, nil
}

func maybeParseTitle(resp *http.Response) (string, error) {
	if !shouldParseTitle(resp) {
		return "", nil
	}

	body, err := peekBody(resp)
	if err != nil {
		return "", err
	}

	return findTitle(body), nil
}

func shouldParseTitle(resp *http.Response) bool {
	contentType := resp.Header.Get("Content-Type")
	return strings.Contains(contentType, "html") || contentType == ""
}

func peekBody(resp *http.Response) ([]byte, error) {
	var rd io.Reader = resp.Body
	switch resp.Header.Get("Content-Encoding") {
	case "gzip":
		gr, err := gzip.NewReader(rd)
		if err != nil {
			return nil, fmt.Errorf("error initializing gzip: %w", err)
		}
		defer gr.Close()
		rd = gr
	case "deflate":
		fr := flate.NewReader(rd)
		defer fr.Close()
		rd = fr
	case "br":
		rd = brotli.NewReader(rd)
	}

	rawBody, err := ioutil.ReadAll(io.LimitReader(rd, maxBodySize))
	if err != nil {
		return nil, fmt.Errorf("error reading response: %w", err)
	}

	body, err := decodeBody(rawBody, resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, fmt.Errorf("error decoding response: %w", err)
	}

	return body, nil
}

func decodeBody(body []byte, contentType string) ([]byte, error) {
	enc, encName, _ := charset.DetermineEncoding(body, contentType)
	if encName == "utf-8" {
		return body, nil
	}
	return enc.NewDecoder().Bytes(body)
}

// Using this naive regex has the nice side effect of preventing
// us from ingesting malformed & potentially malicious titles,
// so this bad title
//
//     <title>Hi XSS vuln <script>alert('HACKED');</script>
//
// will be parsed as
//
//     'Hi XSS vuln '
//
// Hooray for dumb things that accidentally protect you!
var titleRegex = regexp.MustCompile(`(?im)<title[^>]*?>([^<]+)`)

func findTitle(body []byte) string {
	matches := titleRegex.FindSubmatch(body)
	if len(matches) < 2 {
		return ""
	}
	return html.UnescapeString(string(bytes.TrimSpace(matches[1])))
}
