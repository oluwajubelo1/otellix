package langchaingo

import (
	"context"
	"strings"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"
	"github.com/tmc/langchaingo/callbacks"
	"github.com/tmc/langchaingo/llms"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// tracerName is used for spans created by this integration when the caller's
// context does not already contain a recording span.
const tracerName = "github.com/oluwajubelo1/otellix/integrations/langchaingo"

// OtellixHandler is a LangChainGo callback handler that provides automated
// LLM observability and cost tracking.
type OtellixHandler struct {
	callbacks.SimpleHandler

	provider string
	model    string
}

// Option configures an OtellixHandler.
type Option func(*OtellixHandler)

// WithProvider sets the LLM provider name (e.g. "openai", "anthropic") used for
// cost calculation. LangChainGo does not expose the provider in its callbacks,
// so set this to get accurate llm.cost_usd values.
func WithProvider(provider string) Option {
	return func(h *OtellixHandler) { h.provider = provider }
}

// WithModel sets the model name (e.g. "gpt-4o") used for cost calculation.
// LangChainGo does not reliably expose the model in its callbacks, so set this
// to get accurate llm.cost_usd values.
func WithModel(model string) Option {
	return func(h *OtellixHandler) { h.model = model }
}

// NewOtellixHandler creates a new handler instance. Pass WithProvider and
// WithModel to enable accurate cost tracking.
func NewOtellixHandler(opts ...Option) *OtellixHandler {
	h := &OtellixHandler{}
	for _, opt := range opts {
		opt(h)
	}
	return h
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
	// LangChainGo does not create a span itself and passes the caller's context
	// through unchanged, so if it contains no recording span we create our own
	// to guarantee telemetry is always exported.
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		_, span = otel.Tracer(tracerName).Start(ctx, "llm.call")
		defer span.End()
	}

	// 1. Extract usage metrics.
	usage := extractUsage(res)

	// 2. Resolve provider and model: explicit configuration wins, then
	// best-effort detection from the response metadata.
	provider := h.provider
	model := h.model
	if len(res.Choices) > 0 {
		info := res.Choices[0].GenerationInfo
		if m, ok := info["model"].(string); ok && m != "" && model == "" {
			model = m
		}
	}
	if provider == "" {
		provider = providerFromModel(model)
	}
	if provider == "" {
		provider = "unknown"
	}
	if model == "" {
		model = "unknown"
	}

	// 3. Compute cost.
	costUSD := otellix.CalculateCost(provider, model, usage)

	// 4. Record attributes.
	attrs := []attribute.KeyValue{
		attribute.String("llm.provider", provider),
		attribute.String("llm.model", model),
		attribute.Int64("llm.input_tokens", usage.InputTokens),
		attribute.Int64("llm.output_tokens", usage.OutputTokens),
		attribute.Int64("llm.cache_read_tokens", usage.CacheReadTokens),
		attribute.Int64("llm.cache_write_tokens", usage.CacheWriteTokens),
		attribute.Float64("llm.cost_usd", costUSD),
	}
	if userID := otellix.UserFromContext(ctx); userID != "" {
		attrs = append(attrs, attribute.String("llm.user_id", userID))
	}
	if projectID := otellix.ProjectFromContext(ctx); projectID != "" {
		attrs = append(attrs, attribute.String("llm.project_id", projectID))
	}
	if agent := otellix.AgentFromContext(ctx); agent != "" {
		attrs = append(attrs, attribute.String("llm.agent.name", agent))
	}
	if step := otellix.StepFromContext(ctx); step > 0 {
		attrs = append(attrs, attribute.Int("llm.agent.step", step))
	}
	span.SetAttributes(attrs...)

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

// providerFromModel best-effort infers the provider name from a model string,
// e.g. "gpt-4o" -> "openai", "claude-3-5-sonnet" -> "anthropic".
func providerFromModel(model string) string {
	switch {
	case model == "":
		return ""
	case strings.HasPrefix(model, "gpt-"):
		return "openai"
	case strings.HasPrefix(model, "claude"):
		return "anthropic"
	case strings.HasPrefix(model, "gemini"):
		return "gemini"
	case strings.HasPrefix(model, "llama-3"):
		return "ollama"
	}
	return ""
}
