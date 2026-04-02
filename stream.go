package otellix

import (
	"context"
	"time"

	"github.com/oluwajubelo1/otellix/providers"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Stream wraps a provider stream to add OpenTelemetry tracing, budget constraints,
// and cost aggregations over the streamed responses.
type Stream struct {
	base     providers.Stream
	ctx      context.Context
	span     trace.Span
	cfg      *Config
	enforcer *BudgetEnforcer
	provider string
	model    string

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

	var enforcer *BudgetEnforcer
	if cfg.BudgetConfig != nil {
		enforcer = NewBudgetEnforcer(cfg.BudgetConfig)
		// Basic check before starting stream
		allowed, action := enforcer.Check(ctx, cfg.UserID, cfg.ProjectID, 0) // No estimate yet
		if !allowed {
			switch action {
			case FallbackBlock:
				return nil, enforcer.BudgetError(cfg.UserID, cfg.ProjectID)
			case FallbackDowngrade:
				if cfg.FallbackModel != "" {
					cfg.Model = cfg.FallbackModel
					params.Model = cfg.FallbackModel
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
	)

	for k, v := range cfg.Attributes {
		span.SetAttributes(attribute.String(k, v))
	}

	if cfg.EnablePromptFingerprint && cfg.PromptText != "" {
		fingerprint := promptFingerprint(cfg.PromptText)
		span.SetAttributes(attribute.String("llm.prompt_fingerprint", fingerprint))
	}

	start := time.Now()
	baseStream, err := provider.Stream(ctx, params)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.SetAttributes(attribute.Bool("llm.error", true))
		span.End()
		return nil, err
	}

	return &Stream{
		base:     baseStream,
		ctx:      ctx,
		span:     span,
		cfg:      cfg,
		enforcer: enforcer,
		provider: cfg.Provider,
		model:    cfg.Model,
		start:    start,
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
	costUSD := CalculateCost(s.provider, s.model, providers.CallResult{
		InputTokens:  s.inputTokens,
		OutputTokens: s.outputTokens,
	})

	s.costUSD = costUSD

	if s.enforcer != nil {
		allowed, action := s.enforcer.Check(s.ctx, s.cfg.UserID, s.cfg.ProjectID, costUSD)
		if !allowed && action == FallbackBlock {
			s.span.AddEvent("budget.aborted")
			s.Close()
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

	metricAttrs := metric.WithAttributes(
		attribute.String("provider", s.provider),
		attribute.String("model", s.model),
		attribute.String("feature_id", s.cfg.FeatureID),
		attribute.String("user_id", s.cfg.UserID),
		attribute.String("project_id", s.cfg.ProjectID),
	)

	inputTokensCount.Add(s.ctx, s.inputTokens, metricAttrs)
	outputTokensCount.Add(s.ctx, s.outputTokens, metricAttrs)
	costCounter.Add(s.ctx, s.costUSD, metricAttrs)
	latencyHist.Record(s.ctx, latencyMs, metricAttrs)

	if s.enforcer != nil {
		s.enforcer.Record(s.ctx, s.cfg.UserID, s.cfg.ProjectID, s.costUSD)
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
