package httphandler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/mccutchen/urlresolver/urlresolver"
	"github.com/rs/zerolog/hlog"
)

// New creates a new Handler.
func New(resolver *urlresolver.Resolver) *Handler {
	return &Handler{
		resolver: resolver,
	}
}

// Handler is an HTTP request handler that can resolve URLs.
type Handler struct {
	resolver *urlresolver.Resolver
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		sendError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	switch r.URL.Path {
	case "/":
		h.handleIndex(w, r)
	case "/lookup":
		h.handleLookup(w, r)
	default:
		sendError(w, "Not found", http.StatusNotFound)
	}
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Hello, world")
}

func (h *Handler) handleLookup(w http.ResponseWriter, r *http.Request) {
	givenURL := r.URL.Query().Get("url")
	if givenURL == "" {
		sendError(w, "Missing arg url", http.StatusBadRequest)
		return
	}
	if !isValidInput(givenURL) {
		sendError(w, "Invalid url", http.StatusBadRequest)
		return
	}

	// Note: it's possible to get an error while still getting a useful result
	// (e.g. a short URL has expanded to a long URL that we can meaningfully
	// canonicalize, but the request to fetch the title times out).
	//
	// So, we always log the error, but we only return an error response if we
	// did not manage to resolve the URL.
	result, err := h.resolver.Resolve(r.Context(), givenURL)
	if err != nil {
		hlog.FromRequest(r).Error().Err(err).Str("url", givenURL).Msg("error resolving url")
		if result.ResolvedURL == "" {
			sendError(w, "Error resolving URL", http.StatusBadGateway)
			return
		}
	}

	json.NewEncoder(w).Encode(result)
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

func sendError(w http.ResponseWriter, msg string, code int) {
	w.WriteHeader(code)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": msg,
	})
}
