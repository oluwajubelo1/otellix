// Package oteltest provides testing helpers for asserting on the spans Otellix
// emits, using an in-memory OTel span exporter. It is intended for use in your
// own tests.
//
// Usage:
//
//	h := oteltest.New()
//	defer h.Close()
//
//	otellix.Trace(ctx, mock, params, otellix.WithProvider("openai"), otellix.WithModel("gpt-4o"))
//
//	h.AssertSpanCount(t, 1)
//	h.AssertAttribute(t, "llm.call", "llm.cost_usd", 0.00015)
package oteltest

import (
	"context"
	"math"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// Harness captures spans in memory and offers assertion helpers.
type Harness struct {
	exporter *tracetest.InMemoryExporter
	provider *sdktrace.TracerProvider

	prevProvider trace.TracerProvider
}

// New creates a Harness and installs an in-memory TracerProvider as the global
// OTel provider. Call Close() when done to restore the previous provider.
func New() *Harness {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	return &Harness{exporter: exporter, provider: provider, prevProvider: prev}
}

// Close restores the previously installed TracerProvider.
func (h *Harness) Close() {
	_ = h.provider.Shutdown(context.Background())
	otel.SetTracerProvider(h.prevProvider)
}

// Reset clears all captured spans.
func (h *Harness) Reset() { h.exporter.Reset() }

// Spans returns the captured spans.
func (h *Harness) Spans() []tracetest.SpanStub { return h.exporter.GetSpans() }

// SpanCount returns the number of captured spans.
func (h *Harness) SpanCount() int { return len(h.exporter.GetSpans()) }

// SpanByName returns the first span with the given name.
func (h *Harness) SpanByName(name string) (tracetest.SpanStub, bool) {
	for _, s := range h.exporter.GetSpans() {
		if s.Name == name {
			return s, true
		}
	}
	return tracetest.SpanStub{}, false
}

// Attribute returns the value of the given attribute on the first span with the
// given name.
func (h *Harness) Attribute(spanName, key string) (attribute.Value, bool) {
	s, ok := h.SpanByName(spanName)
	if !ok {
		return attribute.Value{}, false
	}
	for _, kv := range s.Attributes {
		if string(kv.Key) == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

// AssertSpanCount fails the test if the number of captured spans differs.
func (h *Harness) AssertSpanCount(t *testing.T, want int) {
	t.Helper()
	if got := h.SpanCount(); got != want {
		t.Errorf("span count = %d, want %d", got, want)
	}
}

// AssertAttribute fails the test if the span's attribute does not match want.
func (h *Harness) AssertAttribute(t *testing.T, spanName, key string, want interface{}) {
	t.Helper()
	v, ok := h.Attribute(spanName, key)
	if !ok {
		t.Errorf("span %q missing attribute %q", spanName, key)
		return
	}
	switch w := want.(type) {
	case string:
		if got := v.AsString(); got != w {
			t.Errorf("attribute %s = %q, want %q", key, got, w)
		}
	case int64:
		if got := v.AsInt64(); got != w {
			t.Errorf("attribute %s = %d, want %d", key, got, w)
		}
	case int:
		if got := v.AsInt64(); got != int64(w) {
			t.Errorf("attribute %s = %d, want %d", key, got, w)
		}
	case float64:
		if got := v.AsFloat64(); math.Abs(got-w) > 1e-9 {
			t.Errorf("attribute %s = %f, want %f", key, got, w)
		}
	case bool:
		if got := v.AsBool(); got != w {
			t.Errorf("attribute %s = %v, want %v", key, got, w)
		}
	default:
		t.Errorf("unsupported want type %T for attribute %s", want, key)
	}
}
