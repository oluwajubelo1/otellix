# SDK Middleware: OpenAI & Anthropic

Already calling the official OpenAI or Anthropic Go SDK? Add one option when you create the client, and every call it makes is traced, costed and checked against your budgets. You don't change the code that makes the calls.

```go
client := openai.NewClient(
    option.WithMiddleware(otellixopenai.Middleware(otellix.WithBudgetConfig(budget))),
)

// Unchanged: now traced, costed and budgeted.
resp, err := client.Chat.Completions.New(ctx, params)
```

Related guides:

- [Budget Guardrails](budget-guardrails.md): limits and fallback actions.
- [Cost Attribution](attribution.md): how calls get a user and project.
- [Middleware & Identity](middleware.md): setting the user from Gin, Echo or Fiber.
- [Streaming](streaming.md): budgets for streamed responses.

---

## When to use it

Otellix gives you two ways to trace calls:

| | SDK middleware (this guide) | `otellix.Trace` with a provider |
| --- | --- | --- |
| Code changes | One option on the client | Each call goes through `otellix.Trace` |
| API surface | The full SDK: tools, images, structured output, every parameter | Otellix's `CallParams` |
| Providers | OpenAI, Anthropic | OpenAI, Anthropic, Gemini, Ollama, your own |
| Tracing, cost, budgets | Yes | Yes |

If you already use an SDK, use the middleware. If you're starting fresh, or want one interface for several providers, use `otellix.Trace`.

---

## Setup

### OpenAI

```go
import (
    "github.com/openai/openai-go"
    "github.com/openai/openai-go/option"

    "github.com/oluwajubelo1/otellix"
    otellixopenai "github.com/oluwajubelo1/otellix/providers/openai"
)

budget := &otellix.BudgetConfig{
    PerUserLimit:   5.00,
    FallbackAction: otellix.FallbackBlock,
}

client := openai.NewClient(
    option.WithMiddleware(otellixopenai.Middleware(
        otellix.WithBudgetConfig(budget),
        otellix.WithFeatureID("support-bot"),
    )),
)
```

### Anthropic

```go
import (
    "github.com/anthropics/anthropic-sdk-go"
    "github.com/anthropics/anthropic-sdk-go/option"

    "github.com/oluwajubelo1/otellix"
    otellixanthropic "github.com/oluwajubelo1/otellix/providers/anthropic"
)

client := anthropic.NewClient(
    option.WithMiddleware(otellixanthropic.Middleware(
        otellix.WithBudgetConfig(budget),
    )),
)
```

`Middleware` takes the same options as `otellix.Trace`: `WithBudgetConfig`, `WithFeatureID`, `WithProjectID`, `WithAttributes`, `WithAnomalyDetection` and so on. They apply to every call the client makes. Create the client, and its `BudgetConfig`, once at startup and share it.

You can also add the middleware to a single request instead of the whole client, by passing `option.WithMiddleware(...)` as a request option.

A runnable example: [`examples/sdk-middleware`](../examples/sdk-middleware/main.go). It runs without an API key, against a local fake API.

---

## Which calls are traced

| Provider | Endpoint | SDK method | Streaming |
| --- | --- | --- | --- |
| OpenAI | `POST /chat/completions` | `client.Chat.Completions.New` / `NewStreaming` | Yes |
| OpenAI | `POST /responses` | `client.Responses.New` / `NewStreaming` | Yes |
| Anthropic | `POST /v1/messages` | `client.Messages.New` / `NewStreaming` | Yes |

Every other request (embeddings, files, token counting, listing models and so on) passes through untouched and untraced.

---

## Who a call is attributed to

The middleware runs inside the SDK, where there's no per-call option to set a user. It takes the user and project from the **request's context** instead:

```go
ctx := otellix.ContextWithUser(r.Context(), "user_123")
ctx = otellix.ContextWithProject(ctx, "acme")

resp, err := client.Chat.Completions.New(ctx, params)
```

In a web app, the [Gin, Echo or Fiber middleware](middleware.md) sets these for you. Pass the request's context to the SDK call and attribution follows.

