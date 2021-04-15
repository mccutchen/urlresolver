package main

import (
	"context"
	"crypto/sha1"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
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
	cacheTTL    = 120 * time.Hour
	defaultPort = "8080"

	// How long we will wait for a client to write its request or read our
	// response.
	clientPatience = 2 * time.Second

	// requestTimeout sets an overall timeout on a single resolve request,
	// including any redirects that must be followed and any time spent in DNS
	// lookup, tcp connect, tls handshake, etc.
	requestTimeout = 10 * time.Second

	// shutdownTimeout is just a bit longer than we expect the longest
	// individual request we're handling to take.
	shutdownTimeout = requestTimeout + clientPatience

	// dialTimeout determines how long we'll wait to make a connection to a
	// remote host.
	dialTimeout = 2 * time.Second

	// server timeouts prevent slow/malicious clients from occupying resources
	// for too long.
	serverReadTimeout  = clientPatience
	serverWriteTimeout = requestTimeout + clientPatience

	// configure our http client to reuse connections somewhat aggressively.s
	transportIdleConnTimeout     = 90 * time.Second
	transportMaxIdleConns        = 100
	transportMaxIdleConnsPerHost = 100
	transportTLSHandshakeTimeout = 2 * time.Second
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
		Addr:         net.JoinHostPort("", port),
		Handler:      applyMiddleware(mux, logger),
		ReadTimeout:  serverReadTimeout,
		WriteTimeout: serverWriteTimeout,
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
	h = func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if p := recover(); p != nil {
					buf := make([]byte, 2048)
					n := runtime.Stack(buf, false)
					stack := string(buf[:n])
					msg := fmt.Sprintf("panic: %s", p)
					ctx := r.Context()

					l.Error().Str("stack", stack).Msg(msg)
					beeline.AddField(ctx, "error", msg)
					beeline.AddField(ctx, "stack", stack)

					w.WriteHeader(http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}(h)
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
		SampleRate:  10, // submit 10% or 1/10 events
		// SamplerHook: makeHoneycombSampler(10), // default sample rate of 10 submits 1/10 events
	})
	return beeline.Close
}

func makeHoneycombSampler(sampleRate int) func(map[string]interface{}) (bool, int) {
	// Deterministic shouldSample taken from https://github.com/honeycombio/beeline-go/blob/7df4c61d91994bd39cc4c458c2e4cc3c0be007e7/sample/deterministic_sampler.go#L55-L57
	shouldSample := func(traceId string, sampleRate int) bool {
		upperBound := math.MaxUint32 / uint32(sampleRate)
		sum := sha1.Sum([]byte(traceId))
		b := sum[:4]
		v := uint32(b[3]) | (uint32(b[2]) << 8) | (uint32(b[1]) << 16) | (uint32(b[0]) << 24)
		return v < upperBound
	}

	return func(fields map[string]interface{}) (bool, int) {
		// Capture all events where an error occurred
		if _, found := fields["error"]; found {
			return true, 1
		}
		if _, found := fields["app.error"]; found {
			return true, 1
		}

		// Capture all non-200 responses from our request handlers
		if _, found := fields["handler.name"]; found {
			if resp, found := fields["response.status_code"]; found && resp.(int) != 200 {
				return true, 1
			}
		}

		// Otherwise, deterministically sample at the given rate
		if shouldSample(fields["trace.trace_id"].(string), sampleRate) {
			return true, sampleRate
		}
		return false, 0
	}
}
