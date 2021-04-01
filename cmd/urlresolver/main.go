package main

import (
	"net"
	"net/http"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"

	"github.com/mccutchen/urlresolver/httphandler"
	"github.com/mccutchen/urlresolver/safetransport"
	"github.com/mccutchen/urlresolver/twitter"
	"github.com/mccutchen/urlresolver/urlresolver"
)

const defaultPort = "8080"

func main() {
	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
	resolver := urlresolver.New(
		safetransport.New(),
		twitter.New(),
	)
	handler := applyMiddleware(httphandler.New(resolver), logger)

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	addr := net.JoinHostPort("", port)
	logger.Info().Msgf("listening on %s", addr)
	logger.Fatal().Err(http.ListenAndServe(addr, handler)).Msg("finished")
}

func applyMiddleware(h http.Handler, l zerolog.Logger) http.Handler {
	h = hlog.AccessHandler(accessLogger)(h)
	h = hlog.NewHandler(l)(h)
	return h
}

func accessLogger(r *http.Request, status int, size int, duration time.Duration) {
	hlog.FromRequest(r).Info().
		Str("method", r.Method).
		Str("remote_addr", r.RemoteAddr).
		Stringer("url", r.URL).
		Int("status", status).
		Int("size", size).
		Dur("duration", duration).
		Send()
}
