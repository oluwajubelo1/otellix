# Integrations

Otellix is designed to play well with the Go ecosystem. We provide native support for major frameworks to ensure you get full visibility with minimal code changes.

## LangChainGo

Otellix provides an `OtellixHandler` that implements the `callbacks.Handler` interface for [LangChainGo](https://github.com/tmc/langchaingo).

### Setup

```go
import (
	"github.com/oluwajubelo1/otellix/integrations/langchaingo"
	"github.com/tmc/langchaingo/llms/openai"
)

func main() {
	// Create the handler. Pass WithProvider/WithModel so llm.cost_usd is
	// accurate (LangChainGo does not expose these in its callbacks).
	handler := langchaingo.NewOtellixHandler(
		langchaingo.WithProvider("openai"),
		langchaingo.WithModel("gpt-4o"),
	)

	// Register it during provider initialization
	p, err := openai.New(openai.WithCallback(handler))
	if err != nil {
		log.Fatal(err)
	}

	// Any calls made via this provider (or chains using it)
	// will now be automatically traced and cost-tracked.
	res, err := p.GenerateContent(ctx, parts)
}
```

The handler attaches `llm.*` attributes to a recording span in the context if one exists. If the context has no span (e.g. `context.Background()`), the handler creates and exports its own `llm.call` span, so telemetry is always captured without extra setup.

### Features
- **Automatic Token Mapping**: Maps provider-specific usage metadata to `llm.input_tokens` / `llm.output_tokens`.
- **Cost Tracking**: With `WithProvider`/`WithModel` configured, `llm.cost_usd` is computed from the pricing table.
- **Identity Attribution**: `otellix.ContextWithUser` / `otellix.ContextWithProject` values in the context are recorded as `llm.user_id` / `llm.project_id`.
- **Provider Support**: Works with OpenAI and Anthropic providers in LangChainGo. Set `WithProvider("anthropic")` / `WithModel("claude-3-5-sonnet-...")` for Anthropic cost tracking.

---

## Future Integrations
- [ ] **Ollama direct support**: Enhanced metadata capturing for local Llama3 instances.
- [ ] **Buffalo Middleware**: Expanding our zero-config attribution to the Buffalo web framework.
