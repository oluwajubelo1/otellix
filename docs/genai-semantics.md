# GenAI Semantic Conventions

Otellix emits **both** the OpenTelemetry GenAI semantic conventions
(`gen_ai.*`) and the legacy `llm.*` attributes on every span. This keeps you
compatible with GenAI-aware backends (Langfuse, OpenLLMetry, OTel Collector
GenAI processors) while preserving backward compatibility with existing Otellix
dashboards.

## What is emitted

For a chat completion (`Trace`):

| Attribute | Example |
|---|---|
| `gen_ai.provider.name` | `openai` |
| `gen_ai.operation.name` | `chat` |
| `gen_ai.request.model` | `gpt-4o` |
| `gen_ai.request.max_tokens` | `200` |
| `gen_ai.response.model` | `gpt-4o` |
| `gen_ai.usage.input_tokens` | `100` |
| `gen_ai.usage.output_tokens` | `50` |
| `gen_ai.content.user` | (when content capture enabled) |
| `llm.provider` | `openai` |
| `llm.model` | `gpt-4o` |
| `llm.input_tokens` / `llm.output_tokens` | `100` / `50` |
| `llm.cost_usd` | `0.00075` |
| `llm.latency_ms` | `482` |

For embeddings (`TraceEmbedding`):

| Attribute | Example |
|---|---|
| `gen_ai.operation.name` | `embeddings` |
| `gen_ai.request.model` | `text-embedding-3-small` |
| `gen_ai.usage.input_tokens` | `20` |
| `llm.embeddings_dimensions` | `1536` |
| `llm.embeddings_count` | `4` |

## Span names

- Chat: `llm.call`
- Embeddings: `llm.embeddings`
- Streaming (same `llm.call` span, plus `llm.time_to_first_token_ms` and
  `llm.tokens_per_second` after the stream completes)

## Why dual-emit?

GenAI semantic conventions are still evolving. Dual-emission means you get the
forward-compatible `gen_ai.*` attributes today without breaking any `llm.*`
alerts or dashboards you already have — best of both worlds.
