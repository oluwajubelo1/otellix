package otellix

import (
	"context"
	"time"

	"github.com/oluwajubelo1/otellix/providers"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Stream wraps a provider stream to add OpenTelemetry tracing, budget constraints,
// and cost aggregations over the streamed responses.
type Stream struct {
	base        providers.Stream
	ctx         context.Context
	span        trace.Span
	cfg         *Config
	enforcer    *BudgetEnforcer
	reservation *Reservation
	pricing     PricingEntry

	// Aggregated metrics
	inputTokens  int64
	outputTokens int64
	costUSD      float64
	start        time.Time

	closed bool
}

// TraceStream acts like Trace, but streams the response back token by token.
func TraceStream(ctx context.Context, provider providers.Provider, params providers.CallParams, opts ...Option) (*Stream, error) {
	initMetrics()

	cfg := NewConfig(opts...)

	if cfg.Provider == "" {
		cfg.Provider = provider.Name()
	}
	if cfg.Model == "" {
		cfg.Model = params.Model
	}

	// Automatic identity extraction from context (middleware support).
	cfg.fillAttributionFromContext(ctx)

	var enforcer *BudgetEnforcer
	var reservation *Reservation
	if cfg.BudgetConfig != nil {
		enforcer = NewBudgetEnforcer(cfg.BudgetConfig)
		if err := cfg.unpricedError(); err != nil {
			return nil, err
		}

		// Hold the estimated cost until the stream is closed. Recv still
		// cuts the stream off if it runs past the budget.
		var allowed bool
		reservation, allowed = enforcer.Reserve(ctx, cfg.UserID, cfg.ProjectID, cfg.estimateCost(params))
		if !allowed {
			switch cfg.BudgetConfig.FallbackAction {
			case FallbackBlock:
				return nil, enforcer.BudgetError(cfg.UserID, cfg.ProjectID)
			case FallbackDowngrade:
				if cfg.downgrade(&params) {
					if err := cfg.unpricedError(); err != nil {
						return nil, err
					}
				}
			}
		}
	}

	spanName := cfg.SpanName
	if spanName == "" {
		spanName = defaultSpanName + ".stream"
	}

	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindClient))

	span.SetAttributes(
		attribute.String("llm.provider", cfg.Provider),
		attribute.String("llm.model", cfg.Model),
		attribute.String("llm.feature_id", cfg.FeatureID),
		attribute.String("llm.user_id", cfg.UserID),
		attribute.String("llm.project_id", cfg.ProjectID),
		attribute.Bool("llm.user_attributed", cfg.UserID != ""),
	)

	for k, v := range cfg.Attributes {
		span.SetAttributes(attribute.String(k, v))
	}

	if cfg.EnablePromptFingerprint && cfg.PromptText != "" {
		fingerprint := promptFingerprint(cfg.PromptText)
		span.SetAttributes(attribute.String("llm.prompt_fingerprint", fingerprint))
	}

	cfg.flagUnpriced(ctx, span)
	pricing, _ := cfg.pricing()

	start := time.Now()
	baseStream, err := provider.Stream(ctx, params)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.SetAttributes(attribute.Bool("llm.error", true))
		span.End()
		reservation.Release(ctx)
		return nil, err
	}

	return &Stream{
		base:        baseStream,
		ctx:         ctx,
		span:        span,
		cfg:         cfg,
		enforcer:    enforcer,
		reservation: reservation,
		pricing:     pricing,
		start:       start,
	}, nil
}

// Recv returns the next token from the stream and updates budget/telemetry.
func (s *Stream) Recv() (providers.StreamEvent, error) {
	evt, err := s.base.Recv()
	if err != nil {
		return evt, err
	}

	// Mid-stream checks
	if evt.InputTokens > 0 {
		s.inputTokens = evt.InputTokens
	}
	if evt.OutputTokens > 0 {
		s.outputTokens = evt.OutputTokens
	} else if evt.Token != "" {
		// Naive estimate: 1 word token is roughly 1 output token, but let's just increment by 1
		// Usually providers send final usage. So this acts as an estimate.
		s.outputTokens++
	}

	// Update cost progressively
	s.costUSD = s.pricing.cost(providers.CallResult{
		InputTokens:  s.inputTokens,
		OutputTokens: s.outputTokens,
	})

	if s.enforcer != nil {
		// Spend already includes this stream's own hold, so check only the
		// cost beyond it.
		allowed, action := s.enforcer.Check(s.ctx, s.cfg.UserID, s.cfg.ProjectID, s.costUSD-s.reservation.held())
		if !allowed && action == FallbackBlock {
			s.span.AddEvent("budget.aborted")
			_ = s.Close() // the budget error is what the caller needs
			return evt, s.enforcer.BudgetError(s.cfg.UserID, s.cfg.ProjectID)
		}
	}

	return evt, nil
}

// Close finalizes the stream, computes total metrics, and ends the trace span.
func (s *Stream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true

	err := s.base.Close()
	latencyMs := float64(time.Since(s.start).Milliseconds())

	// Finalize OTel Span
	s.span.SetStatus(codes.Ok, "")
	s.span.SetAttributes(
		attribute.Int64("llm.input_tokens", s.inputTokens),
		attribute.Int64("llm.output_tokens", s.outputTokens),
		attribute.Float64("llm.latency_ms", latencyMs),
		attribute.Float64("llm.cost_usd", s.costUSD),
	)

	metricAttrs := metricAttributes(s.cfg)

	inputTokensCount.Add(s.ctx, s.inputTokens, metricAttrs)
	outputTokensCount.Add(s.ctx, s.outputTokens, metricAttrs)
	costCounter.Add(s.ctx, s.costUSD, metricAttrs)
	latencyHist.Record(s.ctx, latencyMs, metricAttrs)

	if s.enforcer != nil {
		s.enforcer.Record(s.ctx, s.cfg.UserID, s.cfg.ProjectID, s.costUSD)
		s.reservation.Release(s.ctx)
	}

	if devPrinter != nil {
		res := providers.CallResult{
			InputTokens:  s.inputTokens,
			OutputTokens: s.outputTokens,
		}
		devPrinter(s.cfg, &res, s.costUSD, latencyMs, s.enforcer)
	}

	s.span.End()
	return err
}
