/*
Package httphandler provides a basic net/http http.Handler implementation that
resolves URLs.

The handler expects a ?url=URL_TO_RESOLVE query parameter, and responds with a
JSON object containing the resolved URL and the resolved title:

    $ curl -s localhost:8080/resolve?url=https://nyti.ms/2FVHq9v | jq .
    {
        "given_url": "https://nyti.ms/2FVHq9v",
        "resolved_url": "https://www.nytimes.com/tips",
        "title": "Tips - The New York Times"
    }

If an error occurs during resolution, the response status code will be 203
Non-Authoritative Information (to indicate partial response) and an additional
error field will be added and a partial result will be returned, including the
canonicalized and potentially partially-resolved URL:

    $ curl -s localhost:8080/resolve?url=https://i-do-not-exist.xyz?utm_tag=tracking-code | jq .
    {
        "given_url": "https://i-do-not-exist.xyz?utm_tag=tracking-code",
        "resolved_url": "https://i-do-not-exist.xyz",
        "title": "",
        "error": "resolve error"
    }

*/
package httphandler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/honeycombio/beeline-go"
	"github.com/rs/zerolog/hlog"

	"github.com/mccutchen/urlresolver"
	"github.com/mccutchen/urlresolver/safedialer"
)

// Errors that might be returned by the HTTP handler.
var (
	ErrRequestTimeout = errors.New("request timeout")
	ErrResolveError   = errors.New("resolve error")
	ErrUnsafeURL      = errors.New("unsafe URL")
)

// Cache control
const (
	maxAgeOK  = 365 * 24 * time.Hour
	maxAgeErr = 5 * time.Minute
)

// ResolveResponse defines the HTTP handler's response structure.
type ResolveResponse struct {
	GivenURL    string `json:"given_url"`
	ResolvedURL string `json:"resolved_url"`
	Title       string `json:"title"`
	Error       string `json:"error,omitempty"`
}

// New creates a new Handler.
func New(resolver urlresolver.Interface) *Handler {
	return &Handler{
		resolver: resolver,
	}
}

// Handler is an HTTP request handler that can resolve URLs.
type Handler struct {
	resolver urlresolver.Interface
}

var _ http.Handler = &Handler{} // Handler implements http.Handler

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	givenURL := r.URL.Query().Get("url")
	if givenURL == "" {
		beeline.AddField(ctx, "error", "missing_arg_url")
		sendError(w, "Missing arg url", http.StatusBadRequest)
		return
	}
	if !isValidInput(givenURL) {
		beeline.AddField(ctx, "error", "invalid_url")
		sendError(w, "Invalid url", http.StatusBadRequest)
		return
	}

	// Note: it's possible to get an error while still getting a useful result
	// (e.g. a short URL has expanded to a long URL that we can meaningfully
	// canonicalize, but the request to fetch the title times out).
	//
	// So, we always log the error, but we only return an error response if we
	// did not manage to resolve the URL.
	result, err := h.resolver.Resolve(ctx, givenURL)

	resp := ResolveResponse{
		GivenURL:    givenURL,
		ResolvedURL: result.ResolvedURL,
		Title:       result.Title,
	}
	code := http.StatusOK

	if err != nil {
		// Special case when client closed connection, no need to respond
		if errors.Is(err, context.Canceled) {
			beeline.AddField(ctx, "error", "client closed connection")
			hlog.FromRequest(r).Error().Err(err).Str("url", givenURL).Msg("client closed connection")
			// Use non-standard 499 Client Closed Request status for our own
			// instrumentation purposes (https://httpstatuses.com/499)
			w.WriteHeader(499)
			return
		}

		// Record the real error
		beeline.AddField(ctx, "error", err.Error())
		hlog.FromRequest(r).Error().Err(err).Str("url", givenURL).Msg("error resolving url")

		// A slight abuse of 203 Non-Authoritative Information to indicate a
		// partial result. See https://httpstatuses.com/203.
		code = http.StatusNonAuthoritativeInfo

		// Rewrite the error to hide implementation details
		resp.Error = mapError(err).Error()
	}

	sendJSON(w, code, resp)
}

func isValidInput(givenURL string) bool {
	// Separate conditionals instead of one-liner let us use code coverage to
	// make sure we're covering the cases we care about.
	parsed, err := url.Parse(givenURL)
	if err != nil {
		return false
	}
	if !parsed.IsAbs() {
		return false
	}
	if parsed.Hostname() == "" {
		return false
	}
	return true
}

func sendJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", cacheControlValue(code))
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}

func sendError(w http.ResponseWriter, msg string, code int) {
	sendJSON(w, code, map[string]string{
		"error": msg,
	})
}

func cacheControlValue(code int) string {
	// Allow API responses to be cached aggressively
	maxAge := maxAgeErr
	if code == http.StatusOK {
		maxAge = maxAgeOK
	}
	return fmt.Sprintf("public,max-age=%.0f", maxAge.Seconds())
}

func mapError(err error) error {
	switch {
	case isTimeoutError(err):
		return ErrRequestTimeout
	case isUnsafeError(err):
		return ErrUnsafeURL
	default:
		return ErrResolveError
	}
}

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) || isTimeoutError(errors.Unwrap(err))
}

func isUnsafeError(err error) bool {
	return errors.Is(err, safedialer.ErrUnsafeIP) ||
		errors.Is(err, safedialer.ErrUnsafePort) ||
		errors.Is(err, safedialer.ErrUnsafeNetwork)
}
