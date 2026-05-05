# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Otellix** is an OpenTelemetry-native observability library for Go that provides real-time LLM cost tracking, token counting, and budget enforcement. It wraps LLM API calls from multiple providers (OpenAI, Anthropic, Google Gemini, Ollama) with automatic tracing, cost attribution, and budget guardrails.

**Key positioning**: Prevents "bill shock" by enforcing costs at the application layer before they happen, not after billing alerts arrive.

## Common Commands

```bash
# Run tests with coverage
go test -v -race -coverprofile=coverage.out ./...

# Run linter (golangci-lint must be installed)
golangci-lint run ./...

# Format code
gofmt -w .

# Run a specific example
ANTHROPIC_API_KEY=your_key go run examples/realtime-stream/main.go

# Run the Docker demo stack (Prometheus + Grafana)
curl -sL https://raw.githubusercontent.com/oluwajubelo1/otellix/main/scripts/demo.sh | bash

# For local development with stdout tracing
go run examples/simple-trace/main.go
```

## Architecture & Core Components

### 1. Provider Interface Pattern (`providers/provider.go`)
All LLM providers implement the `Provider` interface with two methods:
- `Call(ctx, params) → CallResult`: Non-streaming LLM requests
- `Stream(ctx, params) → Stream`: Streaming requests with token-by-token feedback

This abstraction decouples business logic from provider SDKs. All providers standardize their responses into a `CallResult` struct containing token counts, cache metrics, model used, and raw response for inspection.

**Supported providers** live in `providers/{anthropic,openai,gemini,ollama}/` — each wraps its SDK and maps provider-specific fields (e.g., `prompt_tokens` vs `input_token_count`) to standardized names.

### 2. Main Tracing Entry Points (`otellix.go`, `tracer.go`)
- `otellix.Trace(ctx, provider, params, ...opts)`: Wraps a single LLM call in an OpenTelemetry span
- `otellix.TraceStream(ctx, provider, params, ...opts)`: Streams tokens via Go iterators while tracking usage and enforcing budget

Both functions:
- Create OTel spans with standardized attributes (`llm.input_tokens`, `llm.cost_usd`, `llm.latency_ms`, etc.)
- Populate metrics instruments (counters for tokens/cost, histogram for latency)
- Apply budget enforcement if configured
- Support context-based user/project attribution via `otellix.ContextWithUser()` / `otellix.ContextWithProject()`

### 3. Budget System (`budget.go`)
The `BudgetEnforcer` checks spend limits **before and during** calls. Enforcement strategies:
- **InMemoryStore**: Per-process limits (development, single-instance services)
- **RedisStore**: Distributed limits (multi-instance, high-availability clusters)

Budget blocks calls if `current_spend + estimated_call_cost > limit`.

### 4. Cost Calculation (`cost.go`)
Maintains a pricing dictionary mapping `(provider, model) → (cost_per_input_token, cost_per_output_token)`. Extended by user-provided pricing maps. Cache token metrics are tracked separately (`CacheReadTokens`, `CacheWriteTokens`) to compute savings from Anthropic/OpenAI prompt caching.

### 5. Middleware & Auto-Attribution (`middleware/`, `integrations/`)
Two integration patterns:
- **Web Middleware** (`middleware/{gin,echo}.go`): Intercepts HTTP requests to extract `user_id`/`project_id` from headers, sessions, or JWT claims. Populates context so downstream LLM calls automatically inherit attribution without manual plumbing.
- **LangChainGo Integration** (`integrations/langchaingo/`): Implements LangChainGo's `callbacks.Handler` interface to capture chain events, cost, and spans.

## Directory Structure

