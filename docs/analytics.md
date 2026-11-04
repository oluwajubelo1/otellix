# Spend Analytics

Otellix keeps a record of recent calls in memory, so you can ask it questions without a metrics backend: where the money goes, what this month will cost, and which models you could switch to.

```go
for _, e := range otellix.GetBreakdown(otellix.ByFeature) {
    fmt.Printf("%-20s $%.2f over %d calls\n", e.Key, e.TotalCostUSD, e.CallCount)
}

forecast := otellix.GetForecast("")
fmt.Printf("This month: $%.2f so far, $%.2f projected\n", forecast.CurrentMonthSpend, forecast.ProjectedMonthSpend)
```

Related guides:

- [Cost Attribution](attribution.md): the same questions answered with Prometheus, across every instance.
- [Budget Windows](budget-windows.md): calendar months and time zones for budgets.

---

## What's recorded

Every call made through `otellix.Trace` or `otellix.TraceStream`, including calls through the [SDK middleware](sdk-middleware.md), is recorded once it finishes, with its provider, model, feature, user, project, tokens, cost and latency.

- Calls that fail aren't recorded, except streams that fail part way, which are recorded with what they streamed.
- Requests served by [request deduplication](request-deduplication.md) without calling the provider aren't recorded: they cost nothing.

> **Fixed in v1.8.0:** streamed calls weren't recorded, so breakdowns and forecasts left out all streamed spend, although budgets counted it.

### Limits: read this before relying on it

The record is **in memory, in each process, for the last 10,000 calls**:

- **It's lost on restart.**
- **Each instance of your app has its own.** With three instances, each sees about a third of your calls.
- **It holds 10,000 calls at most.** A busy app makes 10,000 calls in hours or minutes, after which "this month" only covers those recent calls.

That makes it a good fit for development, scripts, single-instance apps and admin pages on modest traffic. For production totals across instances and restarts, use the metrics Otellix exports. See [Using Prometheus instead](#using-prometheus-instead).

---

## Breakdowns: where the money goes

`GetBreakdown` totals the recorded calls by one dimension, most expensive first:

```go
for _, e := range otellix.GetBreakdown(otellix.ByUser) {
    fmt.Printf("%q: $%.4f, %d calls, %d in / %d out tokens\n",
        e.Key, e.TotalCostUSD, e.CallCount, e.InputTokens, e.OutputTokens)
}
```

| Dimension | Groups by |
| --- | --- |
| `otellix.ByUser` | User ID |
| `otellix.ByProject` | Project ID |
| `otellix.ByFeature` | Feature ID, from `WithFeatureID` |
| `otellix.ByModel` | Model |
| `otellix.ByProvider` | Provider |

Calls without a value for the dimension, such as calls with no user, are grouped under `""`.

---

## Forecasts: what this month will cost

`GetForecast` projects this month's spend from the spend so far:

```go
f := otellix.GetForecast("project:acme")
fmt.Printf("$%.2f spent in %d days, $%.2f a day, $%.2f projected for the month (%d days left)\n",
    f.CurrentMonthSpend, f.DaysElapsedInMonth, f.DailyBurnRate, f.ProjectedMonthSpend, f.DaysRemainingInMonth)
```

The key chooses what to forecast:

| Key | Forecasts |
| --- | --- |
| `""` | Every recorded call |
| `"user:<id>"` | One user's calls |
| `"project:<id>"` | One project's calls |

It's a straight line: the spend this month, divided by the days so far (today counts as a whole day), times the days in the month. Early in the month, a single busy day moves it a lot.

### Which month

`GetForecast` uses the calendar month in **UTC**, the same default as budget windows. For another time zone, use `GetForecastIn`:

```go
lagos, _ := time.LoadLocation("Africa/Lagos")

f := otellix.GetForecastIn("project:acme", lagos)
```

If you have a monthly budget, pass its time zone, so the forecast and the budget agree on when the month starts:

```go
window := otellix.CalendarMonth(lagos)
store := otellixredis.NewRedisBudgetStoreWithWindow(rdb, "otellix:budget", window)

f := otellix.GetForecastIn("project:acme", window.Location())
```

> **Changed in v1.8.0:** `GetForecast` used the server's local time zone, so the month started at different moments on different servers, and not necessarily when your budget's month did.

---

## Optimization suggestions

`GetOptimizationSuggestions` looks at the recorded calls for each model and finds the model in the [pricing table](providers.md) that would have cost least for the same average number of input and output tokens:

```go
report := otellix.GetOptimizationSuggestions()
fmt.Printf("Spend: $%.2f, possible savings: $%.2f\n", report.TotalSpend, report.PotentialSavings)

for _, s := range report.Suggestions {
    fmt.Printf("%s/%s -> %s/%s: %.0f%% cheaper, $%.2f over %d calls\n",
        s.CurrentProvider, s.CurrentModel, s.SuggestedProvider, s.SuggestedModel,
        s.EstimatedSavingsPct, s.EstimatedSavingsUSD, s.CallCount)
}
```

It only suggests a model that's more than 10% cheaper, and only one per model you use, the cheapest.

> **It compares prices, not quality.** The cheapest model is often far less capable, and may come from a provider you don't use. Treat suggestions as a list of candidates to evaluate on your own prompts, not as drop-in replacements.

---

## Using Prometheus instead

With the [Prometheus exporter](attribution.md#metric-names-in-prometheus), the same questions can be answered across every instance, with no 10,000-call limit:

```promql
# Breakdown: spend per feature over the last 7 days
sum by (feature_id) (increase(otellix_llm_cost_usd_total[7d]))

# Daily burn rate, averaged over the last 7 days
sum(increase(otellix_llm_cost_usd_total[7d])) / 7

# A 30-day projection from that rate
sum(increase(otellix_llm_cost_usd_total[7d])) / 7 * 30
```

---

## Testing

`otellix.ResetCallRecords()` clears the record. Call it at the start of tests that check breakdowns or forecasts, so calls from other tests don't leak in:

```go
otellix.ResetCallRecords()
t.Cleanup(otellix.ResetCallRecords)
```

---

## Troubleshooting

**The forecast is lower than my bill.**
The record only holds the last 10,000 calls in this process, and each instance has its own. Use the [Prometheus queries](#using-prometheus-instead) for totals.

**The forecast's month starts at a strange hour.**
`GetForecast` uses UTC. Use `GetForecastIn` with your time zone.

**Streamed calls are missing.**
Upgrade to v1.8.0 or later.

**Everything reset to zero.**
The process restarted. The record lives in memory.
