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

	"github.com/mccutchen/urlresolver"
	"github.com/mccutchen/urlresolver/cachedresolver"
	"github.com/mccutchen/urlresolver/httphandler"
	"github.com/mccutchen/urlresolver/safedialer"
	"github.com/mccutchen/urlresolver/tracetransport"
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

func initResolver(logger zerolog.Logger) urlresolver.Interface {
	transport := tracetransport.New(&http.Transport{
		DialContext: (&net.Dialer{
			Control: safedialer.Control,
			Timeout: dialTimeout,
		}).DialContext,
		IdleConnTimeout:     transportIdleConnTimeout,
		MaxIdleConnsPerHost: transportMaxIdleConnsPerHost,
		MaxIdleConns:        transportMaxIdleConnsPerHost * 2,
		TLSHandshakeTimeout: transportTLSHandshakeTimeout,
	})
	redisCache := initRedisCache(logger)

	var r urlresolver.Interface = urlresolver.New(transport, requestTimeout)
	if redisCache != nil {
		r = cachedresolver.NewCachedResolver(r, cachedresolver.NewRedisCache(redisCache, cacheTTL))
	}
	return r
}

func initRedisCache(logger zerolog.Logger) *cache.Cache {
	redisURL := os.Getenv("FLY_REDIS_CACHE_URL")
	if redisURL == "" {
		logger.Info().Msg("set FLY_REDIS_CACHE_URL to enable caching")
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
		apiKey      = os.Getenv("HONEYCOMB_API_KEY")
		serviceName = os.Getenv("FLY_APP_NAME")
	)

	if apiKey == "" {
		logger.Info().Msg("set HONEYCOMB_API_KEY to capture telemetry")
		return func() {}
	}
	if serviceName == "" {
		serviceName = "urlresolver"
	}

	beeline.Init(beeline.Config{
		Dataset:     serviceName,
		ServiceName: serviceName,
		WriteKey:    apiKey,
		SampleRate:  10, // sample 1/10 or 10% events
	})
	return beeline.Close
}
