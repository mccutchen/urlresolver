package urlresolver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/mccutchen/urlresolver/bufferpool"
)

// tweetFetcher fetches tweets
type tweetFetcher interface {
	Fetch(ctx context.Context, tweetURL string) (tweetData, error)
}

// tweetData is a minimal representation of a tweet's data
type tweetData struct {
	URL  string
	Text string
}

// oembedTweetFetcher knows how to fetch information about a tweet from Twitter's
// oembed endpoint.
type oembedTweetFetcher struct {
	baseURL    string
	httpClient *http.Client
	pool       *bufferpool.BufferPool
}

// newTweetFetcher creates a new oembedTweetFetcher
func newTweetFetcher(transport http.RoundTripper, timeout time.Duration, pool *bufferpool.BufferPool) *oembedTweetFetcher {
	return &oembedTweetFetcher{
		baseURL: "https://publish.twitter.com/oembed",
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   timeout,
		},
		pool: pool,
	}
}

// Fetch returns the title and resolved URL for a tweet by fetching its
// metadata from Twitter's oembed endpoint.
func (f *oembedTweetFetcher) Fetch(ctx context.Context, tweetURL string) (tweetData, error) {
	params := url.Values{
		"url": []string{tweetURL},
	}
	oembedURL := fmt.Sprintf("%s?%s", f.baseURL, params.Encode())

	req, _ := http.NewRequestWithContext(ctx, "GET", oembedURL, nil)
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return tweetData{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return tweetData{}, fmt.Errorf("twitter oembed error: GET %s: HTTP %d", oembedURL, resp.StatusCode)
	}

	buf := f.pool.Get()
	defer f.pool.Put(buf)

	if _, err := io.Copy(buf, resp.Body); err != nil {
		return tweetData{}, fmt.Errorf("error reading twitter oembed response: %w", err)
	}

	var oembedResult struct {
		AuthorName string `json:"author_name"`
		HTML       string `json:"html"`
		URL        string `json:"url"`
	}
	if err := json.Unmarshal(buf.Bytes(), &oembedResult); err != nil {
		return tweetData{}, fmt.Errorf("invalid json in twitter oembed response: %w", err)
	}

	if oembedResult.URL == "" || oembedResult.HTML == "" {
		return tweetData{}, fmt.Errorf("unexpected json format in twitter oembed response: %q", buf.String())
	}

	return tweetData{
		URL:  oembedResult.URL,
		Text: extractTweetText(oembedResult.HTML),
	}, nil
}

// https://regex101.com/r/EBKewP/1
var tweetRegex = regexp.MustCompile(`(?i)^https://(mobile\.)?twitter\.com/([^/]+/status/\d+|i/web/status/\d+)`)

// matchTweetURL matches URLs pointing to tweets. If matched, returns the URL
// to the tweet after removing extra data (extra media paths, query params,
// etc).
func matchTweetURL(s string) (string, bool) {
	match := tweetRegex.FindString(s)
	if match != "" {
		// Twitter's oembed endpoint does not work with these odd
		// /i/web/status/XXX URLs that contain a tweet ID without a username,
		// but it does seem to work if you construct a fake tweet URL with that
		// same tweet ID.
		//
		// So, for those URLs, we pretend the tweet has a username of
		// __urlresolver__.
		if strings.Contains(s, "/i/web/") {
			return strings.ReplaceAll(match, "/i/web/", "/__urlresolver__/"), true
		}
		return match, true
	}
	return "", false
}

var tcoRegex = regexp.MustCompile(`(?i)^https?://t\.co/.+`)

// matchTweetURL matches URLs pointing to tweets. If matched, returns the URL
// to the tweet after removing extra data (extra media paths, query params,
// etc).
func matchTcoURL(s string) bool {
	return tcoRegex.FindString(s) != ""
}

// extractTweetText extracts the text content of a tweet from its html form in
// the twitter oembed response.
//
// At a high level, this function 1) captures only the text within the first
// <p> element in s, 2) replaces all html tags within that <p> with spaces 3)
// normalizes whitespace.
//
// The goal is not perfect fidelity to the original tweet, but something useful
// as the sanitized "title" for a tweet URL.
func extractTweetText(s string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(s))
	var buf strings.Builder

	// Flag that indicates whether we have found the opening <p> and should
	// accumulate text into our buffer.
	captureText := false

outerLoop:
	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			break outerLoop
		case html.StartTagToken:
			tag := tokenizer.Token().Data
			if tag == "p" {
				captureText = true
			} else if captureText {
				// all tags within the <p> are replaced with whitespace, which
				// will be normalized later
				buf.WriteString(" ")
			}
		case html.TextToken:
			if captureText {
				buf.WriteString(tokenizer.Token().Data)
			}
		case html.EndTagToken:
			if tokenizer.Token().Data == "p" {
				break outerLoop
			}
		}
	}

	// Normalize runs of whitespace and newlines by splitting the buffered
	// string into fields and re-joining each with a single space.
	return strings.Join(strings.Fields(buf.String()), " ")
}
