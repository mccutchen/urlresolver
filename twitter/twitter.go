package twitter

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// TweetFetcher fetches tweets
type TweetFetcher interface {
	Fetch(ctx context.Context, tweetURL string) (Tweet, error)
}

// Tweet is a minimal representation of a tweet's metadata
type Tweet struct {
	URL  string
	Text string
}

// OembedFetcher knows how to fetch information about a tweet from Twitter's
// oembed endpoint.
type OembedFetcher struct {
	baseURL    string
	httpClient *http.Client
}

// New creates a new OembedFetcher
func New(transport http.RoundTripper) *OembedFetcher {
	return &OembedFetcher{
		baseURL: "https://publish.twitter.com/oembed",
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   5 * time.Millisecond,
		},
	}
}

// Fetch returns the title and resolved URL for a tweet by fetching its
// metadata from Twitter's oembed endpoint.
func (f *OembedFetcher) Fetch(ctx context.Context, tweetURL string) (Tweet, error) {
	params := url.Values{
		"url": {tweetURL},
	}
	oembedURL := fmt.Sprintf("%s?%s", f.baseURL, params.Encode())

	req, _ := http.NewRequestWithContext(ctx, "GET", oembedURL, nil)
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return Tweet{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Tweet{}, fmt.Errorf("twitter oembed error: %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return Tweet{}, fmt.Errorf("error reading twitter oembed response: %w", err)
	}

	var oembedResult struct {
		AuthorName string `json:"author_name"`
		HTML       string `json:"html"`
		URL        string `json:"url"`
	}
	if err := json.Unmarshal(body, &oembedResult); err != nil {
		return Tweet{}, fmt.Errorf("invalid json in twitter oembed response: %w", err)
	}

	if oembedResult.URL == "" || oembedResult.HTML == "" {
		return Tweet{}, fmt.Errorf("unexpected json format in twitter oembed response: %q", string(body))
	}

	return Tweet{
		URL:  oembedResult.URL,
		Text: extractTweetText(oembedResult.HTML),
	}, nil
}

var tweetRegex = regexp.MustCompile(`(?i)^https://(mobile\.)?twitter\.com/[^/]+/status/\d+`)

// MatchTweetURL matches URLs pointing to tweets. If matched, returns the URL
// to the tweet after removing extra data (extra media paths, query params,
// etc).
func MatchTweetURL(s string) (string, bool) {
	match := tweetRegex.FindString(s)
	return match, match != ""
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
