package otellix

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"github.com/oluwajubelo1/otellix/providers"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	tracerName      = "github.com/oluwajubelo1/otellix"
	defaultSpanName = "llm.call"
)

// Metrics instruments — initialised once via sync.Once.
var (
	meterOnce         sync.Once
	inputTokensCount  metric.Int64Counter
	outputTokensCount metric.Int64Counter
	costCounter       metric.Float64Counter
	latencyHist       metric.Float64Histogram
	errorsCount       metric.Int64Counter
	cacheReadCount    metric.Int64Counter
	cacheWriteCount   metric.Int64Counter
)

// initMetrics creates OTel metric instruments. Safe to call multiple times.
func initMetrics() {
	meterOnce.Do(func() {
		meter := otel.Meter(tracerName)

		inputTokensCount, _ = meter.Int64Counter("otellix.llm.tokens.input",
			metric.WithDescription("Total input tokens sent to LLM providers"),
			metric.WithUnit("{token}"),
		)
		outputTokensCount, _ = meter.Int64Counter("otellix.llm.tokens.output",
			metric.WithDescription("Total output tokens received from LLM providers"),
			metric.WithUnit("{token}"),
		)
		costCounter, _ = meter.Float64Counter("otellix.llm.cost.usd",
			metric.WithDescription("Total cost of LLM calls in USD"),
			metric.WithUnit("USD"),
		)
		latencyHist, _ = meter.Float64Histogram("otellix.llm.latency.ms",
			metric.WithDescription("Latency of LLM calls in milliseconds"),
			metric.WithUnit("ms"),
			metric.WithExplicitBucketBoundaries(50, 100, 250, 500, 1000, 2500, 5000, 10000, 30000),
		)
		errorsCount, _ = meter.Int64Counter("otellix.llm.errors.total",
			metric.WithDescription("Total number of LLM call errors"),
		)
		cacheReadCount, _ = meter.Int64Counter("otellix.llm.tokens.cache_read",
			metric.WithDescription("Total tokens read from cache (hits)"),
			metric.WithUnit("{token}"),
		)
		cacheWriteCount, _ = meter.Int64Counter("otellix.llm.tokens.cache_write",
			metric.WithDescription("Total tokens used to create/update cache"),
			metric.WithUnit("{token}"),
		)
	})
}

