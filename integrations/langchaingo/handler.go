package langchaingo

import (
	"context"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"
	"github.com/tmc/langchaingo/callbacks"
	"github.com/tmc/langchaingo/llms"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// OtellixHandler is a LangChainGo callback handler that provides automated
// LLM observability, cost tracking, and budget enforcement.
type OtellixHandler struct {
	callbacks.SimpleHandler
}

// NewOtellixHandler creates a new handler instance.
func NewOtellixHandler() *OtellixHandler {
	return &OtellixHandler{}
}

// HandleLLMStart is called when an LLM starts.
func (h *OtellixHandler) HandleLLMStart(ctx context.Context, prompts []string) {
	span := trace.SpanFromContext(ctx)
	span.AddEvent("llm.start", trace.WithAttributes(
		attribute.Int("llm.prompt_count", len(prompts)),
	))
}

// HandleLLMGenerateContentStart is called when an LLM starts.
func (h *OtellixHandler) HandleLLMGenerateContentStart(ctx context.Context, ms []llms.MessageContent) {
	span := trace.SpanFromContext(ctx)
	span.AddEvent("llm.start", trace.WithAttributes(
		attribute.Int("llm.message_count", len(ms)),
	))
}

// HandleLLMGenerateContentEnd is called when an LLM finishes.
func (h *OtellixHandler) HandleLLMGenerateContentEnd(ctx context.Context, res *llms.ContentResponse) {
	span := trace.SpanFromContext(ctx)

	// 1. Extract usage metrics.
	usage := extractUsage(res)

	// 2. Determine provider and model (best effort).
	provider := "unknown"
	model := "unknown"

	// In langchaingo, model info is often in GenerationInfo of the first choice.
	if len(res.Choices) > 0 {
		info := res.Choices[0].GenerationInfo
		if m, ok := info["model"].(string); ok {
			model = m
		}
	}

	// 3. Compute cost.
	costUSD := otellix.CalculateCost(provider, model, usage)

	// 4. Record attributes.
	span.SetAttributes(
		attribute.String("llm.model", model),
		attribute.Int64("llm.input_tokens", usage.InputTokens),
		attribute.Int64("llm.output_tokens", usage.OutputTokens),
		attribute.Int64("llm.cache_read_tokens", usage.CacheReadTokens),
		attribute.Int64("llm.cache_write_tokens", usage.CacheWriteTokens),
		attribute.Float64("llm.cost_usd", costUSD),
	)

	span.AddEvent("llm.end")
}

// HandleLLMError is called when an LLM errors.
func (h *OtellixHandler) HandleLLMError(ctx context.Context, err error) {
	span := trace.SpanFromContext(ctx)
	span.RecordError(err)
}

// extractUsage attempts to find token usage in the Choice GenerationInfo.
func extractUsage(res *llms.ContentResponse) providers.CallResult {
	result := providers.CallResult{}
	if len(res.Choices) == 0 {
		return result
	}

	info := res.Choices[0].GenerationInfo

	// OpenAI path
	result.InputTokens = toInt64(info["PromptTokens"])
	result.OutputTokens = toInt64(info["CompletionTokens"])

	// Sometimes it's lowercase or uses underscores in different versions
	if result.InputTokens == 0 {
		result.InputTokens = toInt64(info["prompt_tokens"])
	}
	if result.OutputTokens == 0 {
		result.OutputTokens = toInt64(info["completion_tokens"])
	}

	return result
}

func toInt64(v any) int64 {
	switch i := v.(type) {
	case int:
		return int64(i)
	case int64:
		return i
	case float64:
		return int64(i)
	}
	return 0
}