```
.
├── otellix.go              # Main entry point: SetupDev(), RegisterDevPrinter()
├── tracer.go               # Trace() and TraceStream() implementations
├── budget.go               # BudgetEnforcer, policy-driven spend limits
├── cost.go                 # Provider pricing maps and cost calculation
├── config.go               # Config struct and context helpers (UserID, ProjectID)
├── tokens.go               # Token counting helpers
├── stdout.go               # defaultDevPrinter for human-readable span output
├── stream.go               # Stream wrapper and iterator logic
├── providers/
│   ├── provider.go         # Provider interface definition
│   ├── {anthropic,openai,gemini,ollama}/
│   │   ├── provider.go     # Provider-specific wrapper
│   │   └── cost.go         # Provider-specific pricing
│   └── providertest/       # Mock provider for testing
├── middleware/
│   ├── gin.go              # Gin middleware for auto-attribution
│   └── echo.go             # Echo middleware for auto-attribution
├── integrations/
│   └── langchaingo/        # LangChainGo callback handler
├── exporters/              # OTel exporter definitions (stdout, Prometheus, etc.)
├── examples/               # Usage examples (simple-trace, realtime-stream, etc.)
├── docs/                   # Deep-dive documentation
│   ├── architecture.md     # Visual flow and component design
│   ├── providers.md        # Provider-specific implementation notes
│   ├── budget-guardrails.md
│   ├── middleware.md
│   ├── integrations.md
│   ├── caching.md          # Prompt caching ROI analysis
│   └── streaming.md        # Iterator-based streaming details
├── scripts/
│   └── demo.sh             # Docker demo script (Prometheus + Grafana)
└── awesome-go/             # Unrelated: awesome-go repo quality auditor
```

## Testing

- **Test files**: Located alongside source (e.g., `tracer_test.go` next to `tracer.go`). Total of ~7 test files across the project.
- **Integration vs. Unit**: Unit tests use `providertest.MockProvider`. Integration tests (in `examples/`) use real API keys and network calls — run only when actively developing a provider.
- **Coverage**: CI enforces no regression via `go test -v -race -coverprofile=coverage.out ./...`

## Adding a New LLM Provider

1. **Create the wrapper** in `providers/{newprovider}/provider.go`:
   - Implement `Provider` interface (Call + Stream methods)
   - Map provider SDK responses to standardized `CallResult` struct
   - Handle rate limits, timeouts, and streaming chunks

2. **Add pricing** in `providers/{newprovider}/cost.go`:
   - Create `GetCost(model string) (inputCost, outputCost float64)` function
   - Include cache pricing if the provider supports it

3. **Update global pricing** in `cost.go`:
   - Register your provider in the pricing dictionary so `Trace()` can auto-calculate costs

4. **Test**:
   - Add unit tests with mock responses
   - If possible, add an example in `examples/` that imports your provider

5. **Branch/PR convention**: Use `feat/add-newprovider-support` branch names and Conventional Commits (e.g., `feat: add Mistral provider support`).

## Key Patterns & Conventions

- **Context carries attribution**: User/project IDs flow through the context via `otellix.ContextWithUser()` / `otellix.ContextWithProject()`. Middleware automatically populates these; manual calls can set them explicitly.
- **Standardized OTel attributes**: All spans emit the same set of attributes regardless of provider. No provider-specific attribute names leak into observability backends.
- **Lazy metric initialization**: Metrics instruments are created once (via `sync.Once`) when first needed, allowing safe concurrent use.
- **Budget as a guardrail**: Budget limits are **enforced before execution**, not applied retroactively. Integration with Redis for distributed enforcement.
- **Stream via iterators**: `TraceStream()` returns `func() (string, error)` (a Go 1.23 iterator) rather than channels, reducing buffering complexity.

## Development Tips

- **For local development**: Call `otellix.SetupDev()` in your main to enable stdout tracing. The `defaultDevPrinter` writes human-readable span summaries to stdout without requiring Prometheus/Grafana.
- **Testing new providers**: Use `providertest.MockProvider` to avoid network calls; pass real API keys to integration tests in `examples/`.
- **Debugging cost calculations**: Check `cost.go` for your provider's pricing; verify the provider wrapper is correctly extracting token counts from the API response.
- **Middleware attribution**: If using Gin or Echo, apply `middleware.Middleware()` early in your router setup so all downstream handlers inherit context.

## Dependencies & Constraints

- **Go version**: 1.23.0+ required (uses Go 1.23 iterators for streaming)
- **Core OTel packages**: `go.opentelemetry.io/otel` + `go.opentelemetry.io/otel/sdk` provide the tracing/metric infrastructure
- **Provider SDKs**: Each provider import is optional; only imported if you use that provider in your code
- **Redis (optional)**: Only needed if using `RedisStore` for distributed budget enforcement

## CI/CD

- **CI runs on**: Push to main, all PRs to main
- **Checks**: gofmt verification, golangci-lint, go test with race detector and coverage
- **Coverage upload**: Results sent to Codecov (best-effort, non-blocking)
- **Go version in CI**: 1.23 with `check-latest: true`

Run `golangci-lint run ./...` locally before pushing to catch issues early and avoid pipeline delays.
