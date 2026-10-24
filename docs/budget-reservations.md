# Budget Reservations & Cost Estimates

A budget is only useful if it holds when traffic spikes. This guide explains how Otellix stops calls that run **at the same time** from overshooting a limit together, and how it estimates what a call will cost before making it.

Related guides:

- [Budget Guardrails](budget-guardrails.md): limits, fallback actions and stores.
- [Budget Windows](budget-windows.md): rolling, daily and monthly periods.
- [Unpriced Models](unpriced-models.md): what happens when a model has no price.

---

## The problem: concurrent calls

A budget check has three steps: read how much has been spent, call the provider, then record what the call cost. The provider call in the middle takes seconds.

Without reservations, 20 calls arriving together all read the same spend, all see room under the limit, and all go ahead:

```
limit: $0.035          each call costs $0.01

call 1  reads $0.00 ─ allowed ─ calling... ─ records $0.01
call 2  reads $0.00 ─ allowed ─ calling... ─ records $0.01
  ...
call 20 reads $0.00 ─ allowed ─ calling... ─ records $0.01
                                            total: $0.20, almost 6× the limit
```

That's exactly what happens in a retry storm, a runaway agent loop, or a user hammering a button, which are the moments a budget exists for.

## How reservations fix it

Before a call, Otellix **reserves** its estimated cost: in one atomic step, it checks every limit that applies and, if the estimate fits, adds a *hold* for that amount. A hold counts as spend, so the next call sees it.

```
call 1  reserve $0.01 → spend $0.01 ─ allowed
call 2  reserve $0.01 → spend $0.02 ─ allowed
call 3  reserve $0.01 → spend $0.03 ─ allowed
call 4  reserve $0.01 → $0.04 > $0.035 ─ blocked
  ...
```

When a call finishes, Otellix records its **actual** cost and then drops the hold. If the call fails, the hold is dropped and nothing is charged.

You don't need to change any code. Reservations are on whenever you use `WithBudgetConfig` with the built-in in-memory or Redis store.

> **Fixed in this release:** earlier versions checked and recorded in separate steps, so concurrent calls could overshoot a limit by any amount. A test that sends 20 calls at once at a limit with room for 3 now lets exactly 3 through; before, all 20 went ahead.

---

## How the estimate is worked out

A reservation holds the call's **estimated** cost, since the real cost isn't known until the provider replies. The estimate is:

- **Input tokens:** estimated from the length of `params.SystemPrompt` and every message in `params.Messages`, at about 4 characters per token.
- **Output tokens:** `BudgetConfig.EstimatedOutputTokens` (default **500**), but never more than `params.MaxTokens` when that is set.

Both are priced with the model's input and output prices.

> **Changed in this release:** earlier versions ignored the prompt and assumed 500 input and 500 output tokens for every call, so a 100,000-token prompt was estimated at the same cost as "hi".

### Example

A call to `gpt-4o` ($2.50 per million input tokens, $10 per million output) with a 2,000-character prompt and no `MaxTokens`:

| Part | Tokens | Cost |
| --- | --- | --- |
| Input (2,000 chars ÷ 4, plus 4 per message) | 504 | $0.00126 |
| Output (default) | 500 | $0.00500 |
| **Estimate held** | | **$0.00626** |

---

## Choosing how strict to be

The estimate is a trade-off between never overshooting and never blocking too early.

### Default: typical output

```go
budget := &otellix.BudgetConfig{
    PerUserLimit: 1.00,
    // EstimatedOutputTokens: 500, // the default
}
```

Each call holds a typical cost. Calls that produce more output than the estimate can push spend a little over the limit, but only by the difference between the estimate and the real cost of the calls running at that moment. A user is never blocked while they have a reasonable amount of budget left.

If your calls usually produce much more or much less than 500 tokens, set `EstimatedOutputTokens` to your typical output. The `otellix.llm.tokens.output` metric shows what your calls really produce.

### Strict: worst case

```go
budget := &otellix.BudgetConfig{
    PerUserLimit:     1.00,
    ReserveWorstCase: true,
}
```

Each call holds the cost of its full `params.MaxTokens` of output, the most it can possibly produce. Concurrent calls can then **never** overshoot the limit together (apart from errors in the input estimate).

The cost: while a call runs, it holds its worst case. A user with $0.95 of a $1.00 limit spent can be blocked even though their next call would only cost $0.01, if its worst case is $0.06. Use it for hard caps, such as a free tier where any overshoot is lost money. Calls with no `MaxTokens` fall back to `EstimatedOutputTokens`, so set `MaxTokens` on every call when you use this.

