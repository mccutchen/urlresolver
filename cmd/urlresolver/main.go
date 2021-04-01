package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"time"

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
	"github.com/mccutchen/urlresolver/safetransport"
	"github.com/mccutchen/urlresolver/telemetry"
	"github.com/mccutchen/urlresolver/twitter"
	"github.com/mccutchen/urlresolver/urlresolver"
)

const defaultPort = "8080"

func main() {
	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
	stopTelemetry := initTelemetry(logger)
	defer stopTelemetry(context.Background())

	transport := telemetry.WrapTransport(safetransport.New())
	resolver := urlresolver.New(
		transport,
		twitter.New(transport),
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

type shutdownFunc func(context.Context) error

func initTelemetry(logger zerolog.Logger) shutdownFunc {
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
