package tracetransport

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptrace"

	"github.com/honeycombio/beeline-go"
	"github.com/honeycombio/beeline-go/trace"
	"github.com/honeycombio/beeline-go/wrappers/hnynethttp"
)

// New returns a new transport that adds detailed instrumentation to all
// outgoing requests.
func New(transport http.RoundTripper) http.RoundTripper {
	// Honeycomb's transport will add baseline HTTP request instrumentation, our
	// transport will add detailed network connection info.
	return hnynethttp.WrapRoundTripper(&traceTransport{transport})
}

type traceTransport struct {
	transport http.RoundTripper
}

func (t *traceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	ctx = httptrace.WithClientTrace(ctx, newClientTrace(ctx))
	req = req.WithContext(ctx)

	resp, err := t.transport.RoundTrip(req)

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
	connectSpan  *trace.Span
	dnsSpan      *trace.Span
	tlsSpan      *trace.Span
	upstreamSpan *trace.Span
}

// GetConn is called before a connection is created or retrieved from an idle
// pool. The hostPort is the "host:port" of the target or proxy. GetConn is
// called even if there's already an idle cached connection available.
func (t *tracer) GetConn(hostPort string) {
	_, t.connectSpan = beeline.StartSpan(t.ctx, "net.connect")
	if host, port, err := net.SplitHostPort(hostPort); err == nil {
		t.connectSpan.AddField("net.host.name", host)
		t.connectSpan.AddField("net.host.port", port)
	}
}

// GotConn is called after a successful connection is obtained. There is no
// hook for failure to obtain a connection; instead, use the error from
// Transport.RoundTrip.
func (t *tracer) GotConn(info httptrace.GotConnInfo) {
	t.connectSpan.AddField("net.conn.reused", info.Reused)
	t.connectSpan.AddField("net.conn.was_idle", info.WasIdle)
	t.connectSpan.Send()
}

// DNSStart is called when a DNS lookup begins.
func (t *tracer) DNSStart(info httptrace.DNSStartInfo) {
	_, t.dnsSpan = beeline.StartSpan(t.ctx, "net.dns_lookup")
	t.dnsSpan.AddField("net.host.name", info.Host)
}

// DNSDone is called when a DNS lookup ends.
func (t *tracer) DNSDone(info httptrace.DNSDoneInfo) {
	t.dnsSpan.Send()
}

// TLSHandshakeStart is called when the TLS handshake is started. When
// connecting to a HTTPS site via a HTTP proxy, the handshake happens after the
// CONNECT request is processed by the proxy.
func (t *tracer) TLSHandshakeStart() {
	_, t.tlsSpan = beeline.StartSpan(t.ctx, "net.tls_handshake")
}

// TLSHandshakeDone is called after the TLS handshake with either the
// successful handshake's connection state, or a non-nil error on handshake
// failure.
func (t *tracer) TLSHandshakeDone(state tls.ConnectionState, err error) {
	t.tlsSpan.AddField("net.conn.tls_did_resume", state.DidResume)
	t.tlsSpan.Send()
}

// WroteRequest is called with the result of writing the request and any body.
// It may be called multiple times in the case of retried requests.
func (t *tracer) WroteRequest(info httptrace.WroteRequestInfo) {
	if t.upstreamSpan == nil {
		_, t.upstreamSpan = beeline.StartSpan(t.ctx, "net.conn.time_to_first_byte")
		if info.Err != nil {
			t.upstreamSpan.AddField("error", info.Err.Error())
		}
	}
}

// GotFirstResponseByte is called when the first byte of the response headers
// is available.
func (t *tracer) GotFirstResponseByte() {
	t.upstreamSpan.Send()
}