You can also fix a user or project for the whole client with `otellix.WithUserID(...)` or `otellix.WithProjectID(...)` in `Middleware(...)`, which suits a client used for one tenant or a background job. See [Cost Attribution](attribution.md#precedence) for how options and context combine.

---

## Budgets

Budgets work as they do for `otellix.Trace`: the estimated cost is [reserved](budget-reservations.md) before the request is sent, and the actual cost from the response's `usage` is recorded after.

When a call is blocked, the SDK call returns Otellix's error. The request is never sent, and the SDK doesn't retry it:

```go
resp, err := client.Chat.Completions.New(ctx, params)

var budgetErr *otellix.BudgetExceededError
if errors.As(err, &budgetErr) {
    // Not sent, not retried, nothing spent.
    return fmt.Errorf("you've used your $%.2f AI budget; it resets at %s", budgetErr.Limit, budgetErr.ResetAt)
}
```

The same applies to `*otellix.UnpricedModelError` (see [Unpriced Models](unpriced-models.md)) and `*otellix.AnomalyBlockedError`.

`FallbackDowngrade` works too: the middleware rewrites the `model` field of the request the SDK sends to `FallbackModel`, leaving the rest of the request as it was. The response then comes from the cheaper model, so `resp.Model` shows which one answered.

### How the estimate is made

The middleware reads the request body to estimate the call before it's sent:

- **Input:** the text of the system prompt (`system` / `instructions`) and every message. Images, files and tool definitions aren't counted.
- **Output:** `max_tokens` / `max_completion_tokens` / `max_output_tokens` caps the assumed output, as described in [Budget Reservations](budget-reservations.md#how-the-estimate-is-worked-out).

---

## Streaming

Streamed calls are traced as the SDK reads them. Each event passes through Otellix before the SDK sees it, so a stream that runs over budget is **cut off mid-stream**: the SDK stream stops, and `stream.Err()` returns the `BudgetExceededError`.

```go
stream := client.Chat.Completions.NewStreaming(ctx, params)
for stream.Next() {
    // ...
}
var budgetErr *otellix.BudgetExceededError
if errors.As(stream.Err(), &budgetErr) {
    // Stopped part way because the budget ran out.
}
stream.Close()
```

Always `Close()` the stream. That's when the call's cost is recorded and its reservation released.

### Usage in streams

| API | Usage reported? | What Otellix records |
| --- | --- | --- |
| Anthropic Messages | Always, at the start (input) and end (output) | Exact |
| OpenAI Responses | Always, in `response.completed` | Exact |
| OpenAI Chat Completions | **Only** with `stream_options.include_usage` | Exact with it, estimated without |

For Chat Completions, ask for usage so costs are exact:

```go
params.StreamOptions = openai.ChatCompletionStreamOptionsParam{IncludeUsage: openai.Bool(true)}
```

Without it, Otellix estimates input from the request and output from the streamed text, at about 4 characters per token. That keeps budgets working, but the cost is approximate. The extra usage chunk has an empty `Choices`, so check `len(chunk.Choices) > 0` before reading it.

While a stream runs, its cost so far is estimated from the text streamed, so the mid-stream cutoff works even before the provider reports usage.

---

## Errors and retries

| What happens | What the SDK returns | Traced as |
| --- | --- | --- |
| Call succeeds | The response, unchanged | A successful span with tokens and cost |
| Provider returns an error status (400, 429, 500, ...) | The SDK's own `*openai.Error` / `*anthropic.Error` | An error span; nothing charged |
| Network error | The network error | An error span; the SDK retries as usual |
| Budget, unpriced model or anomaly block | Otellix's error | No span; not sent, not retried |

The SDK retries some failures, such as 429 and 500 responses. The middleware runs once per attempt, so each attempt gets its own span, and only the attempt that succeeds is charged.

---

## Using it with other middleware

- **`otelhttp`:** if you also trace HTTP with `otelhttp`, its HTTP span becomes a child of Otellix's `llm.call` span, as the middleware passes its span's context on.
- **Request deduplication** (`WithRequestCache`) is turned off for SDK middleware. The SDK needs a real HTTP response, which a cached result can't provide.
- **Several middlewares:** order matters as usual. Put Otellix's middleware first, so a blocked call never reaches the others.

---

## Troubleshooting

**Calls aren't traced.**
Check the endpoint is in [the table above](#which-calls-are-traced), and that the middleware is on the client or request that makes the call. A request whose body can't be read as JSON is passed through untraced, rather than broken.

**Calls show `llm.pricing_missing` or cost $0.**
The `model` in the request has no price. Dated model names, such as `gpt-4o-2024-08-06`, need registering with `otellix.RegisterModel`. With **Azure OpenAI**, the model is in the URL, not the body, so set it with `otellix.WithModel("gpt-4o")` in `Middleware(...)`. See [Unpriced Models](unpriced-models.md).

**Streamed Chat Completions costs look slightly off.**
They're estimated. Set `stream_options.include_usage`, as above.

**Calls aren't attributed to a user.**
Pass the request's context, not `context.Background()`, to the SDK call. See [Who a call is attributed to](#who-a-call-is-attributed-to).
