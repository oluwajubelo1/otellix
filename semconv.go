package otellix

import (
	"github.com/oluwajubelo1/otellix/providers"
	"go.opentelemetry.io/otel/attribute"
)

// OTel GenAI semantic convention attribute keys (gen_ai.* namespace).
// Otellix dual-emits the same data under both llm.* and gen_ai.* so spans
// work with GenAI-aware backends (Grafana GenAI, Langfuse, Phoenix, etc.)
// as well as generic OTel tools.
const (
	genAIProviderName  = "gen_ai.provider.name"
	genAIRequestModel  = "gen_ai.request.model"
	genAIResponseModel = "gen_ai.response.model"
	genAIOperationName = "gen_ai.operation.name"
	genAIUsageInput    = "gen_ai.usage.input_tokens"
	genAIUsageOutput   = "gen_ai.usage.output_tokens"
	genAIRequestMax    = "gen_ai.request.max_tokens"
	genAIRequestTemp   = "gen_ai.request.temperature"
)

// genAIOperation values.
const (
	genAIOpChat       = "chat"
	genAIOpEmbeddings = "embeddings"
)

// baseSpanAttributes returns the standard llm.* and gen_ai.* attributes for a
// traced call. operation is one of genAIOpChat / genAIOpEmbeddings.
func baseSpanAttributes(cfg *Config, operation string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("llm.provider", cfg.Provider),
		attribute.String("llm.model", cfg.Model),
		attribute.String("llm.feature_id", cfg.FeatureID),
		attribute.String("llm.user_id", cfg.UserID),
		attribute.String("llm.project_id", cfg.ProjectID),
		attribute.String(genAIProviderName, cfg.Provider),
		attribute.String(genAIRequestModel, cfg.Model),
		attribute.String(genAIOperationName, operation),
		attribute.String(genAIResponseModel, cfg.Model),
	}
	return attrs
}

// requestAttributes emits gen_ai.request.* attributes from the call params.
func requestAttributes(params providers.CallParams) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	if params.MaxTokens > 0 {
		attrs = append(attrs, attribute.Int(genAIRequestMax, params.MaxTokens))
	}
	if params.Temperature != nil {
		attrs = append(attrs, attribute.Float64(genAIRequestTemp, *params.Temperature))
	}
	return attrs
}

// usageAttributes emits the token/cost/latency data under both namespaces.
func usageAttributes(result *providers.CallResult, latencyMs, costUSD float64) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.Int64("llm.input_tokens", result.InputTokens),
		attribute.Int64("llm.output_tokens", result.OutputTokens),
		attribute.Int64("llm.cache_read_tokens", result.CacheReadTokens),
		attribute.Int64("llm.cache_write_tokens", result.CacheWriteTokens),
		attribute.Float64("llm.latency_ms", latencyMs),
		attribute.Float64("llm.cost_usd", costUSD),
		attribute.Int64(genAIUsageInput, result.InputTokens),
		attribute.Int64(genAIUsageOutput, result.OutputTokens),
	}
}
