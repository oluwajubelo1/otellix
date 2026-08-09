// Package nethttp provides an OpenTelemetry-instrumented http.RoundTripper for
// tracing outgoing HTTP requests (e.g. direct LLM API calls) as client spans
// that carry Otellix attribution from the request context.
//
// Usage:
//
//	client := &http.Client{Transport: nethttp.NewTransport(nil)}
//	req := req.WithContext(otellix.ContextWithUser(ctx, "usr_123"))
//	resp, err := client.Do(req)
package nethttp

import (
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/oluwajubelo1/otellix/integrations/nethttp"

// Transport wraps an http.RoundTripper and creates a client span for every
// request, inheriting any active span and Otellix attribution from the context.
type Transport struct {
	base http.RoundTripper
}

// NewTransport returns a Transport wrapping base. If base is nil,
// http.DefaultTransport is used.
func NewTransport(base http.RoundTripper) *Transport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &Transport{base: base}
}

// RoundTrip instruments the wrapped RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, span := otel.Tracer(tracerName).Start(
		req.Context(),
		"llm.http."+req.Method,
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	span.SetAttributes(
		attribute.String("http.method", req.Method),
		attribute.String("http.url", req.URL.String()),
		attribute.String("http.target", req.URL.Path),
	)

	resp, err := t.base.RoundTrip(req.WithContext(ctx))
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return resp, err
	}
	span.SetAttributes(attribute.Int("http.status_code", resp.StatusCode))
	if resp.StatusCode >= 400 {
		span.SetStatus(codes.Error, resp.Status)
	} else {
		span.SetStatus(codes.Ok, "")
	}
	return resp, nil
}
