//nolint:errcheck
package urlresolver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/mccutchen/urlresolver/bufferpool"
	"github.com/stretchr/testify/assert"
)

func TestMatchTweetURL(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		given   string
		wantURL string
		wantOK  bool
	}{
		// basic tweet urls
		{"https://twitter.com/thresholderbot/status/1341197329550995456", "https://twitter.com/thresholderbot/status/1341197329550995456", true},

		// mobile links okay
		{"https://mobile.twitter.com/thresholderbot/status/1341197329550995456", "https://mobile.twitter.com/thresholderbot/status/1341197329550995456", true},

		// media links truncated to parent tweet
		{"https://twitter.com/thresholderbot/status/1341197329550995456/photo/1", "https://twitter.com/thresholderbot/status/1341197329550995456", true},

		// case insensitive
		{"https://TWITTER.com/McCutchen/status/1255896435540676610", "https://TWITTER.com/McCutchen/status/1255896435540676610", true},

		// query parameters ignored
		{"https://twitter.com/thresholderbot/status/1341197329550995456?utm_whatever=foo", "https://twitter.com/thresholderbot/status/1341197329550995456", true},

		// /i/web/status URLs matched
		{"https://twitter.com/i/web/status/1595160647238844416", "https://twitter.com/__urlresolver__/status/1595160647238844416", true},
		{"https://twitter.com/i/web/status/1595160647238844416?foo=bar", "https://twitter.com/__urlresolver__/status/1595160647238844416", true},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.given, func(t *testing.T) {
			t.Parallel()
			url, ok := matchTweetURL(tc.given)
			if url != tc.wantURL {
				t.Errorf("expected url == %q, got %q", tc.wantURL, url)
			}
			if ok != tc.wantOK {
				t.Errorf("expected ok == %v, got %v", tc.wantOK, ok)
			}
		})
	}
}

func TestExtractTweetText(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		given string
		want  string
	}{
		{
			// https://publish.twitter.com/oembed?url=https://twitter.com/NekiasNBA/status/1377329865133801484
			given: `<blockquote class=\"twitter-tweet\">
    <p lang=\"en\" dir=\"ltr\">🚨NEW WORDS ALERT🚨<br><br>Introducing <a
            href=\"https://twitter.com/hashtag/RoamingTheBaseline?src=hash&amp;ref_src=twsrc%5Etfw\">#RoamingTheBaseline</a>,
        a weekly notebook where I write about whatever catches my eye.<br><br>We’re talking Devonte’ pull-ups, an ode to
        Christyn Williams, some empty corner goodness and more!<a
            href=\"https://t.co/MykXNJkycw\">https://t.co/MykXNJkycw</a></p>&mdash; Nekias Duncan (@NekiasNBA) <a
        href=\"https://twitter.com/NekiasNBA/status/1377329865133801484?ref_src=twsrc%5Etfw\">March 31, 2021</a>
</blockquote>\n
<script async src=\"https://platform.twitter.com/widgets.js\" charset=\"utf-8\"></script>\n`,
			want: "🚨NEW WORDS ALERT🚨 Introducing #RoamingTheBaseline, a weekly notebook where I write about whatever catches my eye. We’re talking Devonte’ pull-ups, an ode to Christyn Williams, some empty corner goodness and more! https://t.co/MykXNJkycw",
		},
	}

	for i, tc := range testCases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			got := extractTweetText(tc.given)
			if got != tc.want {
				t.Errorf("incorrect extraction results:\nwant: %q\ngot:  %q", tc.want, got)
			}
		})
	}
}

func TestFetch(t *testing.T) {
	t.Parallel()

	const tweetURL = "https://twitter.com/thresholderbot/status/1341197329550995456"

	testCases := map[string]struct {
		handler    func(*testing.T) http.HandlerFunc
		timeout    time.Duration
		wantResult tweetData
		wantErr    error
	}{
		"ok": {
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					gotURL := r.URL.Query().Get("url")
					assert.Equal(t, tweetURL, gotURL)

					w.Write([]byte(`{
  "url": "https://twitter.com/thresholderbot/status/1341197329550995456",
  "author_name": "Thresholderbot",
  "author_url": "https://twitter.com/thresholderbot",
  "html": "<blockquote class=\"twitter-tweet\"><p lang=\"en\" dir=\"ltr\">Hi. As the year draws to a close, I just wanted to apologize for (probably) turning into a firehouse of bad news aimed directly into your inbox. Rest assured, those responsible have been sacked. <a href=\"https://t.co/o6S0p7s3Ce\">pic.twitter.com/o6S0p7s3Ce</a></p>&mdash; Thresholderbot (@thresholderbot) <a href=\"https://twitter.com/thresholderbot/status/1341197329550995456?ref_src=twsrc%5Etfw\">December 22, 2020</a></blockquote>\n<script async src=\"https://platform.twitter.com/widgets.js\" charset=\"utf-8\"></script>\n",
  "width": 550,
  "height": null,
  "type": "rich",
  "cache_age": "3153600000",
  "provider_name": "Twitter",
  "provider_url": "https://twitter.com",
  "version": "1.0"
}
`))
				}
			},
			wantResult: tweetData{
				Text: "Hi. As the year draws to a close, I just wanted to apologize for (probably) turning into a firehouse of bad news aimed directly into your inbox. Rest assured, those responsible have been sacked. pic.twitter.com/o6S0p7s3Ce",
				URL:  tweetURL,
			},
		},
		"timeout": {
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					select {
					case <-time.After(10 * time.Second):
					case <-r.Context().Done():
					}
				}
			},
			wantErr: errors.New("context deadline exceeded"),
		},
		"timeout during read": {
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
					select {
					case <-time.After(10 * time.Second):
					case <-r.Context().Done():
					}
				}
			},
			wantErr: errors.New("context deadline exceeded"),
		},
		"server error": {
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}
			},
			wantErr: errors.New("twitter oembed error:"),
		},
		"bad JSON": {
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte("["))
				}
			},
			wantErr: errors.New("invalid json in twitter oembed response"),
		},
		"nonsense JSON": {
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte("{}"))
				}
			},
			wantErr: errors.New("unexpected json format"),
		},
		"incomplete HTML": {
			handler: func(t *testing.T) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
  "url": "https://twitter.com/thresholderbot/status/1341197329550995456",
  "author_name": "Thresholderbot",
  "author_url": "https://twitter.com/thresholderbot",
  "html": "<blockquote class=\"twitter-tweet\"></blockquote>\n<script async src=\"https://platform.twitter.com/widgets.js\" charset=\"utf-8\"></script>\n",
  "width": 550,
  "height": null,
  "type": "rich",
  "cache_age": "3153600000",
  "provider_name": "Twitter",
  "provider_url": "https://twitter.com",
  "version": "1.0"
}
`))
				}
			},
			wantResult: tweetData{
				Text: "",
				URL:  tweetURL,
			},
		},
	}

	for name, tc := range testCases {
		tc := tc

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(tc.handler(t))
			defer srv.Close()

			fetcher := newTweetFetcher(http.DefaultTransport, 0, bufferpool.New())
			fetcher.baseURL = srv.URL + "/oembed"

			timeout := tc.timeout
			if timeout == 0 {
				timeout = 1 * time.Second
			}

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			result, err := fetcher.Fetch(ctx, tweetURL)
			if tc.wantErr != nil {
				assert.NotNil(t, err, "expected non-nil error")
				assert.Contains(t, err.Error(), tc.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.wantResult, result)
		})
	}
}
