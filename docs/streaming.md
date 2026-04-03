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
1.  **Before start**: Otellix checks if the user/project has *any* remaining budget. If they are already over, the stream never starts.
2.  **During start**: Otellix starts the LLM stream.
3.  **During Recv**: On every `Recv()` call, Otellix calculates the cumulative cost based on the tokens received so far.
4.  **If budget is hit**: Otellix closes the underlying provider stream and returns a `BudgetExceededError`.

### Why this matters:
Traditional SDKs only track costs *after* the stream finishes. A single malicious or long-running prompt can cost significantly more than the user’s daily limit before you have a chance to stop it. Otellix stops the leak **before it happens**.

## Performance Considerations

*   **Zero-Dependency Parsing**: Our **Ollama** provider uses a raw HTTP NDJSON parser instead of a heavy SDK, making it extremely fast for local development and edge deployments.
*   **Minimal Overhead**: The streaming wrapper adds negligible latency to the `Recv()` call, ensuring that your user’s UI remains snappy.
*   **Context Support**: Full support for `context.Context` ensures that if your user closes their browser tab or cancels the request, the underlying LLM stream is terminated immediately to save costs.
