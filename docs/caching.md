# Prompt Caching ROI Analysis

Prompt caching is one of the most effective ways to reduce LLM costs. Providers like Anthropic and OpenAI now offer significant discounts for tokens served from cache. 

Otellix provides specialized metrics to help you quantify these savings in real-time.

## Captured Metrics

The following attributes are automatically added to your OTel spans:

| Attribute | Description |
| --- | --- |
| `llm.cache_read_tokens` | Tokens served from the provider-side cache. |
| `llm.cache_write_tokens` | Tokens written to the cache (usually priced at full rate). |

Both are factored into `llm.cost_usd`, which uses the provider's discounted cache-read price per token, so your spans reflect the real (lower) cost of cached calls.

## Why it matters
By tracking `llm.cache_read_tokens` and the discounted cost in `llm.cost_usd`, you can:
1.  **Calculate ROI**: See the direct financial benefit of your prompt engineering efforts.
2.  **Optimize Cache TTLs**: Identify which prompts are frequently hit and which are rarely reused.
3.  **Budgeting**: Realize that your actual spend is lower than "worst-case" scenarios, allowing for more aggressive usage.

## Provider Support

### Anthropic (Claude 3.5)
Otellix automatically extracts `cache_read_tokens` and `cache_write_tokens` from the Anthropic response metadata.

### OpenAI (GPT-4o)
OpenAI provides "Cached Tokens" in their usage block. Otellix maps these to the standard `cache_read` metric.

---

## Example Trace Analysis
When viewing an Otellix trace in Tempo or Jaeger, you'll see:
- `llm.input_tokens`: 500
- `llm.cache_read_tokens`: 450
- `llm.cost_usd`: $0.0005 (Discounted — cache-read tokens billed at the reduced cache price)
