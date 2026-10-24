# Native Real-Time Streaming

Otellix supports native Go 1.23 iterators for real-time streaming, allowing your application to provide responsive LLM experiences while maintaining full observability and budget safety.

## The Iterator Pattern

Otellix uses a simple, pull-based iterator interface. This ensures that your application only processes tokens when it is ready, avoiding buffer overflows and keeping resource usage low.

```go
stream, err := otellix.TraceStream(ctx, provider, params)
if err != nil {
    log.Fatal(err)
}
defer stream.Close()

for {
    evt, err := stream.Recv()
    if err != nil {
        if err == io.EOF {
            break
        }
        log.Fatal(err)
    }
    fmt.Print(evt.Token) // Watch it stream live...
}
```

## Token Tracking

Unlike static calls where tokens are reported as a single number at the end, `TraceStream` accumulates tokens in real-time. Each `StreamEvent` returned by `Recv()` contains the cumulative token count and cost estimate.

```go
type StreamEvent struct {
    Token        string
    InputTokens  int64
    OutputTokens int64
    CostUSD      float64
}
```

## Real-Time Mid-Stream Cutoff

A unique feature of Otellix is the ability to **abort a stream mid-flight** if a budget limit is reached. This is especially useful for preventing runaway generation costs for untrusted users.

### How it works:
1.  **Before start**: Otellix estimates the stream's cost from the prompt and expected output, and reserves it against the budget. If there isn't room for the estimate, the stream never starts. See [Budget Reservations & Cost Estimates](budget-reservations.md).
2.  **During start**: Otellix starts the LLM stream.
3.  **During Recv**: On every `Recv()` call, Otellix calculates the cumulative cost based on the tokens received so far, including prompt-cache reads and writes. It compares that with the headroom it last read from the budget store, and re-reads the store only when the headroom would run out or once a second, not on every token.
4.  **If budget is hit**: Otellix closes the underlying provider stream and returns a `BudgetExceededError`.
5.  **On `Close()`**: the actual cost is recorded and the reservation released. Always close streams, usually with `defer stream.Close()`.

### Why this matters:
Traditional SDKs only track costs *after* the stream finishes. A single malicious or long-running prompt can cost significantly more than the user’s daily limit before you have a chance to stop it. Otellix stops the leak **before it happens**.

## Attribution and Budgets for Streams

`TraceStream` resolves the user and project exactly like `Trace`: from `otellix.WithUserID` / `otellix.WithProjectID` options first, then from the context. Contexts set by `otellix.ContextWithUser` / `otellix.ContextWithProject`, or by the [Gin, Echo or Fiber middleware](middleware.md), are picked up automatically:

```go
// In a Gin handler. The middleware already put the user on the request context.
stream, err := otellix.TraceStream(c.Request.Context(), provider, params,
    otellix.WithBudgetConfig(budget),
)
```

This means `PerUserLimit`, `PerProjectLimit` and `UnattributedLimit` all apply to streams, both in the pre-start check and in the mid-stream cutoff. Stream spans also carry `llm.user_attributed`. See [Cost Attribution](attribution.md).

> **Fixed in this release:** earlier versions of `TraceStream` ignored the context, so streams were only attributed (and budgeted) when you passed `WithUserID` / `WithProjectID` explicitly.

## Performance Considerations

*   **Zero-Dependency Parsing**: Our **Ollama** provider uses a raw HTTP NDJSON parser instead of a heavy SDK, making it extremely fast for local development and edge deployments.
*   **Minimal Overhead**: The streaming wrapper adds negligible latency to the `Recv()` call, ensuring that your user’s UI remains snappy.
*   **Context Support**: Full support for `context.Context` ensures that if your user closes their browser tab or cancels the request, the underlying LLM stream is terminated immediately to save costs.
