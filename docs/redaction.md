# Content Capture & PII Redaction

By default Otellix records **token counts and cost only** — never the prompt or
response text. Content capture is opt-in, and when enabled every captured string
is passed through a redaction pipeline first.

## Enabling capture

```go
cc := &otellix.CaptureConfig{
    CaptureInput:  true,                       // capture system prompt + last user message
    CaptureOutput: true,                       // capture the model response
    RedactPatterns: []string{
        `\b\d{10}\b`,                          // phone numbers
        `[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}`, // emails
        `\b\d{16}\b`,                          // card numbers
    },
}

_, err := otellix.Trace(ctx, provider, params,
    otellix.WithCaptureContent(cc),
)
```

Captured content is stored as standardized GenAI semantic attributes:

| Attribute | Content |
|---|---|
| `llm.prompt` | The system prompt (redacted) |
| `gen_ai.content.user` | The last user message (redacted) |
| `llm.response` | The model response (redacted) |

## How redaction works

- Each configured pattern is compiled as a regex.
- Matches are replaced with `[REDACTED]`.
- A default high-signal pattern set is applied even if you supply none
  (email addresses, 10-digit phone numbers, 16-digit card numbers).
- If a pattern fails to compile, Otellix logs a warning and skips it — a bad
  pattern never breaks tracing.

> **Security note**: capturing prompt/response text can leak sensitive data into
> your trace backend. Only enable capture on non-sensitive routes, or rely on the
> redaction patterns to scrub PII before it hits the wire.
