# Dev.to Article Draft

**Title**: I built a Go LLM observability SDK because Python-first tools ignored cost control

---

### The $400 Surprise
A few months ago, I was monitoring the backend for an edtech platform I help lead in Lagos, Nigeria. We had just integrated GPT-4o for a new tutoring feature. Everything looked great in testing.

Then came the weekend.

A single recursive prompt loop, triggered by an edge case in our chain logic, ran for 12 hours. By Monday morning, we had racked up nearly $400 in API costs. In a market where every dollar counts towards product affordability, that wasn't just a "bug" — it was a threat to our sustainability.

### The Problem: Visibility is Not Control
I started looking for Go-native observability tools. I found plenty of great exporters and loggers, but the "LLM Observability" space felt... different.

Most tools were either:
1. **Python-First**: LangSmith and others are fantastic, but they feel like a foreign body in a high-performance Go microservice stack.
2. **Post-Facto**: They tell you what you spent *after* the bill arrives. 
3. **Proxy-Heavy**: I didn't want to route my sensitive traffic through another third-party proxy just to get a graph.

As a Senior Backend Engineer, I wanted something that felt like it belonged in my `go.mod`. I wanted standard OpenTelemetry spans, and most importantly, I wanted **application-layer budget enforcement.**

### Why Go? (And why Otellix?)
Go is the backbone of modern cloud infrastructure for a reason. It's fast, concurrent, and has a first-class OTel ecosystem. I built **Otellix** to fill the gap for Go teams who need production-grade observability without the "Python tax."

#### 1. OpenTelemetry as a First-Class Citizen
Otellix doesn't force a new dashboard on you. It populates your existing Prometheus and Grafana instances with high-fidelity LLM attributes:
- `llm.usage.input_tokens`
- `llm.cost_usd`
- `llm.latency_ms`

#### 2. The Budget Guardrail
This is the feature that saved my sleep. You can set real-time USD limits per user or feature. If a budget is exceeded, Otellix can automatically:
- **Block**: Stop the loop before it drains your wallet.
- **Downgrade**: Swap GPT-4o for GPT-4o-mini on the fly.
- **Notify**: Fire off an OTel event that your alerting system can catch.

```go
// Real-world example: Protecting a feature
result, err := otellix.Trace(ctx, p, params,
    otellix.WithFeatureID("student-tutor"),
    otellix.WithBudgetConfig(dailyLimit),
)
```

#### 3. Zero-Config Identity Attribution
One of my biggest pet peeves is manually passing `user_id` through ten layers of function calls. Otellix includes middleware for Gin and Echo that extracts identity from headers or JWTs and injects it into the context automatically.

### Building for the Global South
Building software in Nigeria gives you a different perspective on "efficiency." When API costs are denominated in USD but your revenue is in a local currency, observability isn't a luxury — it's a requirement for survival.

I built Otellix to be the tool I wished I had: MIT licensed, OTel-native, and Go-focused.

### What's Next?
I’m currently looking for contributors and feedback from other Go shops. We’re working on:
- **Prompt Fingerprinting**: Detecting prompt drift before it affects your RAG accuracy.
- **Ollama Performance Tracking**: Deep dives into local model throughput.

If you’re building LLM apps in Go, I’d love for you to give it a try.

**GitHub**: [https://github.com/oluwajubelo1/otellix](https://github.com/oluwajubelo1/otellix)

---

*I’m Oluwasegun, a Senior Backend Engineer. I build things at the intersection of Go, LLMs, and civic tech. Let’s connect!*
