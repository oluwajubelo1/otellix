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
	// Create the handler
	handler := langchaingo.NewOtellixHandler()

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

### Features
- **Automatic Token Mapping**: Maps provider-specific usage metadata to Otellix cost metrics.
- **Chain Visibility**: Captures the entire lifecycle of chains, including tool calls and nested iterations.
- **Provider Support**: Works with OpenAI and Anthropic providers in LangChainGo.

---

## Future Integrations
- [ ] **Ollama direct support**: Enhanced metadata capturing for local Llama3 instances.
- [ ] **Buffalo Middleware**: Expanding our zero-config attribution to the Buffalo web framework.
