# Unpriced Models

Otellix works out a call's cost from a pricing table of 130+ models. This guide covers what happens when a call uses a model that isn't in that table, and how to make sure those calls still count against your budgets.

Related guides:

- [Budget Guardrails](budget-guardrails.md): limits and fallback actions.
- [Budget Reservations & Cost Estimates](budget-reservations.md): how a call's cost is estimated before it's made.
- [Providers](providers.md): the built-in pricing table.

---

## Why it matters

A model with no pricing has no cost, so Otellix records it as **$0**. A $0 call never uses up a budget. That can happen without anyone noticing:

- you switch to a model released after your Otellix version;
- you call a model by a dated or regional name, such as `gpt-4o-2024-11-20`, that isn't in the table;
- you mistype the model name in config.

In all three cases, every budget silently stops working for that model. This guide shows how to spot it and what to do.

---

## What happens by default

Calls to an unpriced model **go ahead**, are costed at $0, and are flagged three ways so you can find them:

| Where | What you see |
| --- | --- |
| Span attribute | `llm.pricing_missing = true` on the call's span |
| Metric | `otellix.llm.calls.unpriced`, a counter with `provider` and `model` labels. In Prometheus: `otellix_llm_calls_unpriced_total` |
| Log | Once per model: `otellix: no pricing for openai/gpt-unreleased, so its calls are costed at $0; add it with otellix.RegisterModel` |

The log line goes through OpenTelemetry's error handler, which prints to standard error by default. To send it to your own logger:

```go
otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
    slog.Warn("otel", "err", err)
}))
```

These flags appear on every traced call, with or without a budget.

---

## Fixing it

You have three options. Pick the first that fits.

### 1. Add the model's price (best)

Register the real price at startup, before any calls:

```go
otellix.RegisterModel("openai", "gpt-4o-2024-11-20", otellix.PricingEntry{
    InputPricePerMToken:      2.50,
    OutputPricePerMToken:     10.00,
    CacheReadPricePerMToken:  1.25,
    CacheWritePricePerMToken: 2.50,
})
```

Prices are USD per million tokens. You can also use `RegisterModel` to override a built-in price, for example a negotiated discount.

### 2. Set a fallback price

If you can't list every model, for example because users can choose their own, set a price that applies to any model without one:

```go
budget := &otellix.BudgetConfig{
    PerUserLimit: 5.00,
    UnpricedModelPrice: &otellix.PricingEntry{
        InputPricePerMToken:  15.00,
        OutputPricePerMToken: 75.00,
    },
}
```

Unpriced calls are then estimated, reserved and charged at this price. Choose a price at or above your most expensive model, so an unknown model can't spend more than you expect. Calls are still flagged with `llm.pricing_missing`, so you can add their real prices later.

The fallback price is also used for the call's `llm.cost_usd` span attribute and the cost metric.

### 3. Block unpriced models

For strict cost control, refuse any call whose cost can't be checked:

```go
budget := &otellix.BudgetConfig{
    PerUserLimit:  5.00,
    UnpricedModel: otellix.UnpricedBlock,
}

_, err := otellix.Trace(ctx, provider, params, otellix.WithBudgetConfig(budget))

var unpriced *otellix.UnpricedModelError
if errors.As(err, &unpriced) {
    // The provider was never called.
    log.Printf("no price for %s/%s", unpriced.Provider, unpriced.Model)
}
```

`UnpricedModelError` has `Provider` and `Model` fields. If `UnpricedModelPrice` is also set, it wins: the call goes ahead at the fallback price.

With `FallbackDowngrade`, the fallback model is checked too: if it has no price, the call is blocked.

| Setting | Unpriced call goes ahead? | Counts against budget? |
| --- | --- | --- |
| Default (`UnpricedAllow`) | Yes | No, costed at $0 |
| `UnpricedModelPrice` set | Yes | Yes, at the fallback price |
| `UnpricedBlock` | No, `UnpricedModelError` | n/a |

`UnpricedModel` and `UnpricedModelPrice` only apply to calls with a `BudgetConfig`.

---

## Finding unpriced models

Which unpriced models were called in the last day:

```promql
sum by (provider, model) (increase(otellix_llm_calls_unpriced_total[1d]))
```

An alert that fires when any unpriced model is used:

```promql
sum(increase(otellix_llm_calls_unpriced_total[15m])) > 0
```

In a tracing backend, search for spans with `llm.pricing_missing = true`.

---

## Ollama and other local models

Ollama models are priced at $0 by a wildcard entry (`ollama/any`), since they run on your own hardware. They count as priced, so they're never flagged or blocked. To give local models a cost, for example to account for GPU time, register a price with `RegisterModel("ollama", "llama3", ...)`.

---

## Troubleshooting

**I registered a price but calls are still flagged.**
The provider and model must match exactly what the call uses: `cfg.Provider` (the provider's `Name()` or `WithProvider`) and `params.Model` (or `WithModel`). Check the `llm.provider` and `llm.model` attributes on the span.

**The warning only appeared once.**
That's by design: each provider/model is reported once per process, so logs aren't flooded. Use the metric to count calls.

**`UnpricedBlock` is set but calls go ahead.**
`UnpricedModelPrice` is probably set as well, and it takes precedence. Or the call has no `BudgetConfig`.
