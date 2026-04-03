# LLM Providers

Otellix is designed to be provider-agnostic. It wraps existing SDKs and provides a unified interface for tracing and budget enforcement.

## Supported Providers

| Provider | Models Supported | Notes |
| --- | --- | --- |
| **OpenAI** | `gpt-4o`, `gpt-4o-mini`, `gpt-3.5-turbo` | Uses `openai-go`. Automatically tracks cached tokens. |
| **Anthropic** | `claude-3-5-sonnet`, `claude-3-opus`, `claude-3-haiku` | Uses `anthropic-sdk-go`. Tracks prompt caching. |
| **Google Gemini**| `gemini-2.0-flash`, `gemini-1.5-pro` | Uses `google-genai`. |
| **Ollama** | Any (Llama 3, Mistral, Phi-3, etc.) | High-performance, zero-dependency NDJSON streaming. |

## Quick Configuration

### OpenAI
```go
import "github.com/oluwajubelo/otellix/providers/openai"

provider := openai.New(openai.WithAPIKey("your-key"))
```

### Anthropic
```go
import (
    "github.com/oluwajubelo/otellix/providers/anthropic"
    "github.com/anthropics/anthropic-sdk-go/option"
)

provider := anthropic.New(option.WithAPIKey("your-key"))
```

### Google Gemini
```go
import "github.com/oluwajubelo/otellix/providers/gemini"

provider, err := gemini.New(ctx, "your-api-key")
```

### Ollama (Local LLM)
```go
import "github.com/oluwajubelo/otellix/providers/ollama"

provider := ollama.New(ollama.WithBaseURL("http://localhost:11434"))
```

## Custom Models & Pricing

Otellix uses an internal pricing table for cost attribution. You can override or add your own models using `RegisterModel`.

```go
otellix.RegisterModel("openai", "custom-model-v1", otellix.PricingEntry{
    InputPricePerMToken:  15.0,  // $15.00 per 1M tokens
    OutputPricePerMToken: 60.0,  // $60.00 per 1M tokens
    CachePricePerMToken:  7.5,   // $7.50 per 1M cached tokens
})
```

## Creating a Custom Provider

To implement your own provider, satisfy the `providers.Provider` interface:

```go
type Provider interface {
    Name() string
    Call(ctx context.Context, params CallParams) (CallResult, error)
    Stream(ctx context.Context, params CallParams) (Stream, error)
}
```

By ensuring your provider returns a `CallResult` or `Stream`, Otellix automatically handles the OpenTelemetry spans and budget enforcement.
