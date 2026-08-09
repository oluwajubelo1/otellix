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

// EmbeddingParams holds the inputs for an embedding generation call.
type EmbeddingParams struct {
	// Model is the embedding model identifier (e.g. "text-embedding-3-small").
	Model string

	// Input is the list of texts to embed.
	Input []string
}

// EmbeddingResult holds the standardized embedding response.
type EmbeddingResult struct {
	// InputTokens is the total number of tokens consumed by the input texts.
	InputTokens int64

	// Model is the embedding model actually used.
	Model string

	// Embeddings is the resulting vectors, one per input text.
	Embeddings [][]float32

	// Dimensions is the dimensionality of the returned vectors.
	Dimensions int

	// RawResponse holds the provider's raw response for inspection.
	RawResponse interface{}
}

// EmbeddingProvider is the interface that embedding providers implement.
type EmbeddingProvider interface {
	// Embed generates embeddings for the given inputs.
	Embed(ctx context.Context, params EmbeddingParams) (EmbeddingResult, error)

	// Name returns the provider identifier (e.g. "openai").
	Name() string
}

// TraceEmbedding wraps an embedding call with OpenTelemetry tracing and cost
// attribution. Embeddings are billed on input tokens only, so costs are
// computed with CalculateEmbeddingCost.
//
// Usage:
//
//	result, err := otellix.TraceEmbedding(ctx, provider, otellix.EmbeddingParams{
//	    Model: "text-embedding-3-small",
//	    Input: []string{"hello world"},
//	}, otellix.WithUserID("usr_123"))
func TraceEmbedding(ctx context.Context, provider EmbeddingProvider, params EmbeddingParams, opts ...Option) (EmbeddingResult, error) {
	initMetrics()

	cfg := NewConfig(opts...)
	if cfg.Provider == "" {
		cfg.Provider = provider.Name()
	}
	if cfg.Model == "" {
		cfg.Model = params.Model
	}
	if cfg.UserID == "" {
		cfg.UserID = UserFromContext(ctx)
	}
	if cfg.ProjectID == "" {
		cfg.ProjectID = ProjectFromContext(ctx)
	}

	tracer := otel.Tracer(tracerName)
	spanName := cfg.SpanName
	if spanName == "" {
		spanName = "llm.embeddings"
	}
	ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	span.SetAttributes(baseSpanAttributes(cfg, genAIOpEmbeddings)...)
	for k, v := range cfg.Attributes {
		span.SetAttributes(attribute.String(k, v))
	}

	start := time.Now()
	callCtx, cancel := callContext(ctx, cfg.Timeout)
	defer cancel()
	result, err := provider.Embed(callCtx, params)
	latencyMs := float64(time.Since(start).Milliseconds())

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
		errorsCount.Add(ctx, 1, metric.WithAttributes(
			attribute.String("provider", cfg.Provider),
			attribute.String("model", cfg.Model),
			attribute.String("error_type", "embeddings"),
		))
		latencyHist.Record(ctx, latencyMs, metricAttrs)
		return result, err
	}

	costUSD := CalculateEmbeddingCost(cfg.Provider, cfg.Model, result.InputTokens)
	span.SetStatus(codes.Ok, "")
	span.SetAttributes(
		attribute.Int64("llm.input_tokens", result.InputTokens),
		attribute.Float64("llm.latency_ms", latencyMs),
		attribute.Float64("llm.cost_usd", costUSD),
		attribute.Int("llm.embeddings_count", len(result.Embeddings)),
		attribute.Int("llm.embeddings_dimensions", result.Dimensions),
		attribute.Int64(genAIUsageInput, result.InputTokens),
		attribute.Int("gen_ai.usage.output_embeddings", len(result.Embeddings)),
	)
	if result.Model != "" {
		span.SetAttributes(attribute.String(genAIResponseModel, result.Model))
	}

	inputTokensCount.Add(ctx, result.InputTokens, metricAttrs)
	costCounter.Add(ctx, costUSD, metricAttrs)
	latencyHist.Record(ctx, latencyMs, metricAttrs)

	recordCall(CallRecord{
		Timestamp:   time.Now(),
		Provider:    cfg.Provider,
		Model:       cfg.Model,
		FeatureID:   cfg.FeatureID,
		UserID:      cfg.UserID,
		ProjectID:   cfg.ProjectID,
		InputTokens: result.InputTokens,
		CostUSD:     costUSD,
		LatencyMs:   latencyMs,
	})

	if devPrinter != nil {
		res := providers.CallResult{
			InputTokens: result.InputTokens,
			Model:       cfg.Model,
		}
		devPrinter(cfg, &res, costUSD, latencyMs, nil)
	}

	return result, nil
}
