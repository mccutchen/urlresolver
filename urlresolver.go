package urlresolver

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
	"golang.org/x/net/publicsuffix"
	"golang.org/x/sync/singleflight"

	"github.com/mccutchen/urlresolver/bufferpool"
)

const (
	defaultTimeout = 5 * time.Second
	maxRedirects   = 5
	maxBodySize    = 500 * 1024 // we'll read 500kb of body to find title
)

// Interface defines the interface for a URL resolver.
type Interface interface {
	Resolve(context.Context, string) (Result, error)
}

// Result is the result of resolving a URL.
type Result struct {
	ResolvedURL      string
	Title            string
	IntermediateURLs []string
}

// Resolver resolves URLs.
type Resolver struct {
	pool              *bufferpool.BufferPool
	singleflightGroup *singleflight.Group
	timeout           time.Duration
	transport         http.RoundTripper
	tweetFetcher      tweetFetcher
}

var _ Interface = &Resolver{} // Resolver implements Interface

// New creates a new Resolver that uses the given transport to make HTTP
// requests and applies the given timeout to the overall process (including any
// redirects that must be followed).
func New(transport http.RoundTripper, timeout time.Duration) *Resolver {
	if timeout == 0 {
		timeout = defaultTimeout
	}
	pool := bufferpool.New()
	return &Resolver{
		pool:              pool,
		singleflightGroup: &singleflight.Group{},
		timeout:           timeout,
		transport:         transport,
		tweetFetcher:      newTweetFetcher(http.DefaultTransport, timeout, pool),
	}
}

// Resolve resolves the given URL by following any redirects, canonicalizing
// the final URL, and attempting to extract the title from the final response
// body.
func (r *Resolver) Resolve(ctx context.Context, givenURL string) (Result, error) {
	// Immediately canonicalize the given URL to slightly increase the chance
	// of coalescing multiple requests into one.
	if u, err := url.Parse(givenURL); err == nil {
		givenURL = Canonicalize(u)
	}

	val, err, _ := r.singleflightGroup.Do(givenURL, func() (interface{}, error) {
		return r.doResolve(ctx, givenURL)
	})
	return val.(Result), err
}

func (r *Resolver) doResolve(ctx context.Context, givenURL string) (Result, error) {
	result := Result{ResolvedURL: givenURL}

	// Short-circuit special case for tweet URLs, which we ask Twitter to help
	// us resolve.
	if tweetURL, ok := matchTweetURL(givenURL); ok {
		return r.resolveTweet(ctx, tweetURL, result)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", givenURL, nil)
	if err != nil {
		return result, err
	}

	if matchTcoURL(givenURL) {
		req.Header.Set("User-Agent", "curl/7.64.1")
	}

	recorder := &redirectRecorder{&result}

	resp, err := r.httpClient(recorder).Do(req)
	if err != nil {
		// If there's a URL associated with the error, we still want to
		// canonicalize it and return a partial result. This gives us a useful
		// result when we go through one or more redirects but the final URL
		// fails to load (timeout, TLS error, etc).
		//
		// Note: AFAICT, the error from Do() will always be a *url.Error.
		if urlErr, ok := err.(*url.Error); ok {
			result.ResolvedURL = urlErr.URL
			if intermediateURL, _ := url.Parse(urlErr.URL); intermediateURL != nil {
				result.ResolvedURL = Canonicalize(intermediateURL)
			}
		}

		return result, err
	}
	defer resp.Body.Close()

	// At this point, we have at least resolved and canonicalized the URL,
	// whether or not we can successfully extract a title.
	result.ResolvedURL = Canonicalize(resp.Request.URL)

	// Check again for the chance to special-case tweet URLs *after* following
	// any redirects.
	if tweetURL, ok := matchTweetURL(result.ResolvedURL); ok {
		return r.resolveTweet(ctx, tweetURL, result)
	}

	result.Title, err = r.maybeParseTitle(resp)
	return result, err
}

func (r *Resolver) resolveTweet(ctx context.Context, tweetURL string, result Result) (Result, error) {
	tweet, err := r.tweetFetcher.Fetch(ctx, tweetURL)
	if err != nil {
		// We have a resolved tweet URL, so we return a partial result along
		// with the error
		result.ResolvedURL = tweetURL
		return result, err
	}

	result.ResolvedURL = tweet.URL
	result.Title = tweet.Text
	return result, nil
}

func (r *Resolver) httpClient(recorder *redirectRecorder) *http.Client {
	cookieJar, _ := cookiejar.New(&cookiejar.Options{
		PublicSuffixList: publicsuffix.List,
	})
	return &http.Client{
		CheckRedirect: recorder.checkRedirect,
		Jar:           cookieJar,
		Transport:     r.transport,
		Timeout:       r.timeout,
	}
}

func (r *Resolver) maybeParseTitle(resp *http.Response) (string, error) {
	if !shouldParseTitle(resp) {
		return "", nil
	}

	body, err := r.peekBody(resp)
	if err != nil {
		return "", err
	}

	return findTitle(body), nil
}

func (r *Resolver) peekBody(resp *http.Response) ([]byte, error) {
	buf := r.pool.Get()
	defer r.pool.Put(buf)

	if _, err := io.Copy(buf, io.LimitReader(resp.Body, maxBodySize)); err != nil {
		return nil, fmt.Errorf("error reading response: %w", err)
	}

	body, err := decodeBody(buf.Bytes(), resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, fmt.Errorf("error decoding response: %w", err)
	}

	return body, nil
}

func shouldParseTitle(resp *http.Response) bool {
	contentType := resp.Header.Get("Content-Type")
	return strings.Contains(contentType, "html") || contentType == ""
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
//	<title>Hi XSS vuln <script>alert('HACKED');</script>
//
// will be parsed as
//
//	'Hi XSS vuln '
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

type redirectRecorder struct {
	result *Result
}

func (r *redirectRecorder) checkRedirect(req *http.Request, via []*http.Request) error {
	r.result.IntermediateURLs = append(r.result.IntermediateURLs, via[len(via)-1].URL.String())

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
