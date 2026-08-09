# Reliability: Retries, Smart Routing & Live Price Sync

Otellix ships reliability tooling for production LLM workloads: automatic retry on
rate limits, cost-aware model selection, and hot-reloadable pricing.

## Rate-Limit Aware Retries

`otellix.RetryProvider` wraps any `Provider` and transparently retries transient
failures (429 rate limits, timeouts) with exponential backoff + jitter.

```go
rc := &otellix.RetryConfig{
    MaxAttempts:        4,
    BaseDelay:          100 * time.Millisecond,
    MaxDelay:           5 * time.Second,
    RetryOnRateLimit:   true,
    RetryOnTimeout:     true,
    RetryOnServerError: true,
}

p := otellix.RetryProvider(openaiProvider, rc)
```

- Only **retryable** errors are retried: `*providers.RateLimitError`,
  `*providers.TimeoutError`, and (optionally) 5xx server errors.
- Permanent errors (auth, invalid request) return immediately.
- Backoff honours the provider's `Retry-After` header when present.
- If the wrapped provider already implements `otellix.Retryer`, its own
  `RetryDelay(attempt)` is used instead.

## Smart Cost Routing

`otellix.SelectCheapestModel` picks the cheapest model from the pricing table
that satisfies a cost, latency and quality budget:

```go
model, err := otellix.SelectCheapestModel(
    "openai",           // preferred provider
    "gpt-4o",           // preferred model
    500,                // estimated output tokens
    0.05,               // max budget in USD for this call
    true,               // latency-sensitive? (prefers same provider)
)
```

Behaviour:
- If the preferred model fits the budget, it is returned as-is (no routing).
- Otherwise, cheaper models across all registered providers are considered.
- `latencySensitive=true` constrains the search to the preferred provider to
  avoid cross-provider hops.
- Returns a `*providers.RateLimitError`-style error if **no** model fits the
  budget — pair with `RetryProvider` and a fallback for graceful degradation.

## Live Price Sync

Pricing is baked into the table at compile time, but vendors change prices.
`otellix.SyncPricing` refreshes the table at runtime from a hosted JSON feed,
and `otellix.SyncPricingFromJSON` accepts the same shape from any `[]byte`
(config file, env var, database):

```json
{
  "openai/gpt-4o":   {"input": 2.50,  "output": 10.0, "cache_read": 1.25, "cache_write": 2.50},
  "anthropic/claude-sonnet-4-6": {"input": 3.0, "output": 15.0, "cache_read": 0.3}
}
```

```go
// From a hosted feed (e.g. a CDN or GitHub raw file)
if err := otellix.SyncPricing("https://example.com/pricing.json"); err != nil {
    log.Printf("price sync failed: %v", err)
}

// Or from bytes
if err := otellix.SyncPricingFromJSON(pricingBytes); err != nil { ... }
```

Merging is additive — entries already in the table are overridden, entries not
present in the doc are left untouched. Keys are `provider/model`.

Run this in a background ticker to keep costs accurate as vendors adjust prices.
