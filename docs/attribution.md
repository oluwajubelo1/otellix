# Cost Attribution: Who Spent What

A dashboard that says "we spent $612 in the last hour" is a start. Attribution answers the next question: *who* or *what* spent it. Otellix attaches up to three identifiers to every LLM call:

| Identifier | Answers | Set with |
| --- | --- | --- |
| `user_id` | Which end user made the request? | `otellix.WithUserID`, `otellix.ContextWithUser`, or middleware |
| `project_id` | Which tenant, team or customer account? | `otellix.WithProjectID`, `otellix.ContextWithProject`, or middleware |
| `feature_id` | Which product feature, e.g. `support-chat` or `summarise`? | `otellix.WithFeatureID` |

These identifiers end up on spans, on metrics and in budget enforcement. This guide explains how each call gets its identity, and how to find and limit calls that don't have one.

---

## Three ways to attribute a call

### 1. Options on the call

Pass the IDs directly. This is the most explicit approach, and it always wins over the other two:

```go
result, err := otellix.Trace(ctx, provider, params,
    otellix.WithUserID("user_123"),
    otellix.WithProjectID("acme"),
    otellix.WithFeatureID("support-chat"),
)
```

### 2. The context

Put the IDs on the `context.Context` once, and every `Trace` or `TraceStream` call that receives that context picks them up. This saves you passing IDs through every function between your handler and your LLM call:

```go
ctx = otellix.ContextWithUser(ctx, "user_123")
ctx = otellix.ContextWithProject(ctx, "acme")

answer, err := summarise(ctx, doc) // deep inside, otellix.Trace(ctx, ...) is attributed
```

Read them back with `otellix.UserFromContext(ctx)` and `otellix.ProjectFromContext(ctx)`.

### 3. HTTP middleware

Gin, Echo and Fiber middleware set the context for every request automatically, so handlers need no attribution code at all. See [Middleware & Identity](middleware.md) for setup.

### Precedence

For each of `user_id` and `project_id`, Otellix uses the first one it finds:

1. The `WithUserID` / `WithProjectID` option on the call.
2. The value on the context, whether from `ContextWithUser` / `ContextWithProject` or from middleware.
3. Nothing: the call is **unattributed** for that identifier.

The user and project are resolved separately. A call can take its user from an option and its project from the context.

> **Fixed in this release:** `TraceStream` previously ignored the context, so streaming calls never picked up the user or project from middleware or `ContextWithUser`. Per-user and per-project budgets therefore didn't apply to streams. Streams now resolve identity exactly like `Trace`.

---

## Where the identifiers appear

### On spans

Every `llm.call` and `llm.call.stream` span carries:

| Attribute | Value |
| --- | --- |
| `llm.user_id` | The user ID, or `""` |
| `llm.project_id` | The project ID, or `""` |
| `llm.feature_id` | The feature ID, or `""` |
| `llm.user_attributed` | `true` if the call has a user ID, otherwise `false` |

The middleware's `http.request` span also carries `llm.user_id`, `llm.project_id` (when present) and `llm.user_attributed`.

### On metrics

Every metric (`otellix.llm.cost.usd`, `otellix.llm.tokens.input`, `otellix.llm.tokens.output`, `otellix.llm.latency.ms` and so on) has `user_id`, `project_id` and `feature_id` labels. Unattributed calls have an empty label value, `user_id=""`.

#### Metric names in Prometheus

The Prometheus exporter turns the OpenTelemetry names into these Prometheus names. It adds `_total` to counters and a unit suffix where the metric has a unit:

| OpenTelemetry instrument | Prometheus name |
| --- | --- |
| `otellix.llm.cost.usd` | `otellix_llm_cost_usd_total` |
| `otellix.llm.tokens.input` / `.output` | `otellix_llm_tokens_input_total` / `otellix_llm_tokens_output_total` |
| `otellix.llm.tokens.cache_read` / `.cache_write` | `otellix_llm_tokens_cache_read_total` / `otellix_llm_tokens_cache_write_total` |
| `otellix.llm.errors.total` | `otellix_llm_errors_total_total` |
| `otellix.llm.latency.ms` (histogram) | `otellix_llm_latency_ms_milliseconds_bucket`, `_count`, `_sum` |

