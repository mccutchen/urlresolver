package urlresolver

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

	"github.com/andybalholm/brotli"
	"golang.org/x/net/html/charset"
	"golang.org/x/net/publicsuffix"
)

const (
	maxRedirects = 10
	maxBodySize  = 1024 // we'll read 1kb of body to find title
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

// Result is the result of resolving a URL.
type Result struct {
	ResolvedURL string `json:"resolved_url"`
	Title       string `json:"title"`
}

// New creates a new Resolver that will use the given transport.
func New(transport http.RoundTripper) *Resolver {
	return &Resolver{
		transport: transport,
	}
}

// Resolver resolves a URL by following any redirects; cleaning, normalizing,
// and canonicalizing the resulting final URL, and attempting to extract the
// title from URLs that resolve to HTML content.
type Resolver struct {
	transport http.RoundTripper
}

// Resolve resolves any redirects for a URL and attempts to extract the title
// from the final response body
func (r *Resolver) Resolve(ctx context.Context, givenURL string, referer string) (Result, error) {
	req, err := prepareRequest(ctx, givenURL, referer)
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
	result := Result{
		ResolvedURL: Canonicalize(resp.Request.URL),
	}

	title, err := maybeParseTitle(resp)
	if err != nil {
		return result, err
	}

	result.Title = title
	return result, err
}

func (r *Resolver) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return http.ErrUseLastResponse
	}
	// Ugly hack to work around forbes interstitial
	if strings.Contains(req.URL.String(), "forbes.com/forbes/welcome") {
		return http.ErrUseLastResponse
	}
	return nil

}

func (r *Resolver) httpClient() *http.Client {
	cookieJar, _ := cookiejar.New(&cookiejar.Options{
		PublicSuffixList: publicsuffix.List,
	})
	return &http.Client{
		CheckRedirect: r.checkRedirect,
		Jar:           cookieJar,
		Transport:     r.transport,
	}
}

func prepareRequest(ctx context.Context, url string, referer string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	for k, v := range defaultHeaders {
		req.Header.Set(k, v)
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
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
	return string(bytes.TrimSpace(matches[1]))
}
