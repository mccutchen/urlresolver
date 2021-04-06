package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-redis/cache/v8"
	"github.com/go-redis/redis/v8"
	beeline "github.com/honeycombio/beeline-go"
	"github.com/honeycombio/beeline-go/wrappers/hnynethttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"

	"github.com/mccutchen/urlresolver/httphandler"
	"github.com/mccutchen/urlresolver/resolver"
	"github.com/mccutchen/urlresolver/safedialer"
	"github.com/mccutchen/urlresolver/telemetry"
)

const (
	cacheTTL        = 120 * time.Hour
	defaultPort     = "8080"
	requestTimeout  = 6 * time.Second
	shutdownTimeout = requestTimeout + 1*time.Second

	// dialer
	dialTimeout = 1 * time.Second

	// transport
	transportIdleConnTimeout     = 90 * time.Second
	transportMaxIdleConns        = 100
	transportMaxIdleConnsPerHost = 100
	transportTLSHandshakeTimeout = 1 * time.Second
)

func main() {
	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
	stopTelemetry := initTelemetry(logger)
	defer stopTelemetry()

	resolver := initResolver(logger)
	mux := http.NewServeMux()
	mux.Handle("/lookup", httphandler.New(resolver))

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	srv := &http.Server{
		Addr:    net.JoinHostPort("", port),
		Handler: applyMiddleware(mux, logger),
	}

	listenAndServeGracefully(srv, shutdownTimeout, logger)
}

func listenAndServeGracefully(srv *http.Server, shutdownTimeout time.Duration, logger zerolog.Logger) {
	// exitCh will be closed when it is safe to exit, after the server has had
	// a chance to shut down gracefully
	exitCh := make(chan struct{})

	go func() {
		// wait for SIGTERM or SIGINT
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		sig := <-sigCh

		// start graceful shutdown
		logger.Info().Msgf("shutdown started by signal: %s", sig)
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			logger.Error().Err(err).Msg("shutdown error")
		}

		// indicate that it is now safe to exit
		close(exitCh)
	}()

	// start server
	logger.Info().Msgf("listening on %s", srv.Addr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		logger.Error().Err(err).Msg("listen error")
	}

	// wait until it is safe to exit
	<-exitCh
}

func applyMiddleware(h http.Handler, l zerolog.Logger) http.Handler {
	h = hlog.AccessHandler(accessLogger)(h)
	h = hlog.NewHandler(l)(h)
	h = hnynethttp.WrapHandler(h)
	return h
}

func accessLogger(r *http.Request, status int, size int, duration time.Duration) {
	remoteAddr := r.Header.Get("Fly-Client-IP")
	if remoteAddr == "" {
		remoteAddr = r.RemoteAddr
	}

	hlog.FromRequest(r).Info().
		Str("method", r.Method).
		Str("remote_addr", remoteAddr).
		Stringer("url", r.URL).
		Int("status", status).
		Int("size", size).
		Dur("duration", duration).
		Send()
}

func initResolver(logger zerolog.Logger) resolver.Resolver {
	dialer := safedialer.New(net.Dialer{Timeout: dialTimeout})
	transport := telemetry.WrapTransport(&http.Transport{
		DialContext:         dialer.DialContext,
		IdleConnTimeout:     transportIdleConnTimeout,
		MaxIdleConnsPerHost: transportMaxIdleConnsPerHost,
		MaxIdleConns:        transportMaxIdleConnsPerHost * 2,
		TLSHandshakeTimeout: transportTLSHandshakeTimeout,
	})
	redisCache := initRedisCache(logger)

	var r resolver.Resolver = resolver.New(transport, requestTimeout)
	if redisCache != nil {
		r = resolver.NewCachedResolver(r, resolver.NewRedisCache(redisCache, cacheTTL))
	}
	return r
}

func initRedisCache(logger zerolog.Logger) *cache.Cache {
	redisURL := os.Getenv("FLY_REDIS_CACHE_URL")
	if redisURL == "" {
		logger.Info().Msg("FLY_REDIS_CACHE_URL not set, cache disabled")
		return nil
	}

	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		logger.Error().Err(err).Msg("FLY_REDIS_CACHE_URL invalid, cache disabled")
		return nil
	}

	return cache.New(&cache.Options{Redis: redis.NewClient(opt)})
}

func initTelemetry(logger zerolog.Logger) func() {
	var (
		apiKey  = os.Getenv("HONEYCOMB_API_KEY")
		dataset = "urlresolver"
	)

	if apiKey == "" {
		logger.Info().Msg("HONEYCOMB_API_KEY not set, telemetry disabled")
		return func() {}
	}

	beeline.Init(beeline.Config{
		WriteKey: apiKey,
		Dataset:  dataset,
	})
	return beeline.Close
}