> **Prometheus 3 keeps the dots by default.** It stores these metrics under their original names, e.g. `otellix.llm.cost.usd_total`, so queries using the underscore names above return nothing. To get the underscore names, which every query in these docs and the bundled dashboard uses, add this to the `global` section of `prometheus.yml`:
>
> ```yaml
> global:
>   metric_name_validation_scheme: legacy
>   metric_name_escaping_scheme: underscores
> ```
>
> Prometheus 2 always uses underscore names. To keep the dotted names instead, quote them in queries: `{"otellix.llm.cost.usd_total"}`.

In Prometheus, cost per user over the last day:

```promql
sum by (user_id) (increase(otellix_llm_cost_usd_total[1d]))
```

Cost per feature:

```promql
sum by (feature_id) (increase(otellix_llm_cost_usd_total[1d]))
```

> **Cardinality:** each distinct `user_id` creates a new time series. That's fine for thousands of users, but with millions it can strain Prometheus. If it does, rely on traces for per-user detail and aggregate metrics by `project_id` or `feature_id`.

### In budgets

`PerUserLimit` is checked against `user_id`, and `PerProjectLimit` against `project_id`. A call with no user ID skips `PerUserLimit` entirely, which is why the next section matters.

---

## Calls with no user

Of all the identifiers, the user is the one teams most often leave off, and it's the one that tells you who to ask about a cost spike. An unattributed call has two problems:

1. **You can't trace it back to a person.** It shows up in totals with no one to follow up with.
2. **It's not covered by `PerUserLimit`.** Per-user limits only apply when there's a user to apply them to.

Common causes:

- Background jobs, cron tasks and queue workers that run outside an HTTP request.
- Routes the middleware isn't registered on, or that run before it.
- Requests without an `Authorization: Bearer` header, such as anonymous or public endpoints.
- With `WithUserFromContext`: an auth middleware that didn't set the key, or set it to something that isn't a string.
- Code that builds a fresh `context.Background()` instead of passing the request's context down.

### Step 1: find unattributed calls

Every span has `llm.user_attributed`. In your tracing backend (Jaeger, Honeycomb, Grafana Tempo and so on), filter on:

```text
llm.user_attributed = false
```

Then group by `llm.feature_id` or `http.route` to see which code paths are responsible.

In Prometheus, the number of calls with no user over the last day:

```promql
sum(increase(otellix_llm_latency_ms_milliseconds_count{user_id=""}[1d])) or vector(0)
```

This is the number to watch over time. A count that suddenly climbs usually means a new code path has started making LLM calls without passing the user. The latency histogram records every call, including failed ones, so its `_count` is a call counter. `or vector(0)` makes the query show `0` instead of "No data" when every call is attributed.

`increase()` measures change between samples, so it can't count the very first call of a new label combination (a new feature, model and project seen with no user). With real traffic this barely matters. With very little traffic, such as just after starting the demo stack, the count can read `0` for a few minutes even though calls with no user exist.

The share of spend with no user over the last day:

```promql
sum(increase(otellix_llm_cost_usd_total{user_id=""}[1d]))
  /
sum(increase(otellix_llm_cost_usd_total[1d]))
```

And which features it comes from:

```promql
sum by (feature_id) (increase(otellix_llm_cost_usd_total{user_id=""}[1d]))
```

#### On the bundled Grafana dashboard

The dashboard in `examples/docker-compose/dashboards/otellix.json`, used by the Docker demo stack, includes two panels for this:

| Panel | Shows | Thresholds |
| --- | --- | --- |
| **Unattributed Calls (last 24h)** | Calls with no user over the previous 24 hours, as a line over time | None: watch the trend |
| **Unattributed Share of Spend (last 24h)** | Percentage of spend with no user | Green below 5%, yellow from 5%, red from 20% |

