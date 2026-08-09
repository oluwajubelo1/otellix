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
	callCtx  context.Context
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

	// Streaming performance tracking
	firstTokenAt time.Time
	tokenCount   int64
	maxTokens    int

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

	// Wrap the provider with automatic retries if configured.
	if cfg.RetryConfig != nil {
		provider = RetryProvider(provider, cfg.RetryConfig)
	}

	var enforcer *BudgetEnforcer
	if cfg.BudgetConfig != nil {
		enforcer = NewBudgetEnforcer(cfg.BudgetConfig)
		// Basic check before starting stream
		allowed, action := enforcer.Check(ctx, cfg.UserID, cfg.ProjectID, 0) // No estimate yet
		if !allowed {
			switch action {
			case FallbackBlock:
				berr := enforcer.BudgetError(cfg.UserID, cfg.ProjectID)
				enforcer.NotifyBlocked(ctx, berr)
				return nil, berr
			case FallbackDowngrade:
				fallbackModel := cfg.BudgetConfig.FallbackModel
				if fallbackModel == "" {
					fallbackModel = cfg.FallbackModel
				}
				if fallbackModel == "" {
					if routed, err := SelectCheapestModel(cfg.Provider, cfg.Model, 500, enforcer.Remaining(cfg.UserID, cfg.ProjectID), true); err == nil {
						fallbackModel = routed
					}
				}
				if fallbackModel != "" {
					cfg.Model = fallbackModel
					params.Model = fallbackModel
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

	span.SetAttributes(baseSpanAttributes(cfg, genAIOpChat)...)
	span.SetAttributes(requestAttributes(params)...)

	for k, v := range cfg.Attributes {
		span.SetAttributes(attribute.String(k, v))
	}

	if cfg.EnablePromptFingerprint && cfg.PromptText != "" {
		fingerprint := promptFingerprint(cfg.PromptText)
		span.SetAttributes(attribute.String("llm.prompt_fingerprint", fingerprint))
	}

	start := time.Now()
	callCtx, cancel := callContext(ctx, cfg.Timeout)
	defer cancel()
	baseStream, err := provider.Stream(callCtx, params)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.SetAttributes(attribute.Bool("llm.error", true))
		span.End()
		return nil, err
	}

	return &Stream{
		base:      baseStream,
		ctx:       ctx,
		callCtx:   callCtx,
		span:      span,
		cfg:       cfg,
		enforcer:  enforcer,
		provider:  cfg.Provider,
		model:     cfg.Model,
		start:     start,
		maxTokens: params.MaxTokens,
	}, nil
}

// Recv returns the next token from the stream and updates budget/telemetry.
func (s *Stream) Recv() (providers.StreamEvent, error) {
	if err := s.callCtx.Err(); err != nil {
		s.Close()
		return providers.StreamEvent{}, err
	}
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

	// Track time-to-first-token and token throughput.
	if evt.Token != "" {
		if s.firstTokenAt.IsZero() {
			s.firstTokenAt = time.Now()
		}
		s.tokenCount++
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

	// Streaming performance metrics.
	var ttftMs, tokensPerSec float64
	if !s.firstTokenAt.IsZero() {
		ttftMs = float64(s.firstTokenAt.Sub(s.start).Milliseconds())
	}
	if latencyMs > 0 && s.outputTokens > 0 {
		tokensPerSec = float64(s.outputTokens) / (latencyMs / 1000.0)
	}

	result := providers.CallResult{
		InputTokens:  s.inputTokens,
		OutputTokens: s.outputTokens,
	}

	// Finalize OTel Span (dual llm.* / gen_ai.* emission).
	s.span.SetStatus(codes.Ok, "")
	s.span.SetAttributes(usageAttributes(&result, latencyMs, s.costUSD)...)
	s.span.SetAttributes(
		attribute.Float64("llm.time_to_first_token_ms", ttftMs),
		attribute.Float64("llm.tokens_per_second", tokensPerSec),
		attribute.String(genAIResponseModel, s.model),
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
	if ttftMs > 0 {
		ttftHist.Record(s.ctx, ttftMs, metricAttrs)
	}
	if tokensPerSec > 0 {
		tokensPerSecHist.Record(s.ctx, tokensPerSec, metricAttrs)
	}
	recordContextUtilization(s.ctx, result, s.maxTokens, metricAttrs)
	recordCacheRoi(s.ctx, s.provider, s.model, result, metricAttrs)

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
