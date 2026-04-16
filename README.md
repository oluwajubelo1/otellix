# Otellix

**Stop the LLM Bill Shock. Production-grade observability for Go backends — built on OpenTelemetry.**

![LLM Observability Dashboard Mockup](assets/dashboard_mockup.png)

[![CI](https://github.com/oluwajubelo1/otellix/actions/workflows/ci.yml/badge.svg)](https://github.com/oluwajubelo1/otellix/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/oluwajubelo1/otellix.svg)](https://pkg.go.dev/github.com/oluwajubelo1/otellix)
[![Go Report Card](https://goreportcard.com/badge/github.com/oluwajubelo1/otellix)](https://goreportcard.com/report/github.com/oluwajubelo1/otellix)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

---

### 💸 The Problem: "Bill Shock" is Real
Most LLM observability tools tell you what you spent *after* the money is gone. In production, a recursive prompt loop or a single "power user" can rack up thousands in costs before your billing alerts even trigger. 

**Otellix is different.** It brings real-time cost enforcement to the application layer.

---

## ⚡ Key Features

- **🛡️ Mid-Stream Budget Cutoffs**: The only Go SDK that can **abort a stream mid-flight** if a user hits their cost or token budget. Stop the drain while the text is still generating.
- **🚀 Go-Native & High-Performance**: Built for Go 1.23 using native iterators. No heavy Python sidecars or external proxies required.
- **📊 Standard-Compliant (OTel)**: 100% OpenTelemetry compliant. Works out-of-the-box with Jaeger, Honeycomb, Prometheus, and Grafana.
- **🏠 Ollama Ready**: First-class support for local LLMs via Ollama with zero-dependency NDJSON streaming.
- **📉 Smart Fallbacks**: Automatically swap `gpt-4o` for `gpt-4o-mini` (or your local Llama3 instance) when the budget is running low.

---

## 🚀 Quick Start

```bash
go get github.com/oluwajubelo1/otellix
```

### 1. Simple Tracing
Wrap your provider calls to get instant cost attribution and standard OTel spans.

```go
package main

import (
    "context"
    "github.com/oluwajubelo1/otellix"
    "github.com/oluwajubelo1/otellix/providers/openai"
)

func main() {
    shutdown := otellix.SetupDev() // Logs to OTel Collector
    defer shutdown()

    p := openai.New()
    result, err := otellix.Trace(context.Background(), p, 
        providers.CallParams{
            Model: "gpt-4o",
            Messages: []providers.Message{{Role: "user", Content: "Hello world"}},
        },
        otellix.WithUserID("user_456"),
    )
}
```

### 2. Real-Time Budget Enforcement
Track token accumulation live and interrupt a stream if the budget is breached.

```go
stream, err := otellix.TraceStream(ctx, provider, params,
    otellix.WithUserID("user_123"),
    otellix.WithBudgetConfig(dailyLimit_1USD),
)

for evt, err := range stream.Iter() { // Go 1.23 Iterators
    if err != nil {
        if errors.Is(err, otellix.ErrBudgetExceeded) {
            fmt.Println("\n[!] Stream killed: Budget limit reached.")
        }
        break
    }
    fmt.Print(evt.Token)
}
```

---

## 📊 Standardized OTel Attributes
Otellix populates your spans with industry-standard attributes, making your Jaeger/Grafana dashboards powerful and searchable:

| Attribute | Description |
|---|---|
| `llm.provider` | The LLM provider (OpenAI, Ollama, etc.) |
| `llm.model` | The specific model used (e.g., `gpt-4o`, `llama3`) |
| `llm.cost_usd` | Real-time USD cost of the call |
| `llm.usage.total_tokens` | Cumulative token count |
| `llm.budget.blocked` | Boolean flag if the call was blocked by guardrails |

---

## 🏗️ Supported Providers

- **Anthropic**: Claude 3.5 Sonnet, Opus, Haiku (with prompt caching).
- **OpenAI**: GPT-4o, GPT-4o-mini, GPT-3.5-Turbo.
- **Google Gemini**: Gemini 1.5 Pro & Flash.
- **Ollama**: Any local model (Llama3, Mistral, Phi-3).

---

## 📖 Deep Dives
*   [**Architecture**](docs/architecture.md) — How Otellix fits into the OTel ecosystem.
*   [**Budget Guardrails**](docs/budget-guardrails.md) — Setting up cost policies & stores.
*   [**Native Streaming**](docs/streaming.md) — High-performance real-time observability.
*   [**Examples**](examples/) — Docker-Compose setup with Grafana/Tempo pre-configured.

---

## 🤝 Contributing
We love stars and contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md).
Licensed under **MIT**.
