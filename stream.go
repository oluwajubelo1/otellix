package otellix

import (
	"context"
	"errors"
	"io"
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
	base     providers.Stream
	ctx      context.Context
	span     trace.Span
	cfg      *Config
	enforcer *BudgetEnforcer
	pricing  PricingEntry

	// Budget holds for the stream's cost, and how much cost they cover. For
	// a store that can't hold spend, covered counts cost that passed a check.
	holds   []*Reservation
	covered float64
	// overLimit is set once the stream couldn't cover its cost but carried
	// on, under FallbackNotify or FallbackDowngrade.
	overLimit bool

	// Aggregated metrics
	inputTokens      int64
	outputTokens     int64
	cacheReadTokens  int64
	cacheWriteTokens int64
	costUSD          float64
	start            time.Time

	// err is the error the stream ended with, other than io.EOF.
	err    error
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
	var covered float64
	if cfg.BudgetConfig != nil {
		enforcer = NewBudgetEnforcer(cfg.BudgetConfig)
		if err := cfg.unverifiedIdentityError(); err != nil {
			return nil, err
		}
		if err := cfg.unpricedError(); err != nil {
			return nil, err
		}

		// Hold the estimated cost until the stream is closed. Recv still
		// cuts the stream off if it runs past the budget.
		estimate := cfg.estimateCost(params)
		var allowed bool
		reservation, allowed = enforcer.Reserve(ctx, cfg.UserID, cfg.ProjectID, estimate)
		if allowed {
			covered = estimate
		} else {
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
	cfg.setIdentityAttributes(span)

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
		base:     baseStream,
		ctx:      ctx,
		span:     span,
		cfg:      cfg,
		enforcer: enforcer,
		pricing:  pricing,
		holds:    holdsOf(reservation),
		covered:  covered,
		start:    start,
	}, nil
}

// holdsOf returns the holds list for a stream's first reservation.
func holdsOf(r *Reservation) []*Reservation {
	if r == nil {
		return nil
	}
	return []*Reservation{r}
}

// Recv returns the next token from the stream and updates budget/telemetry.
func (s *Stream) Recv() (providers.StreamEvent, error) {
	evt, err := s.base.Recv()
	if err != nil {
		// Finish the stream here, so its cost is recorded even if the
		// caller stops at io.EOF without calling Close.
		if !errors.Is(err, io.EOF) {
			s.err = err
		}
		_ = s.Close()
		return evt, err
	}

	// Mid-stream checks
	if evt.InputTokens > 0 {
		s.inputTokens = evt.InputTokens
	}
	if evt.CacheReadTokens > 0 {
		s.cacheReadTokens = evt.CacheReadTokens
	}
	if evt.CacheWriteTokens > 0 {
		s.cacheWriteTokens = evt.CacheWriteTokens
	}
	if evt.OutputTokens > 0 {
		s.outputTokens = evt.OutputTokens
	} else if evt.Token != "" {
		// Naive estimate: 1 word token is roughly 1 output token, but let's just increment by 1
		// Usually providers send final usage. So this acts as an estimate.
		s.outputTokens++
	}

	// Update cost progressively
	s.costUSD = s.pricing.cost(s.result())

	if s.enforcer != nil && !s.overLimit && !s.cover() {
		if s.cfg.BudgetConfig.FallbackAction == FallbackBlock {
			s.span.AddEvent("budget.aborted")
			_ = s.Close() // the budget error is what the caller needs
			return evt, s.enforcer.BudgetError(s.cfg.UserID, s.cfg.ProjectID)
		}
		s.overLimit = true
	}

	return evt, nil
}

// result returns the tokens streamed so far.
func (s *Stream) result() providers.CallResult {
	return providers.CallResult{
		InputTokens:      s.inputTokens,
		OutputTokens:     s.outputTokens,
		CacheReadTokens:  s.cacheReadTokens,
		CacheWriteTokens: s.cacheWriteTokens,
	}
}

// cover makes sure the stream's cost so far is held against its budgets.
// When the stream runs past what it holds, it reserves more: as much again
// as it already covers, so a long stream makes few store calls, or just
// what it needs when that won't fit. Every cost is held before the stream
// goes on, so streams running at the same time can't together overshoot a
// limit. It returns false when even the cost so far doesn't fit.
func (s *Stream) cover() bool {
	need := s.costUSD - s.covered
	if need <= 0 {
		return true
	}
	amounts := []float64{need}
	if s.covered > need {
		amounts = []float64{s.covered, need}
	}
	for _, amount := range amounts {
		if res, ok := s.enforcer.Reserve(s.ctx, s.cfg.UserID, s.cfg.ProjectID, amount); ok {
			if res != nil {
				s.holds = append(s.holds, res)
			}
			s.covered += amount
			return true
		}
	}
	return false
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
	if s.err != nil {
		s.span.SetStatus(codes.Error, s.err.Error())
		s.span.SetAttributes(attribute.Bool("llm.error", true), attribute.String("llm.error_message", s.err.Error()))
	} else {
		s.span.SetStatus(codes.Ok, "")
	}
	s.span.SetAttributes(
		attribute.Int64("llm.input_tokens", s.inputTokens),
		attribute.Int64("llm.output_tokens", s.outputTokens),
		attribute.Int64("llm.cache_read_tokens", s.cacheReadTokens),
		attribute.Int64("llm.cache_write_tokens", s.cacheWriteTokens),
		attribute.Float64("llm.latency_ms", latencyMs),
		attribute.Float64("llm.cost_usd", s.costUSD),
	)

	metricAttrs := metricAttributes(s.cfg)

	inputTokensCount.Add(s.ctx, s.inputTokens, metricAttrs)
	outputTokensCount.Add(s.ctx, s.outputTokens, metricAttrs)
	cacheReadCount.Add(s.ctx, s.cacheReadTokens, metricAttrs)
	cacheWriteCount.Add(s.ctx, s.cacheWriteTokens, metricAttrs)
	costCounter.Add(s.ctx, s.costUSD, metricAttrs)
	latencyHist.Record(s.ctx, latencyMs, metricAttrs)

	if s.enforcer != nil {
		s.enforcer.Record(s.ctx, s.cfg.UserID, s.cfg.ProjectID, s.costUSD)
		for _, h := range s.holds {
			h.Release(s.ctx)
		}
	}

	if devPrinter != nil {
		res := s.result()
		devPrinter(s.cfg, &res, s.costUSD, latencyMs, s.enforcer)
	}

	s.span.End()
	return err
}
