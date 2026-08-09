// Package slogbridge provides a log/slog.Handler that decorates every log
// record with the active OpenTelemetry span context (trace_id, span_id) and
// Otellix attribution (llm.user_id, llm.project_id) pulled from the record's
// context — so logs correlate with LLM spans without manual plumbing.
//
// Usage:
//
//	logger := slog.New(slogbridge.NewHandler(slog.NewTextHandler(os.Stdout, nil)))
//	slog.SetDefault(logger)
package slogbridge

import (
	"context"
	"log/slog"

	"github.com/oluwajubelo1/otellix"
	"go.opentelemetry.io/otel/trace"
)

// Handler wraps another slog.Handler and enriches records with span context.
type Handler struct {
	next slog.Handler
}

// NewHandler returns a slog.Handler that decorates the given handler with OTel
// span context and Otellix attribution attributes.
func NewHandler(next slog.Handler) slog.Handler {
	return &Handler{next: next}
}

func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() && sc.IsSampled() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	if u := otellix.UserFromContext(ctx); u != "" {
		r.AddAttrs(slog.String("llm.user_id", u))
	}
	if p := otellix.ProjectFromContext(ctx); p != "" {
		r.AddAttrs(slog.String("llm.project_id", p))
	}
	return h.next.Handle(ctx, r)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{next: h.next.WithAttrs(attrs)}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{next: h.next.WithGroup(name)}
}
