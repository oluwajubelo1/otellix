# Changelog

All notable changes to Otellix are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and Otellix uses [Semantic Versioning](https://semver.org/). For releases before v1.6.0, see the [GitHub releases](https://github.com/oluwajubelo1/otellix/releases).

## [Unreleased]

### Added

- **SDK middleware for the official OpenAI and Anthropic Go SDKs.** `otellixopenai.Middleware(...)` and `otellixanthropic.Middleware(...)` plug into `option.WithMiddleware`, so existing SDK calls are traced, costed and budgeted with no change to the code making them. They cover OpenAI Chat Completions and Responses, and Anthropic Messages, streaming or not. Blocked calls return Otellix's error without being sent or retried, streams are cut off mid-stream when the budget runs out, and `FallbackDowngrade` rewrites the model in the request. See [SDK Middleware](docs/sdk-middleware.md).

## [1.6.1] - 2026-10-24

### Fixed

- **Streams read the budget store on every token.** The mid-stream budget check made two store reads per token, which with Redis meant thousands of network round trips for one long stream. It now reads spend on the first token and re-reads only when the stream's cost would use up the headroom it saw, or once a second. The cutoff is still exact for a single stream.
- **OpenAI cached tokens were charged twice.** OpenAI's `prompt_tokens` includes cached tokens, and the OpenAI provider reported them both as input (full price) and as cache reads. Input now excludes them, so cached prompts cost what OpenAI bills.
- Streams now count prompt-cache tokens. They were ignored, so streamed calls with Anthropic or OpenAI prompt caching were costed as if nothing was cached.

### Added

- `CacheReadTokens` and `CacheWriteTokens` on `providers.StreamEvent`, filled in by the Anthropic and OpenAI providers.

## [1.6.0] - 2026-10-24

### Fixed

- **Concurrent calls could overshoot a budget.** The budget check read spend, called the provider, then recorded spend, so calls running at the same time all saw the same spend and all went ahead. In a test of 20 simultaneous calls against a limit with room for 3, all 20 ran and spent almost 6× the limit. Budgets now reserve each call's estimated cost atomically before the call and settle to the actual cost after it, so exactly 3 run. See [Budget Reservations](docs/budget-reservations.md).

### Added

- `BudgetReserver` interface, implemented by `InMemoryBudgetStore` and `RedisBudgetStore`, for stores that can check and hold spend in one atomic step. In Redis this is a single Lua script, so it holds across every instance of your app.
- `BudgetEnforcer.Reserve` and `Reservation`, for using reservations directly.
- `EstimateCallCost`, which estimates a call's cost from its prompt and expected output.
- `BudgetConfig.EstimatedOutputTokens` and `BudgetConfig.ReserveWorstCase` to tune the pre-call estimate.
- `BudgetConfig.UnpricedModel` (`UnpricedAllow`, `UnpricedBlock`), `BudgetConfig.UnpricedModelPrice` and `UnpricedModelError`, for calls to models with no pricing. See [Unpriced Models](docs/unpriced-models.md).
- `llm.pricing_missing` span attribute, `otellix.llm.calls.unpriced` metric, and a once-per-model warning through the OpenTelemetry error handler when a model has no pricing.
- `SetMetricLabels` to drop the `user_id` (or `project_id`, `feature_id`) metric label, which can overload Prometheus with many users. See [Metric Labels & Cardinality](docs/metric-labels.md).
- `user_attributed` metric label (`true`/`false`) on every metric.

### Changed

- The pre-call cost estimate now counts the prompt's input tokens, and caps assumed output at `params.MaxTokens`. Earlier versions assumed 500 input and 500 output tokens for every call. **Long prompts can now be blocked by a budget they used to pass.**
- `TraceStream` now reserves the estimated cost before a stream starts, as `Trace` does. Earlier versions only checked that spend was under the limit, so **a stream now needs room for its estimate to start**.
- The bundled Grafana dashboard's unattributed panels filter on `user_attributed="false"` instead of `user_id=""`.
- `BudgetExceededError.CurrentSpend` and `BudgetStatus.Usage` include the holds of calls still in flight.

[Unreleased]: https://github.com/oluwajubelo1/otellix/compare/v1.6.1...HEAD
[1.6.1]: https://github.com/oluwajubelo1/otellix/compare/v1.6.0...v1.6.1
[1.6.0]: https://github.com/oluwajubelo1/otellix/compare/v1.5.0...v1.6.0
