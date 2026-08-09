# LLM Providers

Otellix is designed to be provider-agnostic. It wraps existing SDKs and provides a unified interface for tracing and budget enforcement.

## Supported Providers

### Provider wrappers (drop-in `Call`/`Stream`)

| Provider | Models Supported | Notes |
| --- | --- | --- |
| **OpenAI** | `gpt-5.5`, `gpt-5.4`, `gpt-5`, `gpt-5-mini`, `gpt-5-nano`, `gpt-4o`, `gpt-4o-mini`, `gpt-4.1*`, `gpt-4-turbo`, `gpt-4`, `gpt-3.5-turbo`, `o3`, `o3-mini`, `o3-pro`, `o4-mini`, `o1`, `o1-mini`, `o1-pro`, `gpt-oss-120b`, `gpt-oss-20b` | Uses `openai-go`. Automatically tracks cached tokens. |
| **Anthropic** | `claude-opus-5`, `claude-opus-4.5`–`4.8`, `claude-opus-4.1`, `claude-opus-4`, `claude-sonnet-5`, `claude-sonnet-4.5`/`4.6`, `claude-3-7-sonnet`, `claude-haiku-4.5`, `claude-haiku-3.5`, `claude-3-haiku`, `claude-fable-5`, `claude-mythos-5` | Uses `anthropic-sdk-go`. Tracks prompt caching. |
| **Google Gemini**| `gemini-3.6-flash`, `gemini-3.5-flash`, `gemini-3.1-pro`, `gemini-3-pro`, `gemini-3-flash`, `gemini-2.5-pro`, `gemini-2.5-flash`, `gemini-2.0-flash`, `gemini-1.5-pro`, `gemini-1.5-flash` | Uses `google-genai`. |
| **Ollama** | Any (Llama 3, Mistral, Phi-3, etc.) | High-performance, zero-dependency NDJSON streaming. |

### Pricing-only support (130+ models)

Cost tracking works out of the box for the models below — pass any provider that satisfies
the `providers.Provider` interface (or a mock) to `otellix.Trace` with `otellix.WithProvider("kimi")`.
No SDK wrapper ships for these yet; call their APIs directly and feed the result through the
standard `CallResult` path.

| Provider | Models Supported |
| --- | --- |
| **Kimi (Moonshot)** | `kimi-k3`, `kimi-k2.7-code`, `kimi-k2.6`, `kimi-k2.5`, `kimi-k2`, `kimi-k2-thinking`, `moonshot-v1-8k/32k/128k` |
| **DeepSeek** | `deepseek-v4`, `deepseek-v4-flash`, `deepseek-v4-pro`, `deepseek-r1`, `deepseek-chat`, `deepseek-v3`/`v3.1`/`v3.2` |
| **Qwen** | `qwen3.5-397b`, `qwen3.5-plus`, `qwen3.5-flash`, `qwen3.7-flash`, `qwen-max`, `qwen-plus`, `qwen-turbo` |
| **GLM (Zhipu)** | `glm-5.2`, `glm-5.1`, `glm-5`, `glm-4.7`, `glm-4.6`, `glm-4.5`, `glm-4.5-air`, `glm-4.5-flash`, `glm-4.7-flash` |
| **ERNIE (Baidu)** | `ernie-4.5`, `ernie-x1`, `ernie-4.5-turbo`, `ernie-speed` |
| **Hunyuan (Tencent)** | `hunyuan-hy3`, `hunyuan-large`, `hunyuan-standard` |
| **Mistral** | `mistral-medium-3.5`, `mistral-large`, `mistral-large-3`, `mistral-small`, `mistral-small-4`, `codestral`, `mistral-nemo`, `ministral-3b/8b`, `magistral-medium/small` |
| **Grok (xAI)** | `grok-4`, `grok-4.3`, `grok-4.20`, `grok-3`, `grok-4-fast` |

See [`cost.go`](../cost.go) for exact per-1M-token prices (July–August 2026 rates).

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
    InputPricePerMToken:      15.0,  // $15.00 per 1M tokens
    OutputPricePerMToken:     60.0,  // $60.00 per 1M tokens
    CacheReadPricePerMToken:  7.5,   // $7.50 per 1M cached tokens read
    CacheWritePricePerMToken: 15.0,  // $15.00 per 1M cached tokens written
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
