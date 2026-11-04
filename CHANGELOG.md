# Changelog

All notable changes to Otellix are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and Otellix uses [Semantic Versioning](https://semver.org/). For releases before v1.6.0, see the [GitHub releases](https://github.com/oluwajubelo1/otellix/releases).

## [Unreleased]

This release closes several ways around a budget. Four fixes change default behaviour, listed first: read them before upgrading.

### Changed (breaking)

- **Budgets only apply limits to verified identity.** The HTTP middleware read the user from a JWT without checking its signature, and the project from the `X-Project-ID` header, so a caller could send a new user ID with every request to get a fresh per-user budget, or name another tenant's project to spend its budget. Otellix now tracks whether each ID was verified. If a budget limit would depend on an unverified one, the call returns `*otellix.UnverifiedIdentityError` and the provider isn't called. IDs from `WithUserFromContext`, `WithProjectFromContext`, the new `WithVerifiedJWT`, `WithUserID`/`WithProjectID` and `ContextWithUser`/`ContextWithProject` are verified. Unverified IDs still attribute calls on spans and in metrics. See [Verified Identity & Budgets](docs/verified-identity.md).
- **Calls with no user are refused when only per-user limits apply.** A call with no user ID skipped `PerUserLimit`, so a request that left out its token, or sent one that failed verification, wasn't limited at all. If a budget sets `PerUserLimit` but not `UnattributedLimit`, such calls now return `*otellix.UnattributedCallError`. Give background jobs a user ID with `WithUserID`, or set `UnattributedLimit` to cap all calls with no user together.
- **The middleware no longer reads `X-Project-ID` by default.** Read the project from your auth with `WithProjectFromContext`, or opt back in, unverified, with `WithProjectHeader("X-Project-ID")`.
- **Request deduplication keeps results within a user by default.** Identical requests from different users, including across tenants, were served each other's results. The new `CacheConfig.Scope` defaults to `CacheScopeUser`; `CacheScopeProject` shares within a project, and `CacheScopeGlobal` keeps the old behaviour. See [Request Deduplication](docs/request-deduplication.md#who-shares-results-scope).
- With the Fiber middleware, upgrade `middleware/fiber` to v0.2.0 alongside this release. Earlier Fiber versions can't mark IDs as verified, so budgets treat all their IDs as unverified.

### Fixed

- **Concurrent streams could overshoot a budget.** A stream held only its estimate, and what it spent beyond that was invisible to other calls until it finished: 5 streams at once against a $0.05 limit spent $0.25. A stream now reserves more, atomically, as soon as it runs past what it holds, so concurrent streams can't overshoot together. This needs no new store method.
- **Streams read to the end without `Close()` weren't charged.** A stream is now finished, and its cost recorded, at `io.EOF` or on an error. A stream that fails part way is charged for what it streamed, and its span marked as an error.
- **SDK responses without usage cost $0.** Some OpenAI-compatible servers and proxies leave `usage` out, and the SDK middleware recorded zero tokens. It now charges an estimate and sets `llm.usage_estimated` on the span.
- The dedup cache key left out `Temperature` and `Extra`, so a request with tools or a response format could be served the result of one without. Both are now in the key, and the key's fields can no longer run into each other.
- **Redis instances with different windows deleted each other's spend.** The Redis store deleted spend from before its own window's start, so an instance whose calendar day started later, such as one in a time zone further east, deleted spend that other instances still counted: $1.00 dropped to $0.00 in a test. It now only deletes spend no window could still count, and reports instances whose windows differ through the OpenTelemetry error handler. A Redis store whose calendar window uses `time.Local` also reports a warning. See [Every instance needs the same window](docs/budget-windows.md#every-instance-needs-the-same-window).
- **Streamed calls were missing from `GetBreakdown` and `GetForecast`,** although budgets counted them.

### Changed

- `GetForecast` uses the calendar month in UTC, not the server's local time zone, so every server agrees on when the month starts. Use the new `GetForecastIn` for another zone.
- For rolling windows, `BudgetExceededError.ResetAt` is in UTC rather than the server's local time zone. Calendar windows still give it in the window's zone. See [Showing reset times to users](docs/budget-windows.md#showing-reset-times-to-users).

### Added

- `UnattributedCallError`, returned for calls with no user under a budget with per-user limits and no `UnattributedLimit`.
- `WithVerifiedJWT(keyFunc, opts...)` middleware option, which checks the JWT's signature and expiry before reading the user.
- `otellix.ContextWithUnverifiedUser` and `otellix.ContextWithUnverifiedProject`, for your own middleware to attribute calls from IDs callers control.
- `llm.user_verified` and `llm.project_verified` span attributes, and `llm.usage_estimated` for calls whose usage was estimated.
- `CacheConfig.Scope`, with `CacheScopeUser`, `CacheScopeProject` and `CacheScopeGlobal`.
- `BudgetConfig.UserStore` and `BudgetConfig.ProjectStore`, so user and project limits can have different windows: for example, a rolling 24 hours for users, which is fair in every time zone, and a calendar month for projects. See [Users in many time zones](docs/budget-windows.md#users-in-many-time-zones).
- `GetForecastIn(key, loc)`, a forecast for the calendar month in a given time zone.
- `BudgetWindow.String()` and `BudgetWindow.Location()`.
- A [Spend Analytics](docs/analytics.md) guide for `GetBreakdown`, `GetForecast` and `GetOptimizationSuggestions`.

## [1.7.0] - 2026-10-24

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

[Unreleased]: https://github.com/oluwajubelo1/otellix/compare/v1.7.0...HEAD
[1.7.0]: https://github.com/oluwajubelo1/otellix/compare/v1.6.1...v1.7.0
[1.6.1]: https://github.com/oluwajubelo1/otellix/compare/v1.6.0...v1.6.1
[1.6.0]: https://github.com/oluwajubelo1/otellix/compare/v1.5.0...v1.6.0
