# Budget Guardrails & Policies

Otellix provides an enforcement layer that goes beyond cost tracking. You set hard or soft limits on LLM spend per user, per project, or for calls with no user, and Otellix applies them **before** the provider is called. Runaway costs are stopped instead of just reported.

Related guides:

- [Budget Windows](budget-windows.md): rolling, daily and monthly periods, time zones and resets.
- [Cost Attribution](attribution.md): how each call gets its user and project, and how to cap calls with no user.

---

## Quick start

```go
budget := &otellix.BudgetConfig{
    PerUserLimit:    1.00,  // $1.00 per user, per window
    PerProjectLimit: 25.00, // $25.00 per project, per window
    FallbackAction:  otellix.FallbackBlock,
}

result, err := otellix.Trace(ctx, provider, params,
    otellix.WithUserID("user_456"),
    otellix.WithProjectID("acme"),
    otellix.WithBudgetConfig(budget),
)

var budgetErr *otellix.BudgetExceededError
if errors.As(err, &budgetErr) {
    // The provider was never called. Nothing was spent.
}
```

With no `Store` and no `Window`, this uses an in-memory store with a rolling 24-hour window. Create the `BudgetConfig` **once** and reuse it across calls: the default in-memory store is created inside it on first use, and every later call shares it. A shared config is safe to use from many goroutines at once.

> **Fixed in this release:** earlier versions could create several default stores when the first calls on a config ran concurrently, losing the spend recorded in all but one. Under load, most spend could go uncounted.

> **Don't create a `BudgetConfig` per request.** Each new config without a `Store` gets its own empty store, so spend never adds up and limits never trigger. Create it once at startup, or pass a shared `Store`.

---

## Key concepts

### 1. The budget config (`otellix.BudgetConfig`)

The central policy object. It sets the limits, what happens when one is hit, and where spend is stored.

