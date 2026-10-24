# Metric Labels & Cardinality

Otellix's metrics carry labels that say who spent what: `user_id`, `project_id` and `feature_id`. This guide explains why the `user_id` label can overload your metrics backend as you grow, and how to turn it off without losing per-user detail.

Related guides:

- [Cost Attribution](attribution.md): where user and project IDs come from.
- [Architecture](architecture.md): how spans and metrics are exported.

---

## The labels

Every Otellix metric (`otellix.llm.cost.usd`, `otellix.llm.tokens.input`, `otellix.llm.tokens.output`, `otellix.llm.latency.ms`, `otellix.llm.tokens.cache_read` and `otellix.llm.tokens.cache_write`) has these labels:

| Label | Values | Can be turned off? |
| --- | --- | --- |
| `provider` | e.g. `openai` | No |
| `model` | e.g. `gpt-4o` | No |
| `user_attributed` | `true` or `false`: whether the call had a user ID | No |
| `feature_id` | your feature names | Yes |
| `user_id` | one value per user | Yes |
| `project_id` | one value per project | Yes |

All are on by default.

## Why `user_id` can be a problem

Prometheus, and most metrics backends, store one **time series** per combination of label values. Each new user creates a new series for every metric and every model they use:

```
series ≈ users × models × features × 6 metrics (+ latency histogram buckets)
```

With 100 users that's nothing. With 100,000 users it's millions of series, which slows queries, uses a lot of memory, and can crash Prometheus or run up a large bill on a hosted backend.

Traces don't have this problem. Each span is stored on its own, so a span attribute with a million different values is fine. That's where per-user detail belongs at scale.

## Turning `user_id` off

Call `SetMetricLabels` once at startup, before any LLM calls:

```go
otellix.SetMetricLabels(otellix.MetricLabels{
    ProjectID: true,
    FeatureID: true,
    // UserID left false: no user_id label
})
```

Fields left out are turned off, so this keeps `project_id` and `feature_id` and drops `user_id`.

After this:

- **Metrics** no longer have a `user_id` label. Totals, per-project and per-feature breakdowns work as before.
- **Spans** still have `llm.user_id`, so you can find any user's calls and cost in your tracing backend.
- **Budgets** are unaffected. They use the budget store, not metrics.
- **Unattributed calls** can still be counted with the `user_attributed` label.

`project_id` can grow large too if you have very many tenants. Turn it off the same way if needed.

### When to turn it off

As a rough guide, turn `user_id` off once you have more than about 10,000 active users, or sooner on a hosted backend that charges per series. If you're not sure, check how many series Otellix creates:

```promql
count({__name__=~"otellix_.*"})
```

---

## Updating your queries

Queries that group or filter by `user_id` stop working once it's off. Replace them like this:

| Before | After |
| --- | --- |
| `sum by (user_id) (otellix_llm_cost_usd_total)` | Use traces: group spans by `llm.user_id` in your tracing backend |
| `otellix_llm_cost_usd_total{user_id=""}` | `otellix_llm_cost_usd_total{user_attributed="false"}` |
| `otellix_llm_cost_usd_total{user_id!=""}` | `otellix_llm_cost_usd_total{user_attributed="true"}` |

Use `user_attributed` even if you keep `user_id` on. In Prometheus, `{user_id=""}` also matches series that have **no** `user_id` label at all, so after turning it off, that filter would match every call and report all spend as unattributed.

The bundled Grafana dashboard (`examples/docker-compose/dashboards/otellix.json`) uses `user_attributed` for its unattributed panels, so they work either way. Its "Cost per user" panel needs `user_id` and is empty when it's off.

---

## Troubleshooting

**Some series still have `user_id` after I turned it off.**
`SetMetricLabels` applies to metrics recorded after it's called. Series recorded earlier stay in Prometheus until they age out. Call it at startup, before any calls.

**The "unattributed" panels show 100% of spend.**
They filter on `user_id=""`, which matches series with no `user_id` label. Switch them to `user_attributed="false"`, as above.

**The `user_attributed` label is missing on old data.**
It was added in this release. Data recorded by earlier versions doesn't have it.