// Trace executes an LLM call through the given provider, wrapping it with full
// OpenTelemetry tracing, cost attribution, and optional budget enforcement.
//
// Usage:
//
//	result, err := otellix.Trace(ctx, provider, params,
//	    otellix.WithModel("claude-sonnet-4-6"),
//	    otellix.WithFeatureID("chat"),
//	    otellix.WithUserID("usr_123"),
//	)
func Trace(ctx context.Context, provider providers.Provider, params providers.CallParams, opts ...Option) (providers.CallResult, error) {
	initMetrics()

	cfg := NewConfig(opts...)

	// Fill in provider name from the provider if not set via options.
	if cfg.Provider == "" {
		cfg.Provider = provider.Name()
	}
	// Fill in model from params if not set via options.
	if cfg.Model == "" {
		cfg.Model = params.Model
	}

	// Automatic identity extraction from context (middleware support).
	if cfg.UserID == "" {
		cfg.UserID = UserFromContext(ctx)
	}
	if cfg.ProjectID == "" {
		cfg.ProjectID = ProjectFromContext(ctx)
	}

	var enforcer *BudgetEnforcer
	if cfg.BudgetConfig != nil {
		enforcer = NewBudgetEnforcer(cfg.BudgetConfig)
		estimatedCost := EstimateCost(cfg.Provider, cfg.Model, 500) // assume ~500 output tokens
		allowed, action := enforcer.Check(ctx, cfg.UserID, cfg.ProjectID, estimatedCost)

		if !allowed {
			switch action {
			case FallbackBlock:
				return providers.CallResult{}, enforcer.BudgetError(cfg.UserID, cfg.ProjectID)
			case FallbackDowngrade:
				if cfg.FallbackModel != "" {
					cfg.Model = cfg.FallbackModel
					params.Model = cfg.FallbackModel
				}
				// FallbackNotify: proceed, we'll add a warning event after the call.
			}
		}
	}

	// Dynamic Prompt Optimization: allow custom decorators to modify params based on budget status.
	if cfg.PromptDecorator != nil {
		status := BudgetStatus{Mode: FallbackNotify}
		if enforcer != nil {
			status = enforcer.Status(ctx, cfg.UserID, cfg.ProjectID)
		}
		cfg.PromptDecorator(ctx, status, &params)
	}

	// Hook 1: Dedup cache check — return cached result immediately if available.
	if cfg.CacheConfig != nil {
		cacheKey := makeCacheKey(cfg.Provider, cfg.Model, params)
		if cached, ok := cfg.CacheConfig.Store.Get(ctx, cacheKey); ok {
			return cached, nil
		}
	}

	// Hook 2: Pre-call anomaly block check.
	if cfg.AnomalyConfig != nil && cfg.AnomalyConfig.Action == AnomalyBlock {
		estimated := EstimateCost(cfg.Provider, cfg.Model, 500)
		if detection, isAnomaly := checkAnomaly(cfg, estimated); isAnomaly {
			if cfg.AnomalyConfig.OnAnomaly != nil {
				cfg.AnomalyConfig.OnAnomaly(ctx, detection)
			}
			return providers.CallResult{}, &AnomalyBlockedError{Detection: detection}
		}
	}

	spanName := cfg.SpanName
	if spanName == "" {
		spanName = defaultSpanName
	}

	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

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
	result, err := provider.Call(ctx, params)
	latencyMs := float64(time.Since(start).Milliseconds())

	// Metric labels shared by all instruments.
	metricAttrs := metric.WithAttributes(
		attribute.String("provider", cfg.Provider),
		attribute.String("model", cfg.Model),
		attribute.String("feature_id", cfg.FeatureID),
		attribute.String("user_id", cfg.UserID),
		attribute.String("project_id", cfg.ProjectID),
	)

	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.SetAttributes(attribute.Bool("llm.error", true))
		span.SetAttributes(attribute.String("llm.error_message", err.Error()))

		errType := "unknown"
		switch err.(type) {
		case *providers.RateLimitError:
			errType = "rate_limit"
		case *providers.TimeoutError:
			errType = "timeout"
		}
		errorsCount.Add(ctx, 1, metric.WithAttributes(
			attribute.String("provider", cfg.Provider),
			attribute.String("model", cfg.Model),
			attribute.String("error_type", errType),
		))

		latencyHist.Record(ctx, latencyMs, metricAttrs)
		return result, err
	}

	span.SetStatus(codes.Ok, "")
	span.SetAttributes(
		attribute.Int64("llm.input_tokens", result.InputTokens),
		attribute.Int64("llm.output_tokens", result.OutputTokens),
		attribute.Int64("llm.cache_read_tokens", result.CacheReadTokens),
		attribute.Int64("llm.cache_write_tokens", result.CacheWriteTokens),
		attribute.Float64("llm.latency_ms", latencyMs),
	)

	costUSD := CalculateCost(cfg.Provider, cfg.Model, result)
	span.SetAttributes(attribute.Float64("llm.cost_usd", costUSD))

	inputTokensCount.Add(ctx, result.InputTokens, metricAttrs)
	outputTokensCount.Add(ctx, result.OutputTokens, metricAttrs)
	cacheReadCount.Add(ctx, result.CacheReadTokens, metricAttrs)
	cacheWriteCount.Add(ctx, result.CacheWriteTokens, metricAttrs)
	costCounter.Add(ctx, costUSD, metricAttrs)
	latencyHist.Record(ctx, latencyMs, metricAttrs)

	// Hook 3: Cache result in dedup store + record to analytics tracker.
	if cfg.CacheConfig != nil {
		cacheKey := makeCacheKey(cfg.Provider, cfg.Model, params)
		cfg.CacheConfig.Store.Set(ctx, cacheKey, result, cfg.CacheConfig.TTL)
	}
	recordCall(CallRecord{
		Timestamp:        time.Now(),
		Provider:         cfg.Provider,
		Model:            cfg.Model,
		FeatureID:        cfg.FeatureID,
		UserID:           cfg.UserID,
		ProjectID:        cfg.ProjectID,
		InputTokens:      result.InputTokens,
		OutputTokens:     result.OutputTokens,
		CacheReadTokens:  result.CacheReadTokens,
		CacheWriteTokens: result.CacheWriteTokens,
		CostUSD:          costUSD,
		LatencyMs:        latencyMs,
	})

	if enforcer != nil {
		enforcer.Record(ctx, cfg.UserID, cfg.ProjectID, costUSD)

		// If budget was exceeded but action was FallbackNotify, add a warning event.
		remaining := enforcer.Remaining(cfg.UserID, cfg.ProjectID)
		if remaining <= 0 {
			span.AddEvent("budget.warning", trace.WithAttributes(
				attribute.String("llm.user_id", cfg.UserID),
				attribute.String("llm.project_id", cfg.ProjectID),
				attribute.Float64("llm.budget_remaining", remaining),
			))
		}
	}

	// Hook 4: Post-call anomaly logging.
	if cfg.AnomalyConfig != nil && cfg.AnomalyConfig.Action == AnomalyLog {
		if detection, isAnomaly := checkAnomaly(cfg, costUSD); isAnomaly {
			span.AddEvent("llm.anomaly", trace.WithAttributes(
				attribute.String("llm.anomaly.key", detection.Key),
				attribute.Float64("llm.anomaly.current_cost", detection.CurrentCost),
				attribute.Float64("llm.anomaly.average_cost", detection.AverageCost),
				attribute.Float64("llm.anomaly.multiplier", detection.Multiplier),
				attribute.Float64("llm.anomaly.threshold", detection.Threshold),
			))
			if cfg.AnomalyConfig.OnAnomaly != nil {
				cfg.AnomalyConfig.OnAnomaly(ctx, detection)
			}
		}
	}

	// Stdout dev exporter callback (if registered).
	if devPrinter != nil {
		devPrinter(cfg, &result, costUSD, latencyMs, enforcer)
	}

	return result, nil
}

// promptFingerprint generates a short hash of the prompt for drift detection.
// Returns the first 8 hex characters of the SHA256 hash.
func promptFingerprint(text string) string {
	h := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%x", h[:4])
}