| Field | Type | Default | What it does |
| --- | --- | --- | --- |
| `PerUserLimit` | `float64` (USD) | `0` = no limit | Maximum spend per user ID, per window. |
| `PerProjectLimit` | `float64` (USD) | `0` = no limit | Maximum spend per project ID, per window. |
| `UnattributedLimit` | `float64` (USD) | `0` = no limit | One shared maximum for **all** calls with no user ID, per window. See [Calls with no user](#calls-with-no-user). |
| `FallbackAction` | `FallbackAction` | `FallbackBlock` | What happens when a limit is hit. See [Fallback actions](#fallback-actions). |
| `FallbackModel` | `string` | `""` | The cheaper model used by `FallbackDowngrade`. |
| `Store` | `BudgetStore` | in-memory | Where spend is kept. See [Budget stores](#2-budget-stores-otellixbudgetstore). |
| `Window` | `*BudgetWindow` | rolling 24h | The window for the **default** in-memory store. Ignored when `Store` is set. See [Budget Windows](budget-windows.md). |
| `ResetInterval` | `time.Duration` | `24h` | Older way to set a rolling window for the default store. Ignored when `Store` or `Window` is set. |
| `PerUserDailyLimit` | `float64` | | **Deprecated:** use `PerUserLimit`. |
| `PerProjectDailyLimit` | `float64` | | **Deprecated:** use `PerProjectLimit`. |

All limits are in US dollars, and apply to the store's [budget window](budget-windows.md), which isn't necessarily a day.

> **Renamed fields.** `PerUserDailyLimit` and `PerProjectDailyLimit` still work, but always applied to the configured window rather than to a day. Use `PerUserLimit` and `PerProjectLimit` instead. If both old and new are set, the new one wins. See [Migrating](budget-windows.md#migrating-from-peruserdailylimit--perprojectdailylimit).

### 2. Budget stores (`otellix.BudgetStore`)

The store keeps track of spend. It also owns the [budget window](budget-windows.md).

| Store | Use it for | Survives restarts? | Shared across instances? |
| --- | --- | --- | --- |
| `InMemoryBudgetStore` | Local development, tests, a single process with short windows | No | No |
| `RedisBudgetStore` | Production, multiple instances, any window longer than a few hours | Yes | Yes |

In memory:

```go
store := otellix.NewInMemoryBudgetStore(24 * time.Hour)                           // rolling 24h
store := otellix.NewInMemoryBudgetStoreWithWindow(otellix.CalendarDay(time.UTC)) // resets at midnight UTC
```

Redis:

```go
import (
    "github.com/redis/go-redis/v9"
    otellixredis "github.com/oluwajubelo1/otellix/stores/redis"
)

// Initialise a go-redis client (see docker-compose.dev.yml for local Redis).
rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})

// Rolling 24h window, shared by every instance of your app.
store := otellixredis.NewRedisBudgetStore(rdb, "otellix:budget", 24*time.Hour)

// Or a calendar month that resets on the 1st, midnight UTC.
store = otellixredis.NewRedisBudgetStoreWithWindow(rdb, "otellix:budget", otellix.CalendarMonth(time.UTC))

config := &otellix.BudgetConfig{
    Store:        store,
    PerUserLimit: 5.00,
}
```

The prefix (`"otellix:budget"`) namespaces the keys. Use a different prefix for each environment or app that shares a Redis instance. Keys look like `otellix:budget:user:<id>`, `otellix:budget:project:<id>` and `otellix:budget:unattributed`.

You can also write your own store by implementing the `BudgetStore` interface: `GetSpend`, `AddSpend` and `GetResetTime`.

### 3. Budget windows

The window decides which spend counts and when it resets: a rolling period such as the last 24 hours or 30 days, a calendar day, or a calendar month. It has its own guide: [Budget Windows](budget-windows.md).

---

## How a call is checked

For `otellix.Trace`:

1. Otellix works out the call's user and project from the options or context. See [Cost Attribution](attribution.md#precedence).
2. It estimates the call's cost from the model's price, assuming about 500 output tokens.
3. It checks each limit that applies:
   - the user's `PerUserLimit`, or `UnattributedLimit` if there's no user;
   - the project's `PerProjectLimit`, if there's a project.

   A limit is exceeded if *current spend + estimate > limit*.
4. If any limit is exceeded, the `FallbackAction` runs.
5. If the call goes ahead, its **actual** cost is recorded against the user (or the unattributed budget) and the project.

For `otellix.TraceStream`, see [Streaming](#real-time-streaming-cutoff) below.

---

## Fallback actions

| Action | Behaviour | Use case |
| --- | --- | --- |
| `FallbackBlock` | Return `BudgetExceededError` immediately. The LLM is never called. | Hard cost caps, e.g. free tiers and untrusted users. |
| `FallbackNotify` | Allow the call, and add a `budget.warning` event to its span. | Internal tools where you want to audit, not interrupt. |
| `FallbackDowngrade` | Swap to `FallbackModel` and proceed. | Keeping the service available while managing costs. |

Downgrade example:

```go
budget := &otellix.BudgetConfig{
    PerUserLimit:   5.00,
    FallbackAction: otellix.FallbackDowngrade,
    FallbackModel:  "gpt-4o-mini", // used once a user passes $5
}
```

If `FallbackModel` is empty, Otellix uses the call's `otellix.WithFallbackModel(...)` option. If neither is set, the call proceeds with the original model.

---

## Calls with no user

A call with no user ID isn't covered by `PerUserLimit`. That includes background jobs, anonymous routes, and requests where the middleware found no identity. Set `UnattributedLimit` to give all of them one **shared** limit:

```go
budget := &otellix.BudgetConfig{
    PerUserLimit:      2.00,  // each signed-in user
    UnattributedLimit: 20.00, // all calls with no user, combined
    FallbackAction:    otellix.FallbackBlock,
}
```

Leaving it at `0` keeps the old behaviour: unattributed calls skip user-level limits. [Cost Attribution](attribution.md#calls-with-no-user) explains how to find these calls and fix them at the source.

---

## Which limit does the error report?

A call can be subject to two limits at once: a user's (or the unattributed one) and a project's. `BudgetExceededError` and `BudgetStatus` describe whichever has the **least remaining**, because that's the one that blocks.

For example, a user has spent $1.90 of their $5.00 limit, in a project that has spent $1.90 of its $2.00 limit. The error reports the project: `CurrentSpend` 1.90, `Limit` 2.00, and `ResetAt` for the project's budget.

> **Fixed in this release:** earlier versions always reported the user's budget when there was a user. A user under their own limit, blocked by their project's, got an error showing the wrong limit.

---

## Implementation patterns

### User-level guardrails

Otellix uses the call's user ID to attribute costs. Once a user hits their limit, further calls from them are blocked, downgraded or flagged, depending on `FallbackAction`.

```go
result, err := otellix.Trace(ctx, p, params,
    otellix.WithUserID("user_456"),
    otellix.WithBudgetConfig(myConfig),
)
```

In a web app, let [middleware](middleware.md) set the user for you, from verified identity if you enforce budgets.

### Real-time streaming cutoff

When using `TraceStream`, Otellix checks the budget before the stream starts. It then tracks the cumulative cost of the tokens received. If the budget is hit **mid-stream**, Otellix stops the stream and returns a `BudgetExceededError` from `Recv()`. Streams take their user and project from the context like `Trace` does. See [Streaming](streaming.md).

## Dynamic prompt optimization

With **prompt decorators**, you can change the request based on the remaining budget. The `BudgetStatus` passed in describes the tightest applicable budget (see [above](#which-limit-does-the-error-report)).

```go
decorator := func(ctx context.Context, status otellix.BudgetStatus, params *providers.CallParams) {
    if status.Remaining < 0.05 {
        params.SystemPrompt += "\n[BUDGET WARNING: Please be highly concise.]"
        params.MaxTokens = 50
    }
}

otellix.Trace(ctx, p, params,
    otellix.WithBudgetConfig(myConfig),
    otellix.WithPromptDecorator(decorator),
)
```

| `BudgetStatus` field | Meaning |
| --- | --- |
| `Usage` | USD spent so far in the current window |
| `Remaining` | USD left before the limit |
| `IsExceeded` | `true` once usage has reached the limit |
| `Mode` | The configured `FallbackAction` |

## Error handling

When a budget is hit with `FallbackBlock`, Otellix returns a typed `BudgetExceededError`. Use it to show the user a helpful message:

```go
var budgetErr *otellix.BudgetExceededError
if errors.As(err, &budgetErr) {
    fmt.Printf("Limit reached. Spend: $%.2f, Limit: $%.2f, Resets at: %v",
        budgetErr.CurrentSpend, budgetErr.Limit, budgetErr.ResetAt)
}
```

| `BudgetExceededError` field | Meaning |
| --- | --- |
| `UserID` | The call's user ID (`""` for unattributed calls) |
| `ProjectID` | The call's project ID |
| `CurrentSpend` | USD spent in the window, for the budget that blocked |
| `Limit` | That budget's limit |
| `ResetAt` | When that budget next frees up. See [Resets](budget-windows.md#resets-and-resetat). |
