# Otellix

**Production-grade LLM observability for Go backends — built for cost-constrained markets.**

[![CI](https://github.com/oluwajubelo1/otellix/actions/workflows/ci.yml/badge.svg)](https://github.com/oluwajubelo1/otellix/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/oluwajubelo1/otellix/graph/badge.svg)](https://codecov.io/gh/oluwajubelo1/otellix)
[![Go Reference](https://pkg.go.dev/badge/github.com/oluwajubelo1/otellix.svg)](https://pkg.go.dev/github.com/oluwajubelo1/otellix)
[![Go Report Card](https://goreportcard.com/badge/github.com/oluwajubelo1/otellix)](https://goreportcard.com/report/github.com/oluwajubelo1/otellix)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

## Why Otellix

- **Every Go LLM observability tool is Python-first.** If you're building LLM features in Go, you're on your own for tracing, cost tracking, and budget control. Otellix is Go-native, built on OpenTelemetry, and designed for production from day one.
- **LLM costs are invisible until they're catastrophic.** Without per-user and per-feature cost attribution, a single runaway prompt can blow your monthly budget. Otellix attaches accurate USD costs to every OTel span, broken down by user, feature, and project.
- **Budget guardrails don't exist in other SDKs.** Otellix is the only Go library that lets you set per-user and per-project daily spend ceilings with automatic enforcement — block, downgrade, or notify when limits are hit. Built by someone who watched LLM API costs eat into product budgets at BudgIT (civic tech) and Class54 (edtech, 400K users) in Nigeria.

## Quick Start

```bash
go get github.com/oluwajubelo1/otellix
```

```go
package main

import (
    "context"
    "log"

    "github.com/oluwajubelo1/otellix"
    "github.com/oluwajubelo1/otellix/providers"
    "github.com/oluwajubelo1/otellix/providers/anthropic"
)

func main() {
    shutdown := otellix.SetupDev()
    defer shutdown()

    provider := anthropic.New()

    result, err := otellix.Trace(context.Background(), provider,
        providers.CallParams{
            Model:    "claude-sonnet-4-6",
            MaxTokens: 256,
            Messages: []providers.Message{
                {Role: "user", Content: "What is OpenTelemetry?"},
            },
        },
        otellix.WithFeatureID("demo"),
        otellix.WithUserID("usr_001"),
    )
    if err != nil {
        log.Fatal(err)
    }
    _ = result
}
```

Output:
```
[otellix] anthropic/claude-sonnet-4-6 | feature: demo | user: usr_001
          tokens: 245 in + 891 out | cost: $0.014070 | latency: 1.2s
          prompt_fingerprint: a3f8b1c2 | budget: $0.014/$1.000 (1%)
```

## Features

| Feature | Description | Providers |
|---|---|---|
| **Automatic tracing** | OTel spans with provider, model, tokens, cost, latency | All |
| **Cost attribution** | Accurate USD cost per call, per user, per feature, per project | All |
| **Budget guardrails** | Per-user/project daily spend ceilings with enforcement | All |
| **Prompt fingerprinting** | SHA256 fingerprint for prompt drift detection | All |
| **Prometheus metrics** | Counters and histograms for all LLM telemetry | All |
| **HTTP middleware** | Auto-extract user/project from JWT and headers | Gin, Echo |
| **Prompt Decorators**| Dynamic prompt optimization based on real-time budget | All |
| **Dev mode** | Human-readable stdout printer for local development | All |

## Configuration

```go
otellix.Trace(ctx, provider, params,
    otellix.WithProvider("anthropic"),       // LLM provider name
    otellix.WithModel("claude-sonnet-4-6"),  // Model identifier
    otellix.WithFeatureID("chat"),           // Product feature (cost attribution)
    otellix.WithUserID("usr_123"),           // User (per-user tracking + budget)
    otellix.WithProjectID("proj_456"),       // Tenant/project (multi-tenant billing)
    otellix.WithSpanName("custom.span"),     // Override default span name
    otellix.WithAttributes(map[string]string{"env": "prod"}),
    otellix.WithPromptFingerprint("system prompt + user message"),
    otellix.WithFallbackModel("claude-haiku-4-5"),
    otellix.WithBudgetConfig(&otellix.BudgetConfig{...}),
)
```

## Budget Guardrails

```go
budgetCfg := &otellix.BudgetConfig{
    PerUserDailyLimit:    0.50,  // $0.50/day per user
    PerProjectDailyLimit: 10.00, // $10.00/day per project
    FallbackAction:       otellix.FallbackBlock,
    ResetInterval:        24 * time.Hour,
}

result, err := otellix.Trace(ctx, provider, params,
    otellix.WithUserID("usr_123"),
    otellix.WithBudgetConfig(budgetCfg),
)
if err != nil {
    var budgetErr *otellix.BudgetExceededError
    if errors.As(err, &budgetErr) {
        log.Printf("Budget exceeded: spend=$%.2f limit=$%.2f resets=%s",
            budgetErr.CurrentSpend, budgetErr.Limit, budgetErr.ResetAt)
    }
}
```

**Fallback modes:**

| Mode | Behaviour |
|---|---|
| `FallbackBlock` | Return `BudgetExceededError` immediately — LLM is never called |
| `FallbackNotify` | Call the LLM but emit a `budget.warning` event on the span |
| `FallbackDowngrade` | Swap to a cheaper model (`Config.FallbackModel`) and proceed |

## Dynamic Prompt Optimization

Prompt Decorators allow your application to react to telemetry data in real-time. For example, you can inject the remaining budget directly into a system prompt to force the LLM to be more concise.

```go
decorator := func(ctx context.Context, status otellix.BudgetStatus, params *providers.CallParams) {
    if status.Remaining < 0.05 {
        params.SystemPrompt += "\n[BUDGET: You have less than $0.05 left. Be concise.]"
        params.MaxTokens = 100 // Hard-cap output tokens to save cost
    }
}

otellix.Trace(ctx, provider, params,
    otellix.WithBudgetConfig(budgetCfg),
    otellix.WithPromptDecorator(decorator),
)
```

## Metrics Emitted

| Metric | Type | Labels | Description |
|---|---|---|---|
| `otellix.llm.tokens.input` | Counter | provider, model, feature_id, user_id, project_id | Total input tokens |
| `otellix.llm.tokens.output` | Counter | Same | Total output tokens |
| `otellix.llm.cost.usd` | Counter (float) | Same | Total cost in USD |
| `otellix.llm.latency.ms` | Histogram | Same | Call latency in milliseconds |
| `otellix.llm.errors.total` | Counter | provider, model, error_type | Total errors |

## OTel Span Attributes

| Attribute | Type | Description |
|---|---|---|
| `llm.provider` | string | Provider name (anthropic, openai, gemini, ollama) |
| `llm.model` | string | Model identifier |
| `llm.feature_id` | string | Product feature that triggered the call |
| `llm.user_id` | string | User who triggered the call |
| `llm.project_id` | string | Tenant/project identifier |
| `llm.input_tokens` | int64 | Input token count |
| `llm.output_tokens` | int64 | Output token count |
| `llm.cached_tokens` | int64 | Cached token count |
| `llm.cost_usd` | float64 | Calculated cost in USD |
| `llm.latency_ms` | float64 | Latency in milliseconds |
| `llm.prompt_fingerprint` | string | SHA256 fingerprint (first 8 hex chars) |
| `llm.error` | bool | Whether the call errored |
| `llm.budget_blocked` | bool | Whether the call was blocked by budget |

## Grafana Dashboard

A pre-built Grafana dashboard is included. Quick start with Docker Compose:

```bash
cd examples/docker-compose
docker-compose up
```

Open http://localhost:3000 — panels include: Total LLM Cost Today, Cost per Feature, Token Usage by Model, Budget Utilisation, Error Rate, P95 Latency.

## Supported Providers

| Provider | Models | Notes |
|---|---|---|
| **Anthropic** | claude-opus-4-6, claude-sonnet-4-6, claude-haiku-4-5 | Prompt caching tracked |
| **OpenAI** | gpt-4o, gpt-4o-mini | Cached tokens tracked |
| **Google Gemini** | gemini-2.5-pro, gemini-2.5-flash | Via google.golang.org/genai |
| **Ollama** | Any local model | $0 cost, HTTP API |

Custom models can be registered:

```go
otellix.RegisterModel("anthropic", "claude-4-opus", otellix.PricingEntry{
    InputPricePerMToken:  20.0,
    OutputPricePerMToken: 100.0,
    CachePricePerMToken:  2.0,
})
```

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Run tests: `go test ./... -race`
4. Run vet: `go vet ./...`
5. Commit (`git commit -m 'feat: add amazing feature'`)
6. Push (`git push origin feature/amazing-feature`)
7. Open a Pull Request

## Licence

MIT — see [LICENSE](LICENSE).
