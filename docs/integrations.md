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

## LangGraph

LangGraph-style agent loops in Go run on top of LangChainGo callbacks, so the same handler captures agent LLM calls end to end. `integrations/langgraph` adds agent identity (name + step) on top:

```go
import (
	"github.com/oluwajubelo1/otellix/integrations/langgraph"
)

handler := langgraph.NewOtellixHandler(
	langgraph.WithProvider("openai"),
	langgraph.WithModel("gpt-4o"),
)
// attach handler to your langchaingo LLM, then inside each node:
ctx = langgraph.ContextWithAgent(ctx, "researcher")
ctx = langgraph.ContextWithStep(ctx, nodeNumber)
```

Every LLM call made while `ctx` is active is attributed to the agent and step, so you can see cost per agent, per step, across the whole run.

---

## nethttp (HTTP client transport)

`integrations/nethttp` provides an `http.RoundTripper` that wraps every outbound request in an `llm.http.*` span — useful when you call LLM HTTP APIs directly (e.g. self-hosted gateways, OpenAI-compatible endpoints):

```go
client := &http.Client{Transport: nethttp.NewTransport(nil)}
```

Each request gets a span (e.g. `llm.http.GET /v1/chat/completions`) with status-code + latency, and it parent-scopes any `Trace()` call made within the handler.

---

## slogbridge (log/slog)

`integrations/slogbridge` wraps any `slog.Handler` and augments log records with the active OTel span context plus Otellix attribution:

```go
logger := slog.New(slogbridge.NewHandler(slog.NewTextHandler(os.Stdout, nil)))
```

Every log line now carries `trace_id`, `span_id`, `llm.user_id`, and `llm.project_id` from the current context — so logs and LLM spans join up in your trace backend.

---

## Test Harness (`otellix/test`)

Write span assertions without a collector:

```go
import oteltest "github.com/oluwajubelo1/otellix/test"

h := oteltest.New()
defer h.Close()

_, err := otellix.Trace(ctx, mock, params, opts...)
h.AssertAttribute(t, "llm.call", "llm.cost_usd", 0.00075)
```

The harness swaps in an in-memory `TracerProvider`, records every span, and exposes `Attribute`, `SpanByName`, `AssertAttribute`, `AssertSpanCount`, and `Reset`. Zero external dependencies.

---

## Future Integrations
- [ ] **Ollama direct support**: Enhanced metadata capturing for local Llama3 instances.
- [ ] **Buffalo Middleware**: Expanding our zero-config attribution to the Buffalo web framework.
