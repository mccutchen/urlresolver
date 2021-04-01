package telemetry

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptrace"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/semconv"
	"go.opentelemetry.io/otel/trace"
)

func WrapTransport(transport http.RoundTripper) http.RoundTripper {
	return &traceTransport{transport}
}

type traceTransport struct {
	transport http.RoundTripper
}

func (t *traceTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	ctx := r.Context()
	ctx, span := trace.SpanFromContext(ctx).Tracer().Start(ctx, "http.request", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	span.SetAttributes(semconv.HTTPClientAttributesFromHTTPRequest(r)...)
	span.SetAttributes(semconv.NetAttributesFromHTTPRequest("tcp", r)...)

	ctx = httptrace.WithClientTrace(ctx, newClientTrace(ctx))
	r = r.WithContext(ctx)

	resp, err := t.transport.RoundTrip(r)

	if err != nil {
		span.SetAttributes(attribute.String("error", err.Error()))
	} else {
		span.SetAttributes(semconv.HTTPAttributesFromHTTPStatusCode(resp.StatusCode)...)
	}

	return resp, err
}

func newClientTrace(ctx context.Context) *httptrace.ClientTrace {
	tracer := &tracer{
		ctx: ctx,
	}
	return &httptrace.ClientTrace{
		DNSDone:              tracer.DNSDone,
		DNSStart:             tracer.DNSStart,
		GetConn:              tracer.GetConn,
		GotConn:              tracer.GotConn,
		GotFirstResponseByte: tracer.GotFirstResponseByte,
		TLSHandshakeDone:     tracer.TLSHandshakeDone,
		TLSHandshakeStart:    tracer.TLSHandshakeStart,
		WroteRequest:         tracer.WroteRequest,
	}
}

// Tracer implements a subset of the *httptrace.ClientTrace callbacks, and
// maintains state in order to instrument the various stages of an HTTP
// request.
type tracer struct {
	ctx          context.Context
	connectSpan  trace.Span
	dnsSpan      trace.Span
	tlsSpan      trace.Span
	upstreamSpan trace.Span
}

// GetConn is called before a connection is created or retrieved from an idle
// pool. The hostPort is the "host:port" of the target or proxy. GetConn is
// called even if there's already an idle cached connection available.
func (t *tracer) GetConn(hostPort string) {
	_, t.connectSpan = trace.SpanFromContext(t.ctx).Tracer().Start(t.ctx, "net.connect")
	if host, port, err := net.SplitHostPort(hostPort); err != nil {
		t.connectSpan.SetAttributes(
			attribute.String(string(semconv.NetHostNameKey), host),
			attribute.String(string(semconv.NetHostPortKey), port),
		)
	}
}

// GotConn is called after a successful connection is obtained. There is no
// hook for failure to obtain a connection; instead, use the error from
// Transport.RoundTrip.
func (t *tracer) GotConn(info httptrace.GotConnInfo) {
	t.connectSpan.SetAttributes(
		attribute.Bool("net.conn.reused", info.Reused),
		attribute.Bool("net.conn.was_idle", info.WasIdle),
	)
	t.connectSpan.End()
}

// DNSStart is called when a DNS lookup begins.
func (t *tracer) DNSStart(info httptrace.DNSStartInfo) {
	_, t.dnsSpan = trace.SpanFromContext(t.ctx).Tracer().Start(t.ctx, "net.dns_lookup")
	t.dnsSpan.SetAttributes(attribute.String(string(semconv.NetHostNameKey), info.Host))
}

// DNSDone is called when a DNS lookup ends.
func (t *tracer) DNSDone(info httptrace.DNSDoneInfo) {
	t.dnsSpan.End()
}

// TLSHandshakeStart is called when the TLS handshake is started. When
// connecting to a HTTPS site via a HTTP proxy, the handshake happens after the
// CONNECT request is processed by the proxy.
func (t *tracer) TLSHandshakeStart() {
	_, t.tlsSpan = trace.SpanFromContext(t.ctx).Tracer().Start(t.ctx, "net.tls_handshake")
}

// TLSHandshakeDone is called after the TLS handshake with either the
// successful handshake's connection state, or a non-nil error on handshake
// failure.
func (t *tracer) TLSHandshakeDone(state tls.ConnectionState, err error) {
	t.tlsSpan.SetAttributes(attribute.Bool("net.conn.tls_did_resume", state.DidResume))
	t.tlsSpan.End()
}

// WroteRequest is called with the result of writing the request and any body.
// It may be called multiple times in the case of retried requests.
func (t *tracer) WroteRequest(info httptrace.WroteRequestInfo) {
	if t.upstreamSpan == nil {
		_, t.upstreamSpan = trace.SpanFromContext(t.ctx).Tracer().Start(t.ctx, "net.conn.time_to_first_byte")
		if info.Err != nil {
			t.upstreamSpan.SetAttributes(attribute.String("error", info.Err.Error()))
		}
	}
}

// GotFirstResponseByte is called when the first byte of the response headers
// is available.
func (t *tracer) GotFirstResponseByte() {
	t.upstreamSpan.End()
}
