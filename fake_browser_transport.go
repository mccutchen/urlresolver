package urlresolver

import (
	"net/http"
)

// Not very sportsmanlike, but basically effective at letting us fetch page
// titles.
var fakeBrowserHeaders = map[string]string{
	"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8",
	"Accept-Language": "en-US,en;q=0.5",
	"Referer":         "https://duckduckgo.com/",
	"User-Agent":      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:87.0) Gecko/20100101 Firefox/87.0",
}

type fakeBrowserTransport struct {
	transport http.RoundTripper
}

func (t *fakeBrowserTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for key, value := range fakeBrowserHeaders {
		if req.Header.Get(key) == "" {
			req.Header.Set(key, value)
		}
	}
	return t.transport.RoundTrip(req)
}
