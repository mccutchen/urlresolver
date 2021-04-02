package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/go-redis/cache/v8"
	"github.com/go-redis/redis/extra/redisotel"
	"github.com/go-redis/redis/v8"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpgrpc"
	"go.opentelemetry.io/otel/exporters/stdout"
	exporttrace "go.opentelemetry.io/otel/sdk/export/trace"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc/credentials"

	"github.com/mccutchen/urlresolver/httphandler"
	"github.com/mccutchen/urlresolver/resolver"
	"github.com/mccutchen/urlresolver/safetransport"
	"github.com/mccutchen/urlresolver/telemetry"
)

const (
	cacheTTL       = 120 * time.Hour
	defaultPort    = "8080"
	requestTimeout = 6 * time.Second
)

func main() {
	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
	stopTelemetry := initTelemetry(logger)
	defer stopTelemetry(context.Background())

	resolver := initResolver(logger)
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
	h = otelhttp.NewHandler(h, "urlresolver", otelhttp.WithSpanNameFormatter(func(op string, r *http.Request) string {
		return r.URL.Path
	}))
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
	transport := telemetry.WrapTransport(safetransport.New())
	redisCache := initRedisCache(logger)

	var r resolver.Resolver
	r = resolver.NewSingleFlightResolver(
		resolver.New(transport, requestTimeout),
	)
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

	client := redis.NewClient(opt)
	client.AddHook(redisotel.TracingHook{})

	return cache.New(&cache.Options{Redis: client})
}

func initTelemetry(logger zerolog.Logger) func(context.Context) error {
	var (
		apiKey  = os.Getenv("HONEYCOMB_API_KEY")
		dataset = "urlresolver"
	)

	var (
		exporter exporttrace.SpanExporter
		err      error
	)
	if apiKey == "" {
		logger.Info().Msg("HONEYCOMB_API_KEY not set, telemetry disabled")
		exporter = noopExporter()
	} else {
		exporter, err = otlp.NewExporter(
			context.Background(),
			otlpgrpc.NewDriver(
				otlpgrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, "")),
				otlpgrpc.WithEndpoint("api.honeycomb.io:443"),
				otlpgrpc.WithHeaders(map[string]string{
					"x-honeycomb-team":    apiKey,
					"x-honeycomb-dataset": dataset,
				}),
			),
		)
		if err != nil {
			logger.Error().Err(err).Msg("failed to initialize honeycomb opentelemetry exporter")
			exporter = noopExporter()
		}
	}

	otel.SetTracerProvider(
		trace.NewTracerProvider(
			trace.WithResource(resource.NewWithAttributes(
				attribute.String("service_name", os.Getenv("FLY_APP_NAME")),
				attribute.String("region", os.Getenv("FLY_REGION")),
				attribute.String("instance", os.Getenv("FLY_ALLOC_ID")),
			)),
			trace.WithSyncer(exporter),
		),
	)

	return exporter.Shutdown
}

func noopExporter() exporttrace.SpanExporter {
	exporter, _ := stdout.NewExporter(
		stdout.WithWriter(io.Discard),
	)
	return exporter
}
