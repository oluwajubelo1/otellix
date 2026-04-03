# Otellix

**Production-grade LLM observability for Go backends — built for cost-constrained markets.**

![LLM Observability Dashboard Mockup](assets/dashboard_mockup.png)

[![CI](https://github.com/oluwajubelo1/otellix/actions/workflows/ci.yml/badge.svg)](https://github.com/oluwajubelo1/otellix/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/oluwajubelo1/otellix/graph/badge.svg)](https://codecov.io/gh/oluwajubelo1/otellix)
[![Go Reference](https://pkg.go.dev/badge/github.com/oluwajubelo1/otellix.svg)](https://pkg.go.dev/github.com/oluwajubelo1/otellix)
[![Go Report Card](https://goreportcard.com/badge/github.com/oluwajubelo1/otellix)](https://goreportcard.com/report/github.com/oluwajubelo1/otellix)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

## Why Otellix

- **Go-Native first.** No Python "sidecars" or heavy frameworks needed. If you're building LLM features in Go, Otellix is your production-ready bridge between AI and Observability.
- **Standards-based.** Built entirely on **OpenTelemetry**. Traces, spans, and metrics are standard-compliant, ensuring zero vendor lock-in. Switch between Jaeger, Honeycomb, Datadog, or Grafana without changing your code.
- **Cost Guardrails.** The only Go SDK that treats LLM costs as a first-class citizen. Attribution per-user, per-feature, and per-project with automatic **Budget Cutoffs** that block or notification mid-stream when limits are hit.
- **Ollama Ready.** Full, high-performance support for local LLMs via Ollama, including real-time NDJSON streaming with zero external dependencies.

## Quick Start

```bash
go get github.com/oluwajubelo1/otellix
```

### Simple Tracing

```go
package main

import (
    "context"
    "github.com/oluwajubelo1/otellix"
    "github.com/oluwajubelo1/otellix/providers/openai"
)

func main() {
    shutdown := otellix.SetupDev()
    defer shutdown()

    provider := openai.New() // Uses OPENAI_API_KEY env

    result, err := otellix.Trace(context.Background(), provider, 
        providers.CallParams{
            Model: "gpt-4o",
            Messages: []providers.Message{{Role: "user", Content: "Hello world"}},
        },
        otellix.WithFeatureID("chat"),
        otellix.WithUserID("user_007"),
    )
    // ...
}
```

## Native Real-time Streaming

Otellix supports native Go 1.23 iterators for streaming. It tracks token accumulation in real-time and can **abort a stream mid-flight** if the user hits their budget limit.

```go
stream, err := otellix.TraceStream(ctx, provider, params,
    otellix.WithUserID("user_123"),
    otellix.WithBudgetConfig(budgetCfg),
)
defer stream.Close()

for {
    evt, err := stream.Recv()
    if err != nil {
        if strings.Contains(err.Error(), "budget") {
            fmt.Println("\n[!] CUTOFF: User ran out of money mid-response!")
        }
        break
    }
    fmt.Print(evt.Token) // Watch it stream live...
}
```

## Features

| Feature | Description | Providers |
|---|---|---|
| **Real-time Streaming** | Native Go iterators with usage tracking | OpenAI, Anthropic, Gemini, Ollama |
| **Cost Attribution** | USD cost per call/user/feature/project | All Cloud Providers |
| **Budget Guardrails** | Per-user/project daily ceilings (Block/Notify/Downgrade) | All |
| **Local LLM Native** | Direct NDJSON parsing for Ollama (no overhead) | Ollama |
| **Prompt Optimization**| Dynamic prompts based on real-time budget telemetry | All |
| **Standard OTel** | 100% compliant Spans, Attributes, and Metrics | All |

## Local LLM Observability (Ollama)

Otellix treats **Ollama** as a first-class provider. It implements a zero-dependency, high-performance NDJSON parser to ensure your local traces are as detailed as your cloud production environment.

```go
provider := ollama.New(ollama.WithBaseURL("http://localhost:11434"))
// Everything else is the same — Otellix abstracts the details.
```

## Budget Guardrails & Fallbacks

| Mode | Behaviour |
|---|---|
| `FallbackBlock` | Block the call (or abort the stream) immediately if limit is hit. |
| `FallbackNotify` | Proceed with the call but emit a `budget.warning` OTel event. |
| `FallbackDowngrade`| Automatically swap `gpt-4o` for `gpt-4o-mini` if budget is low. |

## OTel Attributes (Standardized)

Otellix populates your spans with standardized attributes for powerful querying in Grafana/Honeycomb:
*   `llm.provider`, `llm.model`, `llm.feature_id`, `llm.user_id`, `llm.project_id`
*   `llm.input_tokens`, `llm.output_tokens`, `llm.cost_usd`, `llm.latency_ms`
*   `llm.budget_blocked`, `llm.prompt_fingerprint`

## Grafana Dashboard

A pre-built dashboard is included in `examples/docker-compose`. View live token usage, cost growth, and budget utilization in seconds.

```bash
cd examples/docker-compose && docker-compose up
```

## Supported Providers

*   **Anthropic**: Claude 3.5 Sonnet, Opus, Haiku (with prompt caching metrics).
*   **OpenAI**: GPT-4o, GPT-4o-mini, GPT-3.5-Turbo.
*   **Google Gemini**: Gemini 1.5 Pro & Flash (via Native SDK).
*   **Ollama**: Any local model (Llama3, Mistral, Phi-3).

## Contributing & License

We love contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md).
Licensed under **MIT**.
LICENSE).
