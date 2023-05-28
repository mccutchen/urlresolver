package fakebrowser

import (
	"net/http"
)

// DefaultHeaders defines the headers that will be injected into every outgoing
// request in order to simulate the appearance of a real web browser.
//
// Not very sportsmanlike, but basically effective at letting us fetch page
// titles.
var DefaultHeaders = map[string]string{
	"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
	"Accept-Language": "en-US,en;q=0.5",
	"Referer":         "https://duckduckgo.com/",
	"User-Agent":      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:109.0) Gecko/20100101 Firefox/113.0",
}

// Transport is an http.RoundTripper implementation that injects a set of
// headers into every outgoing request in order to fake the appearance of a
// real web browser making the request.
type Transport struct {
	transport     http.RoundTripper
	injectHeaders map[string]string
}

var _ http.RoundTripper = &Transport{} // Transport implements http.RoundTripper

// New creates a new fake browswer transport.
func New(transport http.RoundTripper, opts ...Option) *Transport {
	t := &Transport{
		transport:     transport,
		injectHeaders: DefaultHeaders,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// RoundTrip executes a single HTTP transaction, after injecting a set of
// headers into the outgoing request.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	// existing headers take precedence over injected headers
	for key, value := range t.injectHeaders {
		if req.Header.Get(key) == "" {
			req.Header.Set(key, value)
		}
	}
	return t.transport.RoundTrip(req)
}

// Option customizes a Transport.
type Option func(*Transport)

// WithHeaders overrides the default set of headers injected into each request.
func WithHeaders(injectHeaders map[string]string) Option {
	return func(t *Transport) {
		t.injectHeaders = injectHeaders
	}
}