To add them to your own Grafana, import the dashboard JSON, or copy the two queries above into new panels. The thresholds are starting points: if some unattributed traffic is expected, such as a public demo endpoint, raise them to match your normal level, so the alert only fires on change.

A Prometheus alert on the same signal, for example when the count doubles day over day:

```yaml
- alert: OtellixUnattributedCallsRising
  expr: |
    sum(increase(otellix_llm_latency_ms_milliseconds_count{user_id=""}[1d]))
      > 2 * (sum(increase(otellix_llm_latency_ms_milliseconds_count{user_id=""}[1d] offset 1d)) or vector(0))
    and sum(increase(otellix_llm_latency_ms_milliseconds_count{user_id=""}[1d])) > 50
  labels:
    severity: warning
  annotations:
    summary: "LLM calls with no user doubled in a day; a new code path may not be passing the user"
```

The `> 50` floor stops it firing on tiny numbers, e.g. going from 1 call to 3.

### Step 2: fix what you can

- In background jobs, attribute explicitly: `otellix.WithUserID(job.RequestedBy)`. For system work, use a clear service identity such as `otellix.WithUserID("system:nightly-digest")`.
- Pass the request's context down instead of `context.Background()`.
- Add `otellix.WithFeatureID(...)` everywhere, so even calls with no user are traceable to a feature.

### Step 3: cap the rest with `UnattributedLimit`

Some calls will always lack a user, e.g. a public demo endpoint. `BudgetConfig.UnattributedLimit` puts one **shared** limit on all of them:

```go
budget := &otellix.BudgetConfig{
    PerUserLimit:      2.00,  // each signed-in user, per window
    UnattributedLimit: 20.00, // all calls with no user, combined, per window
    FallbackAction:    otellix.FallbackBlock,
}
```

How it behaves:

- Every call without a user ID adds to, and is checked against, one shared total.
- Calls with a user ID are unaffected and use `PerUserLimit` as normal.
- It uses the same window and store as your other limits (see [Budget Windows](budget-windows.md)).
- `PerProjectLimit` still applies on top, if the call has a project.
- When the shared limit is hit, `FallbackAction` applies as usual: block, downgrade or notify. `BudgetExceededError.UserID` is `""`, and `CurrentSpend` / `Limit` describe the shared budget.
- When `UnattributedLimit` is `0` (the default), unattributed calls skip user-level limits, as they always have.

Because the limit is shared, one noisy anonymous caller can use it up for every other anonymous caller. That's usually what you want, since it caps your exposure. If it isn't, attribute those calls properly.

---

## Trusting the identity

An attribution is only as trustworthy as where it came from. If a caller can choose their own `user_id`, they can also choose whose budget they spend, or make up a new user on every request to get a fresh budget each time.

- **Options and `ContextWithUser`:** you set these in code, so they're as trustworthy as the value you pass in.
- **Middleware defaults:** the JWT `sub` claim is read *without verifying the signature*, and `project_id` comes from a plain request header. These are only safe behind an auth layer that rejects bad tokens before the LLM call runs.
- **Middleware with `WithUserFromContext` / `WithProjectFromContext`:** identity comes from your auth middleware after it verifies the caller. Use this whenever you enforce budgets.

[Middleware & Identity](middleware.md#use-verified-identity-when-you-enforce-budgets) shows the setup for each framework.

---

## Troubleshooting

**`llm.user_id` is empty, but I use the middleware.**
Check that you pass the *request's* context to `Trace`: `c.Request.Context()` in Gin, `c.Request().Context()` in Echo, `c.Context()` in Fiber. In Fiber, passing `c` itself loses the identity. Also check that the middleware is registered before the route runs.

**Streaming calls aren't attributed or budgeted.**
Upgrade: `TraceStream` only reads identity from the context from this release on.

**`UnattributedLimit` blocks my background jobs.**
They're unattributed, so they share that budget. Give them a service identity with `otellix.WithUserID("system:<job-name>")`, which moves them onto `PerUserLimit`, or raise the limit.

**I set both an option and the context. Which wins?**
The option. See [Precedence](#precedence).