| | Default | `ReserveWorstCase` |
| --- | --- | --- |
| Output assumed | `EstimatedOutputTokens` (500), capped at `MaxTokens` | `MaxTokens` |
| Can concurrent calls overshoot? | Slightly, by how much they beat the estimate | No |
| Can a user be blocked before the limit? | Rarely | Yes, near the limit |

---

## Streams

`TraceStream` reserves the estimate before the stream starts, like `Trace`. While tokens arrive, it also checks the cost so far against the budget. If the stream runs past the budget, `Recv()` returns a `BudgetExceededError` and the stream is closed. On `Close()`, the actual cost is recorded and the hold dropped.

Always `Close()` a stream, usually with `defer stream.Close()`. A stream that is never closed keeps its hold until it falls out of the budget window.

## Request deduplication

With [request deduplication](request-deduplication.md), identical requests made at the same time share one provider call. Each caller reserves its own estimate, but only the caller whose call runs is charged. The other holds are dropped as soon as the shared result comes back. Cache hits are charged nothing.

---

## Budget stores and reservations

Reservations need a store that can check and hold in one atomic step. Stores do that by implementing the `otellix.BudgetReserver` interface:

```go
type BudgetReserver interface {
    Reserve(ctx context.Context, amount float64, limits []BudgetLimit) (id string, ok bool, err error)
    Release(ctx context.Context, id string, keys []string)
}
```

| Store | Atomic reservations? | How |
| --- | --- | --- |
| `InMemoryBudgetStore` | Yes, within one process | Locks every key involved, in a fixed order |
| `RedisBudgetStore` | Yes, across every instance sharing the Redis | One Lua script checks every limit and adds the hold |
| Your own `BudgetStore` | Only if it implements `BudgetReserver` | Otherwise Otellix falls back to the old check, which can overshoot |

### Redis details

In Redis, a hold is stored like any other spend event: a sorted-set member named `hold-<uuid>:<amount>`, scored by time. That means:

- `GetSpend` counts holds without any extra work.
- If your app crashes mid-call and never releases a hold, the hold falls out of the [budget window](budget-windows.md) like old spend does. It can't block a user forever.
- A user's key and a project's key are checked and held in one script, so a call either holds against both or neither.

If Redis is unreachable, Otellix **allows** the call and reports the error through the OpenTelemetry error handler (see `otel.SetErrorHandler`). This matches how `GetSpend` already behaves when Redis is down: a budget store outage shouldn't take every LLM feature down with it.

### Writing your own store

If you have a custom `BudgetStore`, add `Reserve` and `Release`. `Reserve` must, as one atomic operation:

1. Work out each key's current spend, holds included.
2. If any key's spend plus `amount` is over its `Limit`, return `ok=false` and hold nothing.
3. Otherwise add a hold of `amount` to every key, and return an ID for it.

`Release` removes that hold from the keys. Make holds expire with your window, so a crashed caller can't leave one behind forever. The in-memory and Redis stores in this repository are working examples.

---

## Using reservations directly

If you call the enforcer yourself, for example around a provider Otellix doesn't wrap, use `Reserve` instead of `Check`:

```go
enforcer := otellix.NewBudgetEnforcer(budget)

estimate := otellix.EstimateCallCost("openai", "gpt-4o", params, 500)
res, allowed := enforcer.Reserve(ctx, userID, projectID, estimate)
defer res.Release(ctx) // safe on nil, and safe to call twice
if !allowed {
    return enforcer.BudgetError(userID, projectID)
}

result, err := callTheProvider(ctx, params)
if err != nil {
    return err // the deferred Release drops the hold
}

enforcer.Record(ctx, userID, projectID, otellix.CalculateCost("openai", "gpt-4o", result))
res.Release(ctx) // record first, then release, so the spend is never briefly uncounted
```

`Check` still exists, but holds nothing, so calls checked at the same time can all be allowed.

---

## Troubleshooting

**A user is blocked although their spend is below the limit.**
Spend includes the holds of their calls still running, plus the estimate of the new call. Look at the `BudgetExceededError`: `CurrentSpend` includes holds. If you use `ReserveWorstCase`, a large `MaxTokens` holds a lot; lower `MaxTokens`, or switch to the default estimate.

**Spend went slightly over the limit.**
With the default estimate, calls that produce more output than `EstimatedOutputTokens` can overshoot a little. Raise `EstimatedOutputTokens` closer to your real output, or use `ReserveWorstCase`.

**Spend went far over the limit.**
Check that the `BudgetConfig` is created once and shared, not created per request. See [Budget Guardrails](budget-guardrails.md#quick-start). If you use a custom store, check that it implements `BudgetReserver`. Also check the model is priced: unpriced models cost $0, see [Unpriced Models](unpriced-models.md).

**A long prompt is blocked before the call.**
That's intended: the estimate includes the prompt. A 200,000-character prompt is about 50,000 input tokens.
